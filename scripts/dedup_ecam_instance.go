//go:build ignore
// +build ignore

// Dedup script for ecam_instance: removes duplicate rows on the unique key
// {tenant_id, model_uid, asset_id}, keeping the newest doc per group.
//
// Background
// ----------
// The tenant_id int64 migration (scripts/rebuild_tenant_ids.go) could not
// rebuild the unique index tenant_id_1_model_uid_1_asset_id_1 on
// ecam_instance because the prod app's asset-sync cron had been inserting
// string-"Jlc" docs concurrently with the migration. After those were
// rewritten to int64(3), they collided with already-migrated int64 docs on
// the unique key. The index was left dropped. This script removes the dup
// rows so the index can be rebuilt.
//
// Keeper policy
//   * Group by {tenant_id, model_uid, asset_id} over docs whose tenant_id is
//     $type "long" (string docs should be gone after the rewrite, but this is
//     a safety filter — we never dedup docs we might still rewrite).
//   * For groups with >1 doc: keeper = highest utime; tiebreak by highest _id
//     (ObjectId, carries a timestamp — newest insert wins).
//   * Collect the _id of every NON-keeper doc and delete them.
//
// Safety
//   * Never delete the keeper. Assert keeper._id is NOT in the delete set
//     before any DeleteMany.
//   * If a group's model_uid or asset_id is empty/missing, report it but DO
//     NOT dedup that group — those aren't real duplicates of the unique key.
//   * Dry-run by default; -apply gate required to delete.
//
// Usage
//   # Dry-run (default): report only.
//   go run scripts/dedup_ecam_instance.go
//
//   # Apply: actually delete duplicates in batches of 1000.
//   go run scripts/dedup_ecam_instance.go -apply
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mongoURI = flag.String("mongo", "mongodb://106.52.187.69:27017/ecam?authSource=admin", "MongoDB URI")
	database = flag.String("db", "ecam", "Database name")
	username = flag.String("user", "ecmdb", "MongoDB username")
	password = flag.String("password", "123456", "MongoDB password")
	apply    = flag.Bool("apply", false, "Apply the dedup (default: dry-run, preview only)")
	batch    = flag.Int("batch", 1000, "Delete batch size (apply mode)")
)

const collectionName = "ecam_instance"

// dupGroup is what the $group aggregation emits per duplicate key.
type dupGroup struct {
	ID      groupKey `bson:"_id"`
	Members []member `bson:"members"`
	Count   int64    `bson:"count"`
}

// member pairs an _id with its utime so pickKeeper can apply the primary
// "highest utime" rule and only fall back to _id on ties.
type member struct {
	ID    primitive.ObjectID `bson:"id"`
	Utime int64              `bson:"u"`
}

