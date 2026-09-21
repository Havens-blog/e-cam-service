package dao

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Disk 指标 DAO 活体验证(MONGO_DSN 门控)。
// 用独立测试库 ecam_dao_disk_test + Cleanup 整库 Drop:测试期间的任何写入
// (含唯一索引创建)都不接触真实业务集合。
const diskMetricTestDB = "ecam_dao_disk_test"

func liveDiskMetricDAO(t *testing.T) (DiskMetricDAO, *mongo.Collection) {
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
	db := client.Database(diskMetricTestDB)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_ = db.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})
	return NewDiskMetricDAO(mongox.NewMongo(client, diskMetricTestDB)), db.Collection(DiskMetricCollection)
}

// 唯一索引 (account_id, disk_id, date)(T2 AC-3):键序、Unique 标记须精确一致;
// 唯一键必须含 account_id(多账号同盘/共享盘并存,Hard Rule,3cea6d7 修复经验)。
func TestDiskMetricUniqueIndexLive(t *testing.T) {
	_, coll := liveDiskMetricDAO(t)
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
		if len(keys) != 3 || keys["account_id"] != int32(1) || keys["disk_id"] != int32(1) || keys["date"] != int32(1) {
			continue
		}
		if spec.Unique == nil || !*spec.Unique {
			t.Fatalf("索引 %v 存在但未标记 Unique: %+v", keys, spec)
		}
		return
	}
	t.Fatalf("未找到 (account_id, disk_id, date) 唯一索引, specs=%+v", specs)
}

// upsert 幂等 + 零使用率异常行落库可见(T2 AC-4/AC-5):
// 同账号同日重采只保留一行(值覆盖为最新),usage_percent=0 例外放行打 zero_exception 标。
func TestDiskMetricLive(t *testing.T) {
	d, coll := liveDiskMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	base := types.DiskMetric{
		DiskID: "test-live-disk-1", DiskName: "live测试盘", Date: "2026-09-14",
		UsagePercent: 60.693, UsageScope: types.DiskUsageScopeInstanceLevel,
		IOPS: 8.256, Throughput: 0.117,
		AccountID: 9901, Provider: "aliyun",
	}
	// 1. 首写
	if err := d.UpsertMetric(ctx, base); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	// 2. 同 (account_id, disk_id, date) 重写覆盖为最新值(DAO 层保证键唯一)
	updated := base
	updated.UsagePercent = 88.649
	updated.IOPS = 12.5
	if err := d.UpsertMetric(ctx, updated); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	// 3. 仍只有一行,且值为最新
	cnt, err := coll.CountDocuments(ctx, bson.M{"disk_id": base.DiskID})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("metric count = %d, want 1 (同账号同日幂等失败)", cnt)
	}
	var got types.DiskMetric
	if err := coll.FindOne(ctx, bson.M{"disk_id": base.DiskID}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UsagePercent != 88.649 || got.IOPS != 12.5 || got.Date != "2026-09-14" || got.Provider != "aliyun" {
		t.Fatalf("unexpected metric: %+v", got)
	}

	// 4. usage_percent=0 例外放行:正常落库且 qc_status=zero_exception 落库可见
	zero := types.DiskMetric{
		DiskID: "test-live-disk-zero", DiskName: "live零值盘", Date: "2026-09-14",
		UsagePercent: 0, IOPS: 0, Throughput: 0,
		AccountID: 9901, Provider: "aws",
	}
	if err := d.UpsertMetric(ctx, zero); err != nil {
		t.Fatalf("upsert zero row: %v (usage_percent=0 须例外放行,不拦截不跳过)", err)
	}
	var gotZero types.DiskMetric
	if err := coll.FindOne(ctx, bson.M{"disk_id": zero.DiskID}).Decode(&gotZero); err != nil {
		t.Fatalf("decode zero row: %v", err)
	}
	if gotZero.QcStatus != types.DiskMetricQcZeroException {
		t.Fatalf("zero row qc_status = %q, want zero_exception (异常行须落库可见)", gotZero.QcStatus)
	}

	// 5. 唯一索引真实拦截重复键(绕过 upsert 的裸 InsertOne 必须 dup-key 报错)
	dup := bson.M{"account_id": base.AccountID, "disk_id": base.DiskID, "date": base.Date, "usage_percent": 1.0}
	if _, err := coll.InsertOne(ctx, dup); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("want duplicate key error for %v, got %v (唯一索引未生效)", dup, err)
	}
}

