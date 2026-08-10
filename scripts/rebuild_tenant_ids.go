//go:build ignore
// +build ignore

// Migration script: rebuild tenant_id from string to int64 across all ecam_* collections.
//
// Background
// ----------
// Tasks 1+5 of the tenant-unification plan migrated all Go code from string
// tenant slugs to int64 tenant IDs (GetTenantID returns int64, all DAOs filter
// with `filter.TenantID != 0`). The MongoDB data, however, still holds string
// values ("Jlc", "default", ""). With no custom BSON decoder in the repo, this
// type mismatch makes int64 queries silently match nothing → every
// tenant-scoped query returns empty results. This script rewrites the data.
//
// Mapping (from task-4-brief.md, verified via eiam HTTP API):
//   "Jlc"     → int64(3)  (jlc, 深圳市嘉立创科技发展有限公司, newly created)
//   "default" → int64(2)  (default-tenant)
//   ""        → per-collection ruling (see emptyStringPolicies below)
//
// Empty-string per-collection ruling (brief Step 2):
//   ecam_cost_recommendation: "" → 3 (all 605 docs have no owner; assigned to main tenant)
//   ecam_audit_log:           "" → 3 (other 430 docs already Jlc)
//   ecam_environment:         delete 4 "" docs (their dev/prod/test dup the Jlc rows)
//
// Safety invariants
//   * Never map anything to 0 — DAO predicate `!= 0` drops the filter for 0,
//     making such rows readable by every tenant. Only 3, 2, 1 are valid targets.
//   * Orphan (non-ecam_*) collections are skipped entirely.
//   * If a tenant_id value outside {"Jlc","default",""} is found on an ecam_*
//     collection, STOP — that means an unidentified tenant path.
//   * Always write int64(...) explicitly; a bare literal like 3 encodes as
//     int32 and won't match the int64 `bson:"tenant_id"` field.
//
// Maintenance window
//   Run this script ONLY when the prod app is stopped OR already running the
//   new int64-aware code. The old code continuously upserts documents with
//     string tenant_id values (e.g. ecam_instance asset sync at ~100-200 docs/min).
//   Concurrent string inserts race the migration: new "Jlc" docs appear between
//     discovery and UpdateMany, and after rewrite they collide with already-
//     migrated int64 docs on unique indexes (e.g. tenant_id_1_model_uid_1_asset_id_1).
//   The script is idempotent and safe to re-run; finish cleanup after the
//   new code is deployed.
//
// Usage
//   # Dry-run (default): preview only, no writes.
//   go run scripts/rebuild_tenant_ids.go
//
//   # Apply: actually perform the migration (gated flag, never auto-run).
//   go run scripts/rebuild_tenant_ids.go -apply
//
// Flags
//   -mongo     MongoDB URI     (default: prod ecam)
//   -db        Database name   (default: ecam)
//   -user      Username        (default: ecmdb)
//   -password  Password        (default: 123456)
//   -apply     Enable mutation (default: false = dry-run)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mongoURI = flag.String("mongo", "mongodb://106.52.187.69:27017/ecam?authSource=admin", "MongoDB URI")
	database = flag.String("db", "ecam", "Database name")
	username = flag.String("user", "ecmdb", "MongoDB username")
	password = flag.String("password", "123456", "MongoDB password")
	apply    = flag.Bool("apply", false, "Apply the migration (default: dry-run, preview only)")
)

// valueMapping is the global mapping from old string tenant values to new int64 IDs.
// Source: brief Step 1 (verified via eiam HTTP API; ids 1/2/3 confirmed).
var valueMapping = map[string]int64{
	"Jlc":     3,
	"default": 2,
}

// emptyStringPolicy describes per-collection handling of "".
// The brief explicitly forbids mapping "" to 0 and rules each collection.
type emptyStringPolicy struct {
	action string // "rewrite" or "delete"
	value  int64  // target int64 when action == "rewrite"
}

var emptyStringPolicies = map[string]emptyStringPolicy{
	"ecam_cost_recommendation": {action: "rewrite", value: 3},
	"ecam_audit_log":           {action: "rewrite", value: 3},
	"ecam_environment":         {action: "delete"},
}