// groupKey is the unique-key shape {tenant_id, model_uid, asset_id}.
type groupKey struct {
	TenantID int64  `bson:"tenant_id"`
	ModelUID string `bson:"model_uid"`
	AssetID  string `bson:"asset_id"`
}

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Printf("==========================================================")
	log.Printf("ecam_instance dedup on {tenant_id, model_uid, asset_id}")
	log.Printf("URI       : %s", *mongoURI)
	log.Printf("Database  : %s", *database)
	log.Printf("User      : %s", *username)
	if *apply {
		log.Printf("MODE      : APPLY (duplicates WILL be deleted, batch=%d)", *batch)
	} else {
		log.Printf("MODE      : DRY-RUN (no data will be modified)")
	}
	log.Printf("==========================================================")

	// 30 min budget — ecam_instance is ~22k docs; the aggregation + batched
	// deletes are fast, but give ourselves headroom for slow network.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cred := options.Credential{
		Username:   *username,
		Password:   *password,
		AuthSource: "admin",
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(*mongoURI).SetAuth(cred))
	if err != nil {
		log.Fatalf("mongo.Connect: %v", err)
	}
	defer func() { _ = client.Disconnect(ctx) }()
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ping failed: %v", err)
	}
	log.Printf("connected OK")

	coll := client.Database(*database).Collection(collectionName)

	// ---- 1. Find duplicate groups ----
	groups, skipped, err := findDupGroups(ctx, coll)
	if err != nil {
		log.Fatalf("findDupGroups: %v", err)
	}

	// ---- 2. Build the delete list (everything except the keeper per group) ----
	type example struct {
		key      groupKey
		count    int64
		keeper   primitive.ObjectID
		deleting int
	}
	var (
		toDelete    []primitive.ObjectID
		examples    []example
		totalDocs   int64
	)
	keeperSet := make(map[primitive.ObjectID]bool, len(groups))
	for _, g := range groups {
		totalDocs += g.Count
		keeper := pickKeeper(g)
		keeperSet[keeper] = true
		var groupDel []primitive.ObjectID
		for _, m := range g.Members {
			if m.ID == keeper {
				continue
			}
			groupDel = append(groupDel, m.ID)
		}
		toDelete = append(toDelete, groupDel...)
		if len(examples) < 3 {
			examples = append(examples, example{key: g.ID, count: g.Count, keeper: keeper, deleting: len(groupDel)})
		}
	}

	// ---- Safety: keeper must never appear in the delete list. ----
	bad := 0
	for _, id := range toDelete {
		if keeperSet[id] {
			bad++
		}
	}
	if bad > 0 {
		log.Fatalf("SAFETY ABORT: %d keeper _ids appear in the delete set — refusing to proceed", bad)
	}
	// Also defensive: dedup the delete list so the same _id can't be sent twice.
	toDelete = dedupObjectIDs(toDelete)

	log.Printf("----------------------------------------------------------")
	log.Printf("duplicate groups       : %d", len(groups))
	log.Printf("docs in those groups   : %d", totalDocs)
	log.Printf("docs to delete         : %d (after keeper protection)", len(toDelete))
	log.Printf("keepers (protected)    : %d", len(keeperSet))
	if len(skipped) > 0 {
		log.Printf("skipped (empty key)    : %d groups — NOT deduped, investigate:", len(skipped))
		for _, s := range skipped {
			log.Printf("    tenant_id=%d model_uid=%q asset_id=%q", s.TenantID, s.ModelUID, s.AssetID)
		}
	}
	log.Printf("----------------------------------------------------------")
	if len(examples) > 0 {
		log.Printf("example duplicate groups (up to 3):")
		for i, e := range examples {
			log.Printf("  [%d] key={tenant_id:%d, model_uid:%q, asset_id:%q} count=%d keep=%s deleting=%d",
				i+1, e.key.TenantID, e.key.ModelUID, e.key.AssetID, e.count, e.keeper.Hex(), e.deleting)
		}
	} else {
		log.Printf("no duplicate groups found — nothing to do")
	}

	if !*apply {
		log.Printf("----------------------------------------------------------")
		log.Printf("DRY-RUN complete. Re-run with -apply to delete the %d duplicates.", len(toDelete))
		return
	}

	if len(toDelete) == 0 {
		log.Printf("nothing to delete; exiting.")
		return
	}

	// ---- 3. Apply: DeleteMany in batches. ----
	deletedTotal, err := deleteInBatches(ctx, coll, toDelete, *batch)
	if err != nil {
		log.Fatalf("delete failed (deleted %d before error): %v", deletedTotal, err)
	}
	log.Printf("----------------------------------------------------------")
	log.Printf("APPLY done: deleted %d / planned %d duplicates", deletedTotal, len(toDelete))

	// ---- 4. Post-apply count ----
	final, err := coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Printf("WARN final CountDocuments: %v", err)
	} else {
		log.Printf("final ecam_instance doc count: %d", final)
	}
}

