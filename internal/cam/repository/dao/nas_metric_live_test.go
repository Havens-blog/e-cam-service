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

// NAS 指标 DAO 活体验证(MONGO_DSN 门控)。
// 与 CDN live 测试直接写 ecam 库不同,这里用独立测试库 ecam_dao_test + Cleanup
// 整库 Drop:测试期间的任何写入(含唯一索引创建)都不接触真实业务集合。
const nasMetricTestDB = "ecam_dao_test"

func liveNASMetricDAO(t *testing.T) (NASMetricDAO, *mongo.Collection) {
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
	db := client.Database(nasMetricTestDB)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_ = db.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})
	return NewNASMetricDAO(mongox.NewMongo(client, nasMetricTestDB)), db.Collection(NASMetricCollection)
}

// 唯一索引 (account_id, fs_id, date)(T2 AC-3):键序、Unique 标记须精确一致;
// 唯一键必须含 account_id(多账号同 fs 并存,复用 CDN 修复经验)。
func TestNASMetricUniqueIndexLive(t *testing.T) {
	_, coll := liveNASMetricDAO(t)
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
		if len(keys) != 3 || keys["account_id"] != int32(1) || keys["fs_id"] != int32(1) || keys["date"] != int32(1) {
			continue
		}
		if spec.Unique == nil || !*spec.Unique {
			t.Fatalf("索引 %v 存在但未标记 Unique: %+v", keys, spec)
		}
		return
	}
	t.Fatalf("未找到 (account_id, fs_id, date) 唯一索引, specs=%+v", specs)
}

// upsert 幂等 + 零容量异常行落库可见(T2 AC-4/AC-5):
// 同账号同日重采只保留一行(值覆盖为最新),capacity=0 例外放行打 zero_exception 标。
func TestNASMetricLive(t *testing.T) {
	d, coll := liveNASMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	base := types.NASMetric{
		FsID: "test-live-nas-1", FsName: "live测试fs", Date: "2026-09-14",
		Capacity: 2457.32, UsedCapacity: 1378.14,
		AccountID: 9901, Provider: "huawei",
	}
	// 1. 首写
	if err := d.UpsertMetric(ctx, base); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	// 2. 同 (account_id, fs_id, date) 重写覆盖为最新值(DAO 层保证键唯一)
	updated := base
	updated.Capacity = 2500
	updated.UsedCapacity = 1400
	if err := d.UpsertMetric(ctx, updated); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	// 3. 仍只有一行,且值为最新
	cnt, err := coll.CountDocuments(ctx, bson.M{"fs_id": base.FsID})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("metric count = %d, want 1 (同账号同日幂等失败)", cnt)
	}
	var got types.NASMetric
	if err := coll.FindOne(ctx, bson.M{"fs_id": base.FsID}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Capacity != 2500 || got.UsedCapacity != 1400 || got.Date != "2026-09-14" || got.Provider != "huawei" {
		t.Fatalf("unexpected metric: %+v", got)
	}

	// 4. capacity=0 例外放行:正常落库且 qc_status=zero_exception 落库可见
	zero := types.NASMetric{
		FsID: "test-live-nas-zero", FsName: "live零值fs", Date: "2026-09-14",
		Capacity: 0, UsedCapacity: 0,
		AccountID: 9901, Provider: "aws",
	}
	if err := d.UpsertMetric(ctx, zero); err != nil {
		t.Fatalf("upsert zero row: %v (capacity=0 须例外放行,不拦截不跳过)", err)
	}
	var gotZero types.NASMetric
	if err := coll.FindOne(ctx, bson.M{"fs_id": zero.FsID}).Decode(&gotZero); err != nil {
		t.Fatalf("decode zero row: %v", err)
	}
	if gotZero.QcStatus != types.NASMetricQcZeroException {
		t.Fatalf("zero row qc_status = %q, want zero_exception (异常行须落库可见)", gotZero.QcStatus)
	}

	// 5. 唯一索引真实拦截重复键(绕过 upsert 的裸 InsertOne 必须 dup-key 报错)
	dup := bson.M{"account_id": base.AccountID, "fs_id": base.FsID, "date": base.Date, "capacity": 1.0}
	if _, err := coll.InsertOne(ctx, dup); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("want duplicate key error for %v, got %v (唯一索引未生效)", dup, err)
	}
}