// allowedValues is the set of tenant_id values considered legal in cam data.
// String values are sources to be migrated; "int64:N" synthetic keys represent
// already-migrated docs (a state reached by a previous successful run of this
// script — recognizing it makes the script idempotent and resumable).
// Any value outside this set on an ecam_* collection is an unidentified tenant
// path → STOP and report.
var allowedValues = map[string]bool{
	"Jlc":     true,
	"default": true,
	"":        true,
	// Recognized int64 targets (post-migration). Never map anything to 0 —
	// the DAO predicate `!= 0` drops the filter for 0.
	"int64:1": true,
	"int64:2": true,
	"int64:3": true,
}

const ecamPrefix = "ecam_"

// indexSpec captures the subset of MongoDB index options we need to rebuild.
// All tenant_id indexes in the codebase use only {key, unique?, sparse?}
// (verified by reading the 16 index-definition files listed in the brief).
type indexSpec struct {
	Name   string `bson:"name"`
	Key    bson.D `bson:"key"` // ordered — preserves compound-index key order
	Unique bool   `bson:"unique"`
	Sparse bool   `bson:"sparse"`
}

// canonicalTenantIDIndexes is a fallback used when listTenantIDIndexes returns
// no specs but the collection should have tenant_id indexes (i.e., a previous
// run dropped them and failed before recreating). Specs are transcribed from
// the codebase's dao init files. The mongo Go driver does not auto-retry on
// socket EOF, so without this fallback a transient connection drop on the
// index-rebuild step would leave the collection with no tenant_id indexes
// until the app is restarted.
//
// To extend: when another collection's indexes get dropped but not rebuilt,
// copy the specs from the corresponding dao init.go into this map.
var canonicalTenantIDIndexes = map[string][]indexSpec{
	"ecam_cost_unified_bill": {
		{Name: "tenant_id_1_billing_date_1_provider_1", Key: bson.D{{Key: "tenant_id", Value: 1}, {Key: "billing_date", Value: 1}, {Key: "provider", Value: 1}}},
		{Name: "tenant_id_1_account_id_1_billing_date_1", Key: bson.D{{Key: "tenant_id", Value: 1}, {Key: "account_id", Value: 1}, {Key: "billing_date", Value: 1}}},
		{Name: "tenant_id_1_service_type_1_billing_date_1", Key: bson.D{{Key: "tenant_id", Value: 1}, {Key: "service_type", Value: 1}, {Key: "billing_date", Value: 1}}},
		{Name: "tenant_id_1_region_1_billing_date_1", Key: bson.D{{Key: "tenant_id", Value: 1}, {Key: "region", Value: 1}, {Key: "billing_date", Value: 1}}},
		{Name: "billing_date_1_tenant_id_1_provider_1_amount_cny_1", Key: bson.D{{Key: "billing_date", Value: 1}, {Key: "tenant_id", Value: 1}, {Key: "provider", Value: 1}, {Key: "amount_cny", Value: 1}}},
	},
}

