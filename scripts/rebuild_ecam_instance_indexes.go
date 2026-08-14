//go:build ignore
// +build ignore

// Rebuild the tenant_id indexes on ecam_instance that were dropped during the
// tenant_id migration and could not be rebuilt until duplicates were removed.
//
// Specs are transcribed verbatim from internal/cam/repository/dao/init.go
// (initInstanceIndexes). Only the two tenant_id indexes that were dropped are
// recreated here; the other 7 ecam_instance indexes were never touched.
//
//   1. tenant_id_1_model_uid_1_asset_id_1  {tenant_id:1, model_uid:1, asset_id:1}  unique
//   2. tenant_id_1                          {tenant_id:1}
//
// Usage
//   go run scripts/rebuild_ecam_instance_indexes.go
//
// (No -apply gate: CreateOne on an identical existing index is a no-op, so this
// script is idempotent by construction. It will FAIL loudly if duplicates still
// exist, which is exactly the safety we want.)
package main

import (
	"context"
	"flag"
	"log"
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
)

const collectionName = "ecam_instance"

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Printf("==========================================================")
	log.Printf("rebuild ecam_instance tenant_id indexes")
	log.Printf("URI       : %s", *mongoURI)
	log.Printf("Database  : %s", *database)
	log.Printf("User      : %s", *username)
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

	coll := client.Database(*database).Collection(collectionName)

	// Specs transcribed from internal/cam/repository/dao/init.go:initInstanceIndexes.
	// Only the two whose key contains tenant_id — those are the ones the
	// migration dropped. unique/sparse match the source exactly.
	specs := []struct {
		name   string
		keys   bson.D
		unique bool
		sparse bool
	}{
		{
			name:   "tenant_id_1_model_uid_1_asset_id_1",
			keys:   bson.D{{Key: "tenant_id", Value: 1}, {Key: "model_uid", Value: 1}, {Key: "asset_id", Value: 1}},
			unique: true,
		},
		{
			name: "tenant_id_1",
			keys: bson.D{{Key: "tenant_id", Value: 1}},
		},
	}

	anyFailed := false
	for _, s := range specs {
		opts := options.Index().SetName(s.name)
		if s.unique {
			opts.SetUnique(true)
		}
		if s.sparse {
			opts.SetSparse(true)
		}
		err := withRetry(ctx, "CreateOne "+s.name, func() error {
			_, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
				Keys:    s.keys,
				Options: opts,
			})
			return err
		})
		if err != nil {
			// Most likely cause: lingering duplicate on the unique key. Stop
			// and report — do NOT continue with the rest.
			log.Printf("FAIL create index %s: %v", s.name, err)
			anyFailed = true
			break
		}
		log.Printf("  rebuilt index %s (unique=%v, sparse=%v)", s.name, s.unique, s.sparse)
	}
	if anyFailed {
		log.Fatalf("index rebuild failed — investigate before proceeding")
	}

	// List indexes to confirm.
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		log.Fatalf("list indexes: %v", err)
	}
	defer cur.Close(ctx)
	type idx struct {
		Name   string `bson:"name"`
		Key    bson.D `bson:"key"`
		Unique bool   `bson:"unique"`
	}
	var idxs []idx
	if err := cur.All(ctx, &idxs); err != nil {
		log.Fatalf("decode index list: %v", err)
	}
	log.Printf("---- final indexes on ecam_instance (%d) ----", len(idxs))
	for _, ix := range idxs {
		parts := make([]string, 0, len(ix.Key))
		for _, e := range ix.Key {
			parts = append(parts, e.Key)
		}
		flags := ""
		if ix.Unique {
			flags += " unique"
		}
		log.Printf("  - %-44s {%s}%s", ix.Name, strings.Join(parts, ","), flags)
	}
}

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