// 回归:同一 fs 可由多个云账号纳管(多活/共享实例),(account_id, fs_id, date)
// 才是唯一键——不同账号写同 fs 同日,必须各保留一行,不得互相覆盖。
// 仿 CDN TestCDNMetricPerAccountLive(T2 AC-5)。
func TestNASMetricPerAccountLive(t *testing.T) {
	d, coll := liveNASMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 同一 fs、同一天,三个不同账号(阿里/腾讯/火山)各写一笔
	fsID, date := "test-shared-nas-001", "2026-09-14"
	seeds := []types.NASMetric{
		{FsID: fsID, FsName: "共享fs", Date: date, Capacity: 100, UsedCapacity: 10, AccountID: 1, Provider: "aliyun"},
		{FsID: fsID, FsName: "共享fs", Date: date, Capacity: 200, UsedCapacity: 20, AccountID: 4, Provider: "tencent"},
		{FsID: fsID, FsName: "共享fs", Date: date, Capacity: 300, UsedCapacity: 30, AccountID: 5, Provider: "volcengine"},
	}
	for _, m := range seeds {
		if err := d.UpsertMetric(ctx, m); err != nil {
			t.Fatalf("upsert acct %d: %v", m.AccountID, err)
		}
	}

	// 三行都必须保留,各账号值正确
	cnt, err := coll.CountDocuments(ctx, bson.M{"fs_id": fsID, "date": date})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 3 {
		t.Fatalf("shared fs rows = %d, want 3 (跨账号互相覆盖 bug)", cnt)
	}
	for _, seed := range seeds {
		var got types.NASMetric
		filter := bson.M{"fs_id": fsID, "date": date, "account_id": seed.AccountID}
		if err := coll.FindOne(ctx, filter).Decode(&got); err != nil {
			t.Fatalf("decode acct%d: %v", seed.AccountID, err)
		}
		if got.Capacity != seed.Capacity || got.Provider != seed.Provider {
			t.Fatalf("acct%d metric = %+v, want capacity %v (跨账号覆盖丢失)", seed.AccountID, got, seed.Capacity)
		}
	}

	// 账号内同日重写仍幂等(同一账号同 fs 同日只留一行,值覆盖)
	update := seeds[1]
	update.Capacity = 250
	if err := d.UpsertMetric(ctx, update); err != nil {
		t.Fatalf("re-upsert acct4: %v", err)
	}
	cnt, err = coll.CountDocuments(ctx, bson.M{"fs_id": fsID, "date": date})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 3 {
		t.Fatalf("after re-upsert rows = %d, want 3", cnt)
	}
	var got types.NASMetric
	if err := coll.FindOne(ctx, bson.M{"fs_id": fsID, "date": date, "account_id": 4}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Capacity != 250 {
		t.Fatalf("acct4 re-upsert capacity = %v, want 250", got.Capacity)
	}
}

// 首写生效 vs 覆盖更新活体验证(T5 AC:今日行首写生效/昨日行覆盖更新):
//   - BulkInsertIfAbsent 同键二次写入:首次值保留(已有行不被覆盖,不产生脏行);
//   - BulkUpsertMetrics 同键二次写入:值覆盖为最新(次日补采昨日行的语义)。
func TestNASMetricInsertIfAbsentLive(t *testing.T) {
	d, coll := liveNASMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	base := types.NASMetric{
		FsID: "test-insert-absent-nas-1", FsName: "首写生效fs", Date: "2026-09-14",
		Capacity: 100, UsedCapacity: 10,
		AccountID: 9903, Provider: "aliyun",
	}
	// 1. 首写:插入成功
	if err := d.BulkInsertIfAbsent(ctx, []types.NASMetric{base}); err != nil {
		t.Fatalf("insert-if-absent first: %v", err)
	}
	// 2. 同键二次首写:不得覆盖已有行(首写生效)
	retry := base
	retry.Capacity = 999
	retry.UsedCapacity = 99
	if err := d.BulkInsertIfAbsent(ctx, []types.NASMetric{retry}); err != nil {
		t.Fatalf("insert-if-absent second: %v", err)
	}
	var got types.NASMetric
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "fs_id": base.FsID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Capacity != 100 || got.UsedCapacity != 10 {
		t.Fatalf("首写生效失败,二次写入覆盖了已有行: capacity=%v used=%v (want 100/10)", got.Capacity, got.UsedCapacity)
	}
	// 行数仍为 1(同日重复采集不产生脏行)
	cnt, err := coll.CountDocuments(ctx, bson.M{"account_id": base.AccountID, "fs_id": base.FsID})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("rows = %d, want 1 (首写批重放产生脏行)", cnt)
	}

	// 3. 覆盖更新路径(BulkUpsertMetrics)同键重写:值覆盖为最新(昨日补采语义)
	override := base
	override.Capacity = 250
	override.UsedCapacity = 25
	if err := d.BulkUpsertMetrics(ctx, []types.NASMetric{override}); err != nil {
		t.Fatalf("bulk upsert override: %v", err)
	}
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "fs_id": base.FsID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode override: %v", err)
	}
	if got.Capacity != 250 || got.UsedCapacity != 25 {
		t.Fatalf("覆盖更新失败: capacity=%v used=%v (want 250/25)", got.Capacity, got.UsedCapacity)
	}

	// 4. 首写批与覆盖批互不串扰:覆盖后再次首写仍不回退已有值
	if err := d.BulkInsertIfAbsent(ctx, []types.NASMetric{retry}); err != nil {
		t.Fatalf("insert-if-absent after override: %v", err)
	}
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "fs_id": base.FsID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode final: %v", err)
	}
	if got.Capacity != 250 {
		t.Fatalf("覆盖后首写批误改已有行: capacity=%v (want 250)", got.Capacity)
	}
}