// collInfo is the discovery result for one collection.
type collInfo struct {
	name   string
	values map[string]int64 // value → count (explicit, via CountDocuments)
	total  int64            // count of docs with tenant_id exists
}

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Printf("==========================================================")
	log.Printf("tenant_id int64 migration")
	log.Printf("URI       : %s", *mongoURI)
	log.Printf("Database  : %s", *database)
	log.Printf("User      : %s", *username)
	if *apply {
		log.Printf("MODE      : APPLY (data WILL be modified)")
	} else {
		log.Printf("MODE      : DRY-RUN (no data will be modified)")
	}
	log.Printf("==========================================================")
	if *apply {
		log.Printf("NOTE: ensure prod app is stopped or running int64-aware code; concurrent string inserts will race")
	}

	// 2h budget — ecam_cost_unified_bill has ~5M docs; updateMany is a single
	// server-side op but takes minutes. Don't artificially cut it short.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
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

	db := client.Database(*database)

	// ---- Phase 1: discovery ----
	log.Printf("")
	log.Printf("========== DISCOVERY ==========")
	infos, orphans, err := discover(ctx, db)
	if err != nil {
		log.Fatalf("discover: %v", err)
	}

	// Validate: every value seen must be in allowedValues.
	var unknowns []string
	for _, c := range infos {
		for v := range c.values {
			if !allowedValues[v] {
				unknowns = append(unknowns, fmt.Sprintf("%s=%q", c.name, v))
			}
		}
	}
	if len(unknowns) > 0 {
		log.Printf("")
		log.Printf("!!! STOP — unknown tenant_id values found (not in mapping) !!!")
		for _, u := range unknowns {
			log.Printf("  - %s", u)
		}
		log.Printf("This means a tenant source we have not identified. Aborting before any mutation.")
		os.Exit(2)
	}

	// Print summary
	printSummary(infos, orphans)

	if !*apply {
		log.Printf("")
		log.Printf("DRY-RUN complete. Re-run with -apply to perform the migration.")
		return
	}

	// ---- Phase 2: apply ----
	// Process ALL collections even if one fails — the operator needs the full
	// picture from VALIDATE to decide what to fix. Failures are collected and
	// reported at the end (after VALIDATE); the process exits non-zero if any
	// collection failed or any validation check failed.
	log.Printf("")
	log.Printf("========== APPLY ==========")
	applyStart := time.Now()
	var applyFailed []string
	for i := range infos {
		c := &infos[i]
		coll := db.Collection(c.name)
		start := time.Now()
		if err := migrateCollection(ctx, coll, c); err != nil {
			log.Printf("FAIL %s: %v", c.name, err)
			applyFailed = append(applyFailed, c.name)
			continue
		}
		log.Printf("  %s: collection done in %s", c.name, time.Since(start))
	}
	log.Printf("APPLY total elapsed: %s", time.Since(applyStart))

	// ---- Phase 3: validate ----
	// Run unconditionally on every active collection, regardless of whether
	// its APPLY step succeeded. This surfaces partial-success states (e.g.
	// data rewritten but index rebuild failed) that a $type aggregation can
	// still detect.
	log.Printf("")
	log.Printf("========== VALIDATE ==========")
	var validateFailed []string
	for _, c := range infos {
		if err := validateCollection(ctx, db.Collection(c.name), c.name); err != nil {
			log.Printf("VALIDATION FAIL %s: %v", c.name, err)
			validateFailed = append(validateFailed, c.name)
		}
	}

	// Final exit decision: non-zero if any APPLY or VALIDATE step failed.
	if len(applyFailed) > 0 {
		log.Printf("APPLY failed for %d collection(s): %s", len(applyFailed), strings.Join(applyFailed, ", "))
	}
	if len(validateFailed) > 0 {
		log.Printf("VALIDATE failed for %d collection(s): %s", len(validateFailed), strings.Join(validateFailed, ", "))
	}
	if len(applyFailed) > 0 || len(validateFailed) > 0 {
		log.Printf("Migration completed with errors (apply=%d, validate=%d).", len(applyFailed), len(validateFailed))
		os.Exit(1)
	}
	log.Printf("All active collections validated: tenant_id is int64 (\"long\") only (or empty collection).")
}

// discover lists all collections and probes each for tenant_id presence.
func discover(ctx context.Context, db *mongo.Database) (infos []collInfo, orphans []string, err error) {
	names, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, nil, fmt.Errorf("ListCollectionNames: %w", err)
	}
	sort.Strings(names)
	log.Printf("total collections in %s: %d", db.Name(), len(names))

	for _, name := range names {
		// Probe: does this collection have any doc with tenant_id?
		var probe bson.M
		err := db.Collection(name).FindOne(ctx, bson.M{"tenant_id": bson.M{"$exists": true}}).Decode(&probe)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue // no tenant_id docs (or empty collection) → skip
			}
			return nil, nil, fmt.Errorf("probe %s: %w", name, err)
		}
		if !strings.HasPrefix(name, ecamPrefix) {
			orphans = append(orphans, name)
			continue
		}
		info, err := inspect(ctx, db.Collection(name), name)
		if err != nil {
			return nil, nil, fmt.Errorf("inspect %s: %w", name, err)
		}
		infos = append(infos, *info)
	}
	log.Printf("active ecam_* collections with tenant_id: %d", len(infos))
	log.Printf("orphan (non-ecam_*) collections with tenant_id: %d (will skip)", len(orphans))
	return infos, orphans, nil
}

