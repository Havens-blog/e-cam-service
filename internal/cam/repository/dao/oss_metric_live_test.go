package dao

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// OSS 指标 DAO 活体验证(MONGO_DSN 门控,AC-5)。
// 与 NAS live 测试同型,但用独立测试库 ecam_dao_oss_test + Cleanup 整库 Drop:
// 避免与 nas/cdn live 测试共库时整库 Drop 互相清场,测试期间任何写入都不接触
// 真实业务集合。
const ossMetricTestDB = "ecam_dao_oss_test"

func liveOSSMetricDAO(t *testing.T) (OSSMetricDAO, *mongo.Collection) {
	t.Helper()
	dsn := os.Getenv("MONGO_DSN")
	if dsn == "" {
		t.Skip("set MONGO_DSN to run live check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(dsn))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database(ossMetricTestDB)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_ = db.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})
	return NewOSSMetricDAO(mongox.NewMongo(client, ossMetricTestDB)), db.Collection(OSSMetricCollection)
}

// 唯一索引 (account_id, bucket_name, date)(T2 AC-3):键序、Unique 标记须精确一致;
// 唯一键必须含 account_id(Hard Rule:跨账号共享 bucket 名不得互相覆盖,3cea6d7
// 多账号修复经验)。
func TestOSSMetricUniqueIndexLive(t *testing.T) {
	_, coll := liveOSSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	specs, err := coll.Indexes().ListSpecifications(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	for _, spec := range specs {
		var keys bson.M
		if err := bson.Unmarshal(spec.KeysDocument, &keys); err != nil {
			continue
		}
		if len(keys) != 3 || keys["account_id"] != int32(1) || keys["bucket_name"] != int32(1) || keys["date"] != int32(1) {
			continue
		}
		if spec.Unique == nil || !*spec.Unique {
			t.Fatalf("索引 %v 存在但未标记 Unique: %+v", keys, spec)
		}
		return
	}
	t.Fatalf("未找到 (account_id, bucket_name, date) 唯一索引, specs=%+v", specs)
}

// 多账号同 bucket 名并存(Hard Rule / T2 AC-3):同一 bucket_name 同一天三个不同
// 账号各写一笔,必须各保留一行,不得互相覆盖;账号内同日重写仍幂等。
// 仿 NAS TestNASMetricPerAccountLive(CDN 修复经验回归)。
func TestOSSMetricPerAccountLive(t *testing.T) {
	d, coll := liveOSSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 跨账号共享存储桶(同 bucket 名被多家云账号纳管),同一天各写一笔
	bucketName, date := "test-shared-oss-001", "2026-09-14"
	seeds := []types.OSSMetric{
		{BucketName: bucketName, Date: date, StorageSize: 100, ObjectCount: 10, AccountID: 1, Provider: "aliyun"},
		{BucketName: bucketName, Date: date, StorageSize: 200, ObjectCount: 20, AccountID: 4, Provider: "tencent"},
		{BucketName: bucketName, Date: date, StorageSize: 300, ObjectCount: 30, AccountID: 5, Provider: "volcengine"},
	}
	for _, m := range seeds {
		if err := d.UpsertMetric(ctx, m); err != nil {
			t.Fatalf("upsert acct %d: %v", m.AccountID, err)
		}
	}

	// 三行都必须保留,各账号值正确
	cnt, err := coll.CountDocuments(ctx, bson.M{"bucket_name": bucketName, "date": date})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 3 {
		t.Fatalf("shared bucket rows = %d, want 3 (跨账号互相覆盖 bug)", cnt)
	}
	for _, seed := range seeds {
		var got types.OSSMetric
		filter := bson.M{"bucket_name": bucketName, "date": date, "account_id": seed.AccountID}
		if err := coll.FindOne(ctx, filter).Decode(&got); err != nil {
			t.Fatalf("decode acct%d: %v", seed.AccountID, err)
		}
		if got.StorageSize != seed.StorageSize || got.ObjectCount != seed.ObjectCount || got.Provider != seed.Provider {
			t.Fatalf("acct%d metric = %+v, want storage %v/objects %d (跨账号覆盖丢失)",
				seed.AccountID, got, seed.StorageSize, seed.ObjectCount)
		}
	}

	// 账号内同日重写仍幂等(同一账号同 bucket 同日只留一行,值覆盖)
	update := seeds[1]
	update.StorageSize = 250
	update.ObjectCount = 25
	if err := d.UpsertMetric(ctx, update); err != nil {
		t.Fatalf("re-upsert acct4: %v", err)
	}
	cnt, err = coll.CountDocuments(ctx, bson.M{"bucket_name": bucketName, "date": date})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 3 {
		t.Fatalf("after re-upsert rows = %d, want 3", cnt)
	}
	var got types.OSSMetric
	if err := coll.FindOne(ctx, bson.M{"bucket_name": bucketName, "date": date, "account_id": 4}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.StorageSize != 250 || got.ObjectCount != 25 {
		t.Fatalf("acct4 re-upsert = %v/%v, want 250/25", got.StorageSize, got.ObjectCount)
	}
}

// upsert 幂等 + 零容量异常行落库可见(T2 AC-1/AC-4):
// 同账号同日重采只保留一行(值覆盖为最新),storage_size=0 例外放行打 zero_exception 标。
func TestOSSMetricUpsertLive(t *testing.T) {
	d, coll := liveOSSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	base := types.OSSMetric{
		BucketName: "test-live-oss-1", Date: "2026-09-14",
		StorageSize: 1378.14, ObjectCount: 1024,
		AccountID: 9901, Provider: "aliyun",
	}
	// 1. 首写
	if err := d.UpsertMetric(ctx, base); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	// 2. 同 (account_id, bucket_name, date) 重写覆盖为最新值(DAO 层保证键唯一)
	updated := base
	updated.StorageSize = 1500
	updated.ObjectCount = 2048
	if err := d.UpsertMetric(ctx, updated); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	// 3. 仍只有一行,且值为最新
	cnt, err := coll.CountDocuments(ctx, bson.M{"bucket_name": base.BucketName, "account_id": base.AccountID})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("metric count = %d, want 1 (同账号同日幂等失败)", cnt)
	}
	var got types.OSSMetric
	if err := coll.FindOne(ctx, bson.M{"bucket_name": base.BucketName, "account_id": base.AccountID}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.StorageSize != 1500 || got.ObjectCount != 2048 || got.Date != "2026-09-14" || got.Provider != "aliyun" {
		t.Fatalf("unexpected metric: %+v", got)
	}

	// 4. storage_size=0 例外放行:正常落库且 qc_status=zero_exception 落库可见
	zero := types.OSSMetric{
		BucketName: "test-live-oss-zero", Date: "2026-09-14",
		StorageSize: 0, ObjectCount: 0,
		AccountID: 9901, Provider: "huawei",
	}
	if err := d.UpsertMetric(ctx, zero); err != nil {
		t.Fatalf("upsert zero row: %v (storage_size=0 须例外放行,不拦截不跳过)", err)
	}
	var gotZero types.OSSMetric
	if err := coll.FindOne(ctx, bson.M{"bucket_name": zero.BucketName}).Decode(&gotZero); err != nil {
		t.Fatalf("decode zero row: %v", err)
	}
	if gotZero.QcStatus != types.OSSMetricQcZeroException {
		t.Fatalf("zero row qc_status = %q, want zero_exception (异常行须落库可见)", gotZero.QcStatus)
	}

	// 5. 唯一索引真实拦截重复键(绕过 upsert 的裸 InsertOne 必须 dup-key 报错)
	dup := bson.M{"account_id": base.AccountID, "bucket_name": base.BucketName, "date": base.Date, "storage_size": 1.0}
	if _, err := coll.InsertOne(ctx, dup); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("want duplicate key error for %v, got %v (唯一索引未生效)", dup, err)
	}
}

// 首写生效 vs 覆盖更新活体验证(T2 AC-3 今日行不覆盖/昨日行补采覆盖):
//   - BulkInsertIfAbsent 同键二次写入:首次值保留(已有行不被覆盖,不产生脏行);
//   - BulkUpsertMetrics 同键二次写入:值覆盖为最新(次日补采昨日行的语义)。
func TestOSSMetricInsertIfAbsentLive(t *testing.T) {
	d, coll := liveOSSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	base := types.OSSMetric{
		BucketName: "test-insert-absent-oss-1", Date: "2026-09-14",
		StorageSize: 100, ObjectCount: 10,
		AccountID: 9903, Provider: "aliyun",
	}
	// 1. 首写:插入成功
	if err := d.BulkInsertIfAbsent(ctx, []types.OSSMetric{base}); err != nil {
		t.Fatalf("insert-if-absent first: %v", err)
	}
	// 2. 同键二次首写:不得覆盖已有行(首写生效)
	retry := base
	retry.StorageSize = 999
	retry.ObjectCount = 99
	if err := d.BulkInsertIfAbsent(ctx, []types.OSSMetric{retry}); err != nil {
		t.Fatalf("insert-if-absent second: %v", err)
	}
	var got types.OSSMetric
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "bucket_name": base.BucketName, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.StorageSize != 100 || got.ObjectCount != 10 {
		t.Fatalf("首写生效失败,二次写入覆盖了已有行: storage=%v objects=%d (want 100/10)", got.StorageSize, got.ObjectCount)
	}
	// 行数仍为 1(同日重复采集不产生脏行)
	cnt, err := coll.CountDocuments(ctx, bson.M{"account_id": base.AccountID, "bucket_name": base.BucketName})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("rows = %d, want 1 (首写批重放产生脏行)", cnt)
	}

	// 3. 覆盖更新路径(BulkUpsertMetrics)同键重写:值覆盖为最新(昨日补采语义)
	override := base
	override.StorageSize = 250
	override.ObjectCount = 25
	if err := d.BulkUpsertMetrics(ctx, []types.OSSMetric{override}); err != nil {
		t.Fatalf("bulk upsert override: %v", err)
	}
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "bucket_name": base.BucketName, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode override: %v", err)
	}
	if got.StorageSize != 250 || got.ObjectCount != 25 {
		t.Fatalf("覆盖更新失败: storage=%v objects=%d (want 250/25)", got.StorageSize, got.ObjectCount)
	}

	// 4. 首写批与覆盖批互不串扰:覆盖后再次首写仍不回退已有值
	if err := d.BulkInsertIfAbsent(ctx, []types.OSSMetric{retry}); err != nil {
		t.Fatalf("insert-if-absent after override: %v", err)
	}
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "bucket_name": base.BucketName, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode final: %v", err)
	}
	if got.StorageSize != 250 {
		t.Fatalf("覆盖后首写批误改已有行: storage=%v (want 250)", got.StorageSize)
	}
}