// 批量写入活体验证:单批多条一次落库、同键二次批量重放幂等不重复(值覆盖)、
// 零容量行整批内照常落库并打标。
func TestNASMetricBulkUpsertLive(t *testing.T) {
	d, coll := liveNASMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	batch := []types.NASMetric{
		{FsID: "test-bulk-nas-1", Date: "2026-09-14", Capacity: 100, UsedCapacity: 10, AccountID: 9902, Provider: "aliyun"},
		{FsID: "test-bulk-nas-1", Date: "2026-09-15", Capacity: 110, UsedCapacity: 20, AccountID: 9902, Provider: "aliyun"},
		{FsID: "test-bulk-nas-1", Date: "2026-09-16", Capacity: 120, UsedCapacity: 30, AccountID: 9902, Provider: "aliyun"},
		{FsID: "test-bulk-nas-zero", Date: "2026-09-16", Capacity: 0, UsedCapacity: 0, AccountID: 9902, Provider: "aws"},
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
	var gotZero types.NASMetric
	if err := coll.FindOne(ctx, bson.M{"fs_id": "test-bulk-nas-zero"}).Decode(&gotZero); err != nil {
		t.Fatal(err)
	}
	if gotZero.QcStatus != types.NASMetricQcZeroException {
		t.Fatalf("bulk zero row qc_status = %q, want zero_exception", gotZero.QcStatus)
	}

	// 同键二次批量重放(值覆盖,行数不增)
	replay := []types.NASMetric{
		{FsID: "test-bulk-nas-1", Date: "2026-09-14", Capacity: 101, UsedCapacity: 11, AccountID: 9902, Provider: "aliyun"},
		{FsID: "test-bulk-nas-1", Date: "2026-09-15", Capacity: 111, UsedCapacity: 21, AccountID: 9902, Provider: "aliyun"},
		{FsID: "test-bulk-nas-1", Date: "2026-09-16", Capacity: 121, UsedCapacity: 31, AccountID: 9902, Provider: "aliyun"},
		{FsID: "test-bulk-nas-zero", Date: "2026-09-16", Capacity: 0, UsedCapacity: 0, AccountID: 9902, Provider: "aws"},
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
	var got types.NASMetric
	if err := coll.FindOne(ctx, bson.M{"fs_id": "test-bulk-nas-1", "date": "2026-09-16"}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Capacity != 121 {
		t.Fatalf("replayed capacity = %v, want 121", got.Capacity)
	}
}