// inspect counts documents per tenant_id value. We use explicit CountDocuments
// rather than relying on Distinct because "" is invisible in fmt output and
// can be misread; counting each candidate value directly is authoritative.
func inspect(ctx context.Context, coll *mongo.Collection, name string) (*collInfo, error) {
	info := &collInfo{name: name, values: map[string]int64{}}

	// Distinct returns the value set (and yes, "" comes back, just hard to see).
	values, err := coll.Distinct(ctx, "tenant_id", bson.M{"tenant_id": bson.M{"$exists": true}})
	if err != nil {
		return nil, fmt.Errorf("Distinct: %w", err)
	}
	for _, v := range values {
		var key string
		switch x := v.(type) {
		case string:
			key = x
		case int64:
			// Already migrated to int64 — track under a synthetic key so the
			// plan/summary can show it, allowedValues accepts the known 1/2/3,
			// and migrateCollection skips the rewrite.
			key = fmt.Sprintf("int64:%d", x)
		default:
			// Non-string, non-int64 value present — surface as a recognizable
			// synthetic key so allowedValues fails loudly and the operator
			// can investigate.
			key = fmt.Sprintf("<%T:%v>", v, v)
		}
		count, err := coll.CountDocuments(ctx, bson.M{"tenant_id": v})
		if err != nil {
			return nil, fmt.Errorf("CountDocuments(%v): %w", v, err)
		}
		info.values[key] = count
		info.total += count
	}

	// Safety net: in case Distinct missed "" (shouldn't, but cheap to verify).
	emptyCount, err := coll.CountDocuments(ctx, bson.M{"tenant_id": ""})
	if err != nil {
		return nil, fmt.Errorf("CountDocuments(\"\"): %w", err)
	}
	if emptyCount > 0 {
		if _, ok := info.values[""]; !ok {
			info.values[""] = emptyCount
			info.total += emptyCount
		}
	}
	return info, nil
}

// printSummary prints the dry-run plan: per-collection, per-value, planned action.
func printSummary(infos []collInfo, orphans []string) {
	log.Printf("")
	log.Printf("========== PLAN ==========")
	if len(orphans) > 0 {
		log.Printf("Orphan collections (skipped, not ecam_*):")
		for _, n := range orphans {
			log.Printf("  - %s", n)
		}
		log.Printf("")
	}
	log.Printf("Per-collection plan (only ecam_*):")
	for _, c := range infos {
		log.Printf("--- %s (total %d docs with tenant_id) ---", c.name, c.total)
		vals := sortedValues(c.values)
		for _, v := range vals {
			cnt := c.values[v]
			action, detail := planAction(c.name, v)
			label := v
			if v == "" {
				label = "(empty)"
			}
			log.Printf("  %-12s (%6d docs) → %-7s %s", fmt.Sprintf("%q", label), cnt, action, detail)
		}
	}
}

// sortedValues returns the keys sorted with "" first, then alphabetical.
func sortedValues(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i] == "" {
			return true
		}
		if out[j] == "" {
			return false
		}
		return out[i] < out[j]
	})
	return out
}

// planAction returns (action, detail) for a (collection, value) pair.
// action is "rewrite" / "delete" / "skip" / "STOP".
func planAction(name, value string) (action, detail string) {
	if strings.HasPrefix(value, "int64:") {
		return "skip", "already migrated (int64)"
	}
	if value == "" {
		p, ok := emptyStringPolicies[name]
		if !ok {
			return "STOP", "no empty-string policy for this collection"
		}
		switch p.action {
		case "rewrite":
			return "rewrite", fmt.Sprintf("tenant_id → %d", p.value)
		case "delete":
			return "delete", "remove documents"
		}
	}
	newID, ok := valueMapping[value]
	if !ok {
		return "STOP", "unknown value (not in mapping)"
	}
	return "rewrite", fmt.Sprintf("tenant_id → %d", newID)
}