// 回归:同一云盘可由多个云账号纳管(共享盘/多重挂载),(account_id, disk_id, date)
// 才是唯一键——不同账号写同盘同日,必须各保留一行,不得互相覆盖(Hard Rule)。
// 仿 NAS TestNASMetricPerAccountLive(T2 AC-5)。
func TestDiskMetricPerAccountLive(t *testing.T) {
	d, coll := liveDiskMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 同一盘、同一天,三个不同账号(阿里/华为/AWS)各写一笔
	diskID, date := "test-shared-disk-001", "2026-09-14"
	seeds := []types.DiskMetric{
		{DiskID: diskID, DiskName: "共享盘", Date: date, UsagePercent: 60.693, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 10, Throughput: 1, AccountID: 1, Provider: "aliyun"},
		{DiskID: diskID, DiskName: "共享盘", Date: date, UsagePercent: 41.79, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 20, Throughput: 2, AccountID: 2, Provider: "huawei"},
		{DiskID: diskID, DiskName: "共享盘", Date: date, UsagePercent: 19.47, UsageScope: types.DiskUsageScopeBusyShare, IOPS: 30, Throughput: 3, AccountID: 3, Provider: "aws"},
	}
	for _, m := range seeds {
		if err := d.UpsertMetric(ctx, m); err != nil {
			t.Fatalf("upsert acct %d: %v", m.AccountID, err)
		}
	}

	// 三行都必须保留,各账号值正确
	cnt, err := coll.CountDocuments(ctx, bson.M{"disk_id": diskID, "date": date})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 3 {
		t.Fatalf("shared disk rows = %d, want 3 (跨账号互相覆盖 bug)", cnt)
	}
	for _, seed := range seeds {
		var got types.DiskMetric
		filter := bson.M{"disk_id": diskID, "date": date, "account_id": seed.AccountID}
		if err := coll.FindOne(ctx, filter).Decode(&got); err != nil {
			t.Fatalf("decode acct%d: %v", seed.AccountID, err)
		}
		if got.UsagePercent != seed.UsagePercent || got.Provider != seed.Provider {
			t.Fatalf("acct%d metric = %+v, want usage %v (跨账号覆盖丢失)", seed.AccountID, got, seed.UsagePercent)
		}
	}

	// 账号内同日重写仍幂等(同一账号同盘同日只留一行,值覆盖)
	update := seeds[1]
	update.UsagePercent = 50.5
	if err := d.UpsertMetric(ctx, update); err != nil {
		t.Fatalf("re-upsert acct2: %v", err)
	}
	cnt, err = coll.CountDocuments(ctx, bson.M{"disk_id": diskID, "date": date})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 3 {
		t.Fatalf("after re-upsert rows = %d, want 3", cnt)
	}
	var got types.DiskMetric
	if err := coll.FindOne(ctx, bson.M{"disk_id": diskID, "date": date, "account_id": 2}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.UsagePercent != 50.5 {
		t.Fatalf("acct2 re-upsert usage = %v, want 50.5", got.UsagePercent)
	}
}

// 首写生效 vs 覆盖更新活体验证(T2 AC-3:今日行首写生效不覆盖/昨日行补采覆盖更新):
//   - BulkInsertIfAbsent 同键二次写入:首次值保留(已有行不被覆盖,不产生脏行);
//   - BulkUpsertMetrics 同键二次写入:值覆盖为最新(次日补采昨日行的语义)。
func TestDiskMetricInsertIfAbsentLive(t *testing.T) {
	d, coll := liveDiskMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	base := types.DiskMetric{
		DiskID: "test-insert-absent-disk-1", DiskName: "首写生效盘", Date: "2026-09-14",
		UsagePercent: 41.79, UsageScope: types.DiskUsageScopeInstanceLevel,
		IOPS: 1, Throughput: 0.1,
		AccountID: 9903, Provider: "huawei",
	}
	// 1. 首写:插入成功
	if err := d.BulkInsertIfAbsent(ctx, []types.DiskMetric{base}); err != nil {
		t.Fatalf("insert-if-absent first: %v", err)
	}
	// 2. 同键二次首写:不得覆盖已有行(首写生效)
	retry := base
	retry.UsagePercent = 99
	retry.IOPS = 99
	if err := d.BulkInsertIfAbsent(ctx, []types.DiskMetric{retry}); err != nil {
		t.Fatalf("insert-if-absent second: %v", err)
	}
	var got types.DiskMetric
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "disk_id": base.DiskID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UsagePercent != 41.79 || got.IOPS != 1 {
		t.Fatalf("首写生效失败,二次写入覆盖了已有行: usage=%v iops=%v (want 41.79/1)", got.UsagePercent, got.IOPS)
	}
	// 行数仍为 1(同日重复采集不产生脏行)
	cnt, err := coll.CountDocuments(ctx, bson.M{"account_id": base.AccountID, "disk_id": base.DiskID})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("rows = %d, want 1 (首写批重放产生脏行)", cnt)
	}

	// 3. 覆盖更新路径(BulkUpsertMetrics)同键重写:值覆盖为最新(昨日补采语义)
	override := base
	override.UsagePercent = 55
	override.Throughput = 0.2
	if err := d.BulkUpsertMetrics(ctx, []types.DiskMetric{override}); err != nil {
		t.Fatalf("bulk upsert override: %v", err)
	}
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "disk_id": base.DiskID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode override: %v", err)
	}
	if got.UsagePercent != 55 || got.Throughput != 0.2 {
		t.Fatalf("覆盖更新失败: usage=%v throughput=%v (want 55/0.2)", got.UsagePercent, got.Throughput)
	}

	// 4. 首写批与覆盖批互不串扰:覆盖后再次首写仍不回退已有值
	if err := d.BulkInsertIfAbsent(ctx, []types.DiskMetric{retry}); err != nil {
		t.Fatalf("insert-if-absent after override: %v", err)
	}
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "disk_id": base.DiskID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode final: %v", err)
	}
	if got.UsagePercent != 55 {
		t.Fatalf("覆盖后首写批误改已有行: usage=%v (want 55)", got.UsagePercent)
	}
}

