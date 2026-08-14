//go:build ignore
// +build ignore

// Probe: verify the prod app has stopped inserting string tenant_id docs
// into ecam_instance before running the migration apply.
//
// It counts ecam_instance docs where tenant_id is $type "string" twice with
// a 40-second gap, prints the string/long split, and lists current indexes.
// If the string count is still growing, prod is still running → BLOCKED.
//
// Usage:
//   go run scripts/prod_stopped_probe.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
	gap      = flag.Duration("gap", 40*time.Second, "gap between the two counts")
)

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Printf("==========================================================")
	log.Printf("prod-stopped probe for ecam_instance")
	log.Printf("URI       : %s", *mongoURI)
	log.Printf("Database  : %s", *database)
	log.Printf("User      : %s", *username)
	log.Printf("Gap       : %s", *gap)
	log.Printf("==========================================================")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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

	coll := client.Database(*database).Collection("ecam_instance")

	// ---- Initial split (string vs long vs other) ----
	printSplit(ctx, coll, "INITIAL")

	// ---- Index list ----
	printIndexes(ctx, coll)

	// ---- Two counts with gap ----
	c1 := countType(ctx, coll, "string")
	log.Printf("COUNT #1 (string tenant_id): %d at %s", c1, time.Now().Format(time.RFC3339Nano))
	log.Printf("sleeping %s before second count...", *gap)
	select {
	case <-ctx.Done():
		log.Fatalf("ctx cancelled during gap: %v", ctx.Err())
	case <-time.After(*gap):
	}
	c2 := countType(ctx, coll, "string")
	log.Printf("COUNT #2 (string tenant_id): %d at %s", c2, time.Now().Format(time.RFC3339Nano))

	log.Printf("----------------------------------------------------------")
	delta := c2 - c1
	switch {
	case delta > 0:
		log.Printf("VERDICT: BLOCKED — string count grew by %d in %s; prod is STILL RUNNING", delta, *gap)
	case delta < 0:
		log.Printf("VERDICT: OK — string count shrank by %d in %s; prod is stopped (or migration is running)", -delta, *gap)
	default:
		log.Printf("VERDICT: OK — string count stable; prod appears STOPPED")
	}
	log.Printf("----------------------------------------------------------")

	// Final split for the record.
	printSplit(ctx, coll, "FINAL")
}

func countType(ctx context.Context, coll *mongo.Collection, bsonType string) int64 {
	n, err := coll.CountDocuments(ctx, bson.M{"tenant_id": bson.M{"$type": bsonType}})
	if err != nil {
		log.Fatalf("CountDocuments type=%s: %v", bsonType, err)
	}
	return n
}

func printSplit(ctx context.Context, coll *mongo.Collection, label string) {
	cur, err := coll.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"tenant_id": bson.M{"$exists": true}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "$type", Value: "$tenant_id"}}},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	})
	if err != nil {
		log.Fatalf("Aggregate split (%s): %v", label, err)
	}
	defer cur.Close(ctx)
	type res struct {
		ID string `bson:"_id"`
		N  int64  `bson:"n"`
	}
	var rs []res
	if err := cur.All(ctx, &rs); err != nil {
		log.Fatalf("decode split (%s): %v", label, err)
	}
	total, err := coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("total count: %v", err)
	}
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("%s=%d", r.ID, r.N))
	}
	log.Printf("[%-7s split] total docs=%d | tenant_id types: %s", label, total, strings.Join(parts, ", "))
}

func printIndexes(ctx context.Context, coll *mongo.Collection) {
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		log.Fatalf("index list: %v", err)
	}
	defer cur.Close(ctx)
	type idx struct {
		Name   string `bson:"name"`
		Key    bson.D `bson:"key"`
		Unique bool   `bson:"unique"`
		Sparse bool   `bson:"sparse"`
	}
	var idxs []idx
	if err := cur.All(ctx, &idxs); err != nil {
		log.Fatalf("decode indexes: %v", err)
	}
	sort.Slice(idxs, func(i, j int) bool { return idxs[i].Name < idxs[j].Name })
	log.Printf("---- current indexes on ecam_instance (%d) ----", len(idxs))
	for _, ix := range idxs {
		keys := make([]string, 0, len(ix.Key))
		for _, e := range ix.Key {
			keys = append(keys, fmt.Sprintf("%s=%v", e.Key, e.Value))
		}
		flags := ""
		if ix.Unique {
			flags += " unique"
		}
		if ix.Sparse {
			flags += " sparse"
		}
		log.Printf("  - %-50s {%s}%s", ix.Name, strings.Join(keys, ", "), flags)
	}
	log.Printf("----")
}