// migrateCollection performs the per-collection migration in the brief's
// mandatory order: drop tenant_id indexes → rewrite/delete data → rebuild indexes.
// Dropping indexes first avoids unique-constraint collisions during the rewrite
// (e.g. ecam_environment before dedup, ecam_audit_log where "" and Jlc both → 3).
//
// Resumability: the function is idempotent. UpdateMany matches 0 on already-
// migrated data; DropOne on an already-dropped index is a no-op; CreateOne on
// an existing identical index is a no-op. If the DB has lost its tenant_id
// indexes (e.g., previous run dropped them then crashed before recreating),
// the canonical-spec fallback is used for the rebuild.
func migrateCollection(ctx context.Context, coll *mongo.Collection, c *collInfo) error {
	log.Printf("  %s: starting migration", c.name)

	// 1. Snapshot existing tenant_id indexes.
	existingSpecs, err := listTenantIDIndexes(ctx, coll)
	if err != nil {
		return fmt.Errorf("list indexes: %w", err)
	}

	// Determine rebuild specs: existing snapshot, or canonical fallback if the
	// snapshot is empty (recovery scenario after a previous failed run).
	rebuildSpecs := existingSpecs
	if len(rebuildSpecs) == 0 {
		if canon, ok := canonicalTenantIDIndexes[c.name]; ok {
			rebuildSpecs = canon
			log.Printf("  %s: no tenant_id indexes in DB; using %d canonical specs as fallback", c.name, len(canon))
		} else {
			// Genuine "collection has no tenant_id indexes" is fine, but a
			// previous failed run that dropped-and-crashed would land here too.
			// The validation stage only checks $type, not index presence, so
			// warn loudly to flag the silent-success case.
			log.Printf("  %s: WARN no tenant_id indexes found in DB and no canonical fallback; if this collection should have indexes, rebuild manually", c.name)
		}
	}

	// Drop existing tenant_id indexes (only those that were actually present).
	for _, s := range existingSpecs {
		if _, err := coll.Indexes().DropOne(ctx, s.Name); err != nil {
			// Non-droppable indexes (e.g. _id_) should not appear in our list,
			// but warn instead of aborting if mongo refuses.
			log.Printf("  %s: WARN drop index %s failed: %v", c.name, s.Name, err)
			continue
		}
		log.Printf("  %s: dropped index %s", c.name, s.Name)
	}

	// 2. Rewrite/delete data per value.
	vals := sortedValues(c.values)
	for _, v := range vals {
		cnt := c.values[v]
		switch {
		case strings.HasPrefix(v, "int64:"):
			// Already migrated (e.g., by a previous run). No data rewrite needed.
			log.Printf("  %s: %s (%d docs) already migrated, skipping", c.name, v, cnt)
			continue
		case v == "Jlc":
			var n *mongo.UpdateResult
			if err := withRetry(ctx, fmt.Sprintf("UpdateMany Jlc on %s", c.name), func() error {
				var err error
				n, err = coll.UpdateMany(ctx,
					bson.M{"tenant_id": "Jlc"},
					bson.M{"$set": bson.M{"tenant_id": int64(3)}},
				)
				return err
			}); err != nil {
				return fmt.Errorf("UpdateMany Jlc: %w", err)
			}
			log.Printf("  %s: \"Jlc\" → 3 (matched %d, modified %d, planned %d)", c.name, n.MatchedCount, n.ModifiedCount, cnt)
		case v == "default":
			var n *mongo.UpdateResult
			if err := withRetry(ctx, fmt.Sprintf("UpdateMany default on %s", c.name), func() error {
				var err error
				n, err = coll.UpdateMany(ctx,
					bson.M{"tenant_id": "default"},
					bson.M{"$set": bson.M{"tenant_id": int64(2)}},
				)
				return err
			}); err != nil {
				return fmt.Errorf("UpdateMany default: %w", err)
			}
			log.Printf("  %s: \"default\" → 2 (matched %d, modified %d, planned %d)", c.name, n.MatchedCount, n.ModifiedCount, cnt)
		case v == "":
			p, ok := emptyStringPolicies[c.name]
			if !ok {
				return fmt.Errorf("no empty-string policy for %s (should have been caught earlier)", c.name)
			}
			switch p.action {
			case "rewrite":
				var n *mongo.UpdateResult
				if err := withRetry(ctx, fmt.Sprintf("UpdateMany empty on %s", c.name), func() error {
					var err error
					n, err = coll.UpdateMany(ctx,
						bson.M{"tenant_id": ""},
						bson.M{"$set": bson.M{"tenant_id": int64(p.value)}},
					)
					return err
				}); err != nil {
					return fmt.Errorf("UpdateMany empty: %w", err)
				}
				log.Printf("  %s: \"\" → %d (matched %d, modified %d, planned %d)", c.name, p.value, n.MatchedCount, n.ModifiedCount, cnt)
			case "delete":
				n, err := coll.DeleteMany(ctx, bson.M{"tenant_id": ""})
				if err != nil {
					return fmt.Errorf("DeleteMany empty: %w", err)
				}
				log.Printf("  %s: deleted %d empty-string docs (planned %d)", c.name, n.DeletedCount, cnt)
			}
		default:
			return fmt.Errorf("unknown value %q in %s (should have been caught earlier)", v, c.name)
		}
	}

	// 3. Rebuild tenant_id indexes from the rebuild specs. The mongo Go driver
	//    does not auto-retry on socket EOF, so wrap CreateOne in withRetry
	//    (the data rewrite's UpdateMany calls above are also wrapped). A
	//    transient connection drop here would otherwise leave the collection
	//    with no tenant_id indexes until the app is restarted.
	for _, s := range rebuildSpecs {
		opts := options.Index().SetName(s.Name)
		if s.Unique {
			opts.SetUnique(true)
		}
		if s.Sparse {
			opts.SetSparse(true)
		}
		if err := withRetry(ctx, fmt.Sprintf("CreateOne index %s on %s", s.Name, c.name), func() error {
			_, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
				Keys:    s.Key,
				Options: opts,
			})
			return err
		}); err != nil {
			return fmt.Errorf("CreateOne index %s: %w", s.Name, err)
		}
		log.Printf("  %s: rebuilt index %s (unique=%v, sparse=%v)", c.name, s.Name, s.Unique, s.Sparse)
	}
	return nil
}