// 批量写入活体验证:单批多条一次落库、同键二次批量重放幂等不重复(值覆盖)、
// 零使用率行整批内照常落库并打标。
func TestDiskMetricBulkUpsertLive(t *testing.T) {
	d, coll := liveDiskMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	batch := []types.DiskMetric{
		{DiskID: "test-bulk-disk-1", Date: "2026-09-14", UsagePercent: 10, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 1, Throughput: 0.1, AccountID: 9902, Provider: "aliyun"},
		{DiskID: "test-bulk-disk-1", Date: "2026-09-15", UsagePercent: 20, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 2, Throughput: 0.2, AccountID: 9902, Provider: "aliyun"},
		{DiskID: "test-bulk-disk-1", Date: "2026-09-16", UsagePercent: 30, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 3, Throughput: 0.3, AccountID: 9902, Provider: "aliyun"},
		{DiskID: "test-bulk-disk-zero", Date: "2026-09-16", UsagePercent: 0, IOPS: 0, Throughput: 0, AccountID: 9902, Provider: "aws"},
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
	var gotZero types.DiskMetric
	if err := coll.FindOne(ctx, bson.M{"disk_id": "test-bulk-disk-zero"}).Decode(&gotZero); err != nil {
		t.Fatal(err)
	}
	if gotZero.QcStatus != types.DiskMetricQcZeroException {
		t.Fatalf("bulk zero row qc_status = %q, want zero_exception", gotZero.QcStatus)
	}

	// 同键二次批量重放(值覆盖,行数不增)
	replay := []types.DiskMetric{
		{DiskID: "test-bulk-disk-1", Date: "2026-09-14", UsagePercent: 11, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 1.1, Throughput: 0.11, AccountID: 9902, Provider: "aliyun"},
		{DiskID: "test-bulk-disk-1", Date: "2026-09-15", UsagePercent: 21, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 2.1, Throughput: 0.21, AccountID: 9902, Provider: "aliyun"},
		{DiskID: "test-bulk-disk-1", Date: "2026-09-16", UsagePercent: 31, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 3.1, Throughput: 0.31, AccountID: 9902, Provider: "aliyun"},
		{DiskID: "test-bulk-disk-zero", Date: "2026-09-16", UsagePercent: 0, IOPS: 0, Throughput: 0, AccountID: 9902, Provider: "aws"},
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
	var got types.DiskMetric
	if err := coll.FindOne(ctx, bson.M{"disk_id": "test-bulk-disk-1", "date": "2026-09-16"}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.UsagePercent != 31 {
		t.Fatalf("replayed usage = %v, want 31", got.UsagePercent)
	}
}