// 批量写入活体验证:单批多条一次落库、同键二次批量重放幂等不重复(值覆盖)、
// 零容量行整批内照常落库并打标。
func TestOSSMetricBulkUpsertLive(t *testing.T) {
	d, coll := liveOSSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	batch := []types.OSSMetric{
		{BucketName: "test-bulk-oss-1", Date: "2026-09-14", StorageSize: 100, ObjectCount: 10, AccountID: 9902, Provider: "aliyun"},
		{BucketName: "test-bulk-oss-1", Date: "2026-09-15", StorageSize: 110, ObjectCount: 11, AccountID: 9902, Provider: "aliyun"},
		{BucketName: "test-bulk-oss-1", Date: "2026-09-16", StorageSize: 120, ObjectCount: 12, AccountID: 9902, Provider: "aliyun"},
		{BucketName: "test-bulk-oss-zero", Date: "2026-09-16", StorageSize: 0, ObjectCount: 0, AccountID: 9902, Provider: "aws"},
	}
	if err := d.BulkUpsertMetrics(ctx, batch); err != nil {
		t.Fatalf("bulk upsert: %v", err)
	}
	cnt, err := coll.CountDocuments(ctx, bson.M{"account_id": 9902})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 4 {
		t.Fatalf("bulk rows = %d, want 4", cnt)
	}
	var gotZero types.OSSMetric
	if err := coll.FindOne(ctx, bson.M{"bucket_name": "test-bulk-oss-zero"}).Decode(&gotZero); err != nil {
		t.Fatal(err)
	}
	if gotZero.QcStatus != types.OSSMetricQcZeroException {
		t.Fatalf("bulk zero row qc_status = %q, want zero_exception", gotZero.QcStatus)
	}

	// 同键二次批量重放(值覆盖,行数不增)
	replay := []types.OSSMetric{
		{BucketName: "test-bulk-oss-1", Date: "2026-09-14", StorageSize: 101, ObjectCount: 10, AccountID: 9902, Provider: "aliyun"},
		{BucketName: "test-bulk-oss-1", Date: "2026-09-15", StorageSize: 111, ObjectCount: 11, AccountID: 9902, Provider: "aliyun"},
		{BucketName: "test-bulk-oss-1", Date: "2026-09-16", StorageSize: 121, ObjectCount: 12, AccountID: 9902, Provider: "aliyun"},
		{BucketName: "test-bulk-oss-zero", Date: "2026-09-16", StorageSize: 0, ObjectCount: 0, AccountID: 9902, Provider: "aws"},
	}
	if err := d.BulkUpsertMetrics(ctx, replay); err != nil {
		t.Fatalf("bulk replay: %v", err)
	}
	cnt, err = coll.CountDocuments(ctx, bson.M{"account_id": 9902})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 4 {
		t.Fatalf("after replay rows = %d, want 4 (批量重放幂等失败)", cnt)
	}
	var got types.OSSMetric
	if err := coll.FindOne(ctx, bson.M{"bucket_name": "test-bulk-oss-1", "date": "2026-09-16"}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.StorageSize != 121 {
		t.Fatalf("replayed storage = %v, want 121", got.StorageSize)
	}
}