// withRetry runs fn up to 3 times with linear backoff, retrying only on
// transient connection-level errors (EOF, connection closed/reset/refused).
// Non-retryable errors return immediately on the first attempt.
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

// listTenantIDIndexes lists all indexes on the collection whose key spec
// contains "tenant_id". These are the ones we must drop before rewriting data
// (unique constraints) and recreate afterward (all).
func listTenantIDIndexes(ctx context.Context, coll *mongo.Collection) ([]indexSpec, error) {
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var all []indexSpec
	for cur.Next(ctx) {
		var s indexSpec
		if err := cur.Decode(&s); err != nil {
			return nil, err
		}
		has := false
		for _, e := range s.Key {
			if e.Key == "tenant_id" {
				has = true
				break
			}
		}
		if has {
			all = append(all, s)
		}
	}
	return all, cur.Err()
}

// validateCollection runs a $type aggregation over tenant_id and verifies the
// only BSON type present is "long" (int64). Any "string"/"int"/"double"/etc.
// means the migration missed some docs — a silent-failure mode that must not
// pass unflagged.
func validateCollection(ctx context.Context, coll *mongo.Collection, name string) error {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"tenant_id": bson.M{"$exists": true}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "$type", Value: "$tenant_id"}}},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}
	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("Aggregate: %w", err)
	}
	defer cur.Close(ctx)

	type res struct {
		ID string `bson:"_id"`
		N  int64  `bson:"n"`
	}
	var results []res
	if err := cur.All(ctx, &results); err != nil {
		return fmt.Errorf("decode aggregation: %w", err)
	}
	if len(results) == 0 {
		log.Printf("  VALIDATED %-32s: no tenant_id docs (empty collection)", name)
		return nil
	}
	parts := make([]string, 0, len(results))
	bad := false
	for _, r := range results {
		parts = append(parts, fmt.Sprintf("%s=%d", r.ID, r.N))
		if r.ID != "long" {
			bad = true
		}
	}
	log.Printf("  VALIDATED %-32s: %s", name, strings.Join(parts, ", "))
	if bad {
		return fmt.Errorf("non-long BSON types present in tenant_id")
	}
	return nil
}