// findDupGroups runs the $group aggregation and returns only groups with
// count > 1 whose {model_uid, asset_id} are both non-empty (others are
// returned in `skipped` for the operator to investigate).
func findDupGroups(ctx context.Context, coll *mongo.Collection) (groups []dupGroup, skipped []groupKey, err error) {
	pipeline := mongo.Pipeline{
		// Only long tenant_id docs — string docs should already be gone, but
		// we must never dedup docs that the rewrite might still touch.
		{{Key: "$match", Value: bson.M{"tenant_id": bson.M{"$type": "long"}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "tenant_id", Value: "$tenant_id"},
				{Key: "model_uid", Value: "$model_uid"},
				{Key: "asset_id", Value: "$asset_id"},
			}},
			{Key: "members", Value: bson.D{{Key: "$push", Value: bson.D{
				{Key: "id", Value: "$_id"},
				{Key: "u", Value: "$utime"},
			}}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		// Only groups with more than one doc — i.e. the duplicates.
		{{Key: "$match", Value: bson.M{"count": bson.M{"$gt": 1}}}},
	}

	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, nil, fmt.Errorf("Aggregate: %w", err)
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		var g dupGroup
		if err := cur.Decode(&g); err != nil {
			return nil, nil, fmt.Errorf("decode group: %w", err)
		}
		if g.ID.ModelUID == "" || g.ID.AssetID == "" {
			skipped = append(skipped, g.ID)
			continue
		}
		groups = append(groups, g)
	}
	return groups, skipped, cur.Err()
}

// pickKeeper chooses the doc with the highest utime; ties broken by the
// highest _id (ObjectId embeds a 4-byte timestamp, so highest _id == newest
// insert, which matches the "keep the latest-seen sync" intent).
func pickKeeper(g dupGroup) primitive.ObjectID {
	var (
		bestID    primitive.ObjectID
		bestU     int64
		bestFound bool
	)
	for _, m := range g.Members {
		switch {
		case !bestFound:
			bestID, bestU, bestFound = m.ID, m.Utime, true
		case m.Utime > bestU:
			bestID, bestU = m.ID, m.Utime
		case m.Utime == bestU && m.ID.Hex() > bestID.Hex():
			// utime tie — fall back to newest ObjectId (newest insert).
			bestID = m.ID
		}
	}
	return bestID
}

// deleteInBatches deletes _ids in chunks of `batchSize` using DeleteMany.
// Returns the total deleted count. Wraps each DeleteMany in withRetry so a
// transient socket EOF doesn't kill the run halfway through.
func deleteInBatches(ctx context.Context, coll *mongo.Collection, ids []primitive.ObjectID, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	var total int64
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]

		var res *mongo.DeleteResult
		err := withRetry(ctx, fmt.Sprintf("DeleteMany batch [%d:%d]", i, end), func() error {
			var err error
			res, err = coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": chunk}})
			return err
		})
		if err != nil {
			return total, fmt.Errorf("DeleteMany [%d:%d]: %w", i, end, err)
		}
		total += res.DeletedCount
		log.Printf("  deleted batch [%d:%d]: %d (running total %d / %d)", i, end, res.DeletedCount, total, len(ids))
	}
	return total, nil
}

// withRetry mirrors scripts/rebuild_tenant_ids.go: 3 attempts, linear backoff,
// retry only on transient connection errors. Transcribed (not imported) so this
// file stays standalone.
func withRetry(ctx context.Context, op string, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			msg := err.Error()
			retryable := strings.Contains(msg, "EOF") ||
				strings.Contains(msg, "connection reset") ||
				strings.Contains(msg, "connection refused") ||
				strings.Contains(msg, "socket was unexpectedly closed") ||
				strings.Contains(msg, "driver: bad connection") ||
				strings.Contains(msg, "server returned error on SDAM")
			if !retryable {
				return err
			}
			log.Printf("  attempt %d for %s failed (transient): %v", attempt, op, err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*5) * time.Second):
			}
			continue
		}
		return nil
	}
	return lastErr
}

// dedupObjectIDs returns a copy of `in` with duplicates removed, preserving
// order. Defensive — guards against the same _id being sent to DeleteMany
// twice if the aggregation ever produces overlapping groups.
func dedupObjectIDs(in []primitive.ObjectID) []primitive.ObjectID {
	if len(in) == 0 {
		return in
	}
	seen := make(map[primitive.ObjectID]bool, len(in))
	out := make([]primitive.ObjectID, 0, len(in))
	for _, id := range in {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) != len(in) {
		fmt.Fprintf(os.Stderr, "WARN: delete list contained %d duplicate _ids, deduped to %d\n", len(in)-len(out), len(out))
	}
	return out
}

// hex is a small helper so we can print ObjectIDs without importing yet
// another package; primitive.ObjectID has a .Hex() method already, so this
// is intentionally a no-op shim kept for readability of the example log line.
