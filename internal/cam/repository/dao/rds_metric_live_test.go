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

// RDS 指标 DAO 活体验证(MONGO_DSN 门控,与 Disk 指标 DAO 同型)。
// 用独立测试库 ecam_dao_rds_test + Cleanup 整库 Drop:测试期间的任何写入
// (含唯一索引创建)都不接触真实业务集合。
const rdsMetricTestDB = "ecam_dao_rds_test"

func liveRDSMetricDAO(t *testing.T) (RDSMetricDAO, *mongo.Collection) {
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
	db := client.Database(rdsMetricTestDB)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_ = db.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})
	return NewRDSMetricDAO(mongox.NewMongo(client, rdsMetricTestDB)), db.Collection(RDSMetricCollection)
}

// 唯一索引 (account_id, rds_id, date)(T2 AC-3):键序、Unique 标记须精确一致;
// 唯一键必须含 account_id(跨账号共享实例并存各留一行,Hard Rule,3cea6d7
// 多账号修复经验;rds_id 是地域内唯一标识,类比 Disk disk_id)。
func TestRDSMetricUniqueIndexLive(t *testing.T) {
	_, coll := liveRDSMetricDAO(t)
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
		if len(keys) != 3 || keys["account_id"] != int32(1) || keys["rds_id"] != int32(1) || keys["date"] != int32(1) {
			continue
		}
		if spec.Unique == nil || !*spec.Unique {
			t.Fatalf("索引 %v 存在但未标记 Unique: %+v", keys, spec)
		}
		return
	}
	t.Fatalf("未找到 (account_id, rds_id, date) 唯一索引, specs=%+v", specs)
}

// upsert 幂等 + 四指标全 0 异常行落库可见(T2 AC-4/AC-5):
// 同账号同日重采只保留一行(值覆盖为最新),全 0 行例外放行打 zero_exception 标。
func TestRDSMetricLive(t *testing.T) {
	d, coll := liveRDSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	base := types.RDSMetric{
		RdsID: "test-live-rds-1", InstanceName: "live测试库", Date: "2026-09-14",
		CPUPercent: 15.7, MemoryPercent: 82.06, DiskPercent: 33.5, Connections: 128,
		Engine: "mysql", AccountID: 9901, Provider: "aliyun",
	}
	// 1. 首写
	if err := d.UpsertMetric(ctx, base); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	// 2. 同 (account_id, rds_id, date) 重写覆盖为最新值(DAO 层保证键唯一)
	updated := base
	updated.CPUPercent = 88.6
	updated.Connections = 200
	if err := d.UpsertMetric(ctx, updated); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	// 3. 仍只有一行,且值为最新
	cnt, err := coll.CountDocuments(ctx, bson.M{"rds_id": base.RdsID})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("metric count = %d, want 1 (同账号同日幂等失败)", cnt)
	}
	var got types.RDSMetric
	if err := coll.FindOne(ctx, bson.M{"rds_id": base.RdsID}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CPUPercent != 88.6 || got.Connections != 200 || got.Date != "2026-09-14" || got.Engine != "mysql" || got.Provider != "aliyun" {
		t.Fatalf("unexpected metric: %+v", got)
	}

	// 4. 四指标全 0 例外放行:正常落库且 qc_status=zero_exception 落库可见
	zero := types.RDSMetric{
		RdsID: "test-live-rds-zero", InstanceName: "live全零库", Date: "2026-09-14",
		AccountID: 9901, Provider: "huawei",
	}
	if err := d.UpsertMetric(ctx, zero); err != nil {
		t.Fatalf("upsert zero row: %v (四指标全 0 须例外放行,不拦截不跳过)", err)
	}
	var gotZero types.RDSMetric
	if err := coll.FindOne(ctx, bson.M{"rds_id": zero.RdsID}).Decode(&gotZero); err != nil {
		t.Fatalf("decode zero row: %v", err)
	}
	if gotZero.QcStatus != types.RDSMetricQcZeroException {
		t.Fatalf("zero row qc_status = %q, want zero_exception (异常行须落库可见)", gotZero.QcStatus)
	}

	// 5. 唯一索引真实拦截重复键(绕过 upsert 的裸 InsertOne 必须 dup-key 报错)
	dup := bson.M{"account_id": base.AccountID, "rds_id": base.RdsID, "date": base.Date, "cpu_percent": 1.0}
	if _, err := coll.InsertOne(ctx, dup); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("want duplicate key error for %v, got %v (唯一索引未生效)", dup, err)
	}
}

// 回归:同一 RDS 实例可被多个云账号纳管(跨账号共享实例),(account_id, rds_id,
// date) 才是唯一键——不同账号写同实例同日,必须各保留一行,不得互相覆盖
// (Hard Rule,3cea6d7 多账号修复经验)。仿 Disk TestDiskMetricPerAccountLive。
func TestRDSMetricPerAccountLive(t *testing.T) {
	d, coll := liveRDSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 同一实例、同一天,三个不同账号(阿里/华为/AWS)各写一笔
	rdsID, date := "test-shared-rds-001", "2026-09-14"
	seeds := []types.RDSMetric{
		{RdsID: rdsID, InstanceName: "共享库", Date: date, CPUPercent: 60.7, MemoryPercent: 70.1, DiskPercent: 33.5, Connections: 100, Engine: "mysql", AccountID: 1, Provider: "aliyun"},
		{RdsID: rdsID, InstanceName: "共享库", Date: date, CPUPercent: 41.8, MemoryPercent: 60.2, DiskPercent: 22.2, Connections: 50, Engine: "postgresql", AccountID: 2, Provider: "huawei"},
		{RdsID: rdsID, InstanceName: "共享库", Date: date, CPUPercent: 19.5, MemoryPercent: 90.5, DiskPercent: 11.1, Connections: 25, Engine: "mysql", AccountID: 3, Provider: "aws"},
	}
	for _, m := range seeds {
		if err := d.UpsertMetric(ctx, m); err != nil {
			t.Fatalf("upsert acct %d: %v", m.AccountID, err)
		}
	}

	// 三行都必须保留,各账号值正确
	cnt, err := coll.CountDocuments(ctx, bson.M{"rds_id": rdsID, "date": date})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 3 {
		t.Fatalf("shared rds rows = %d, want 3 (跨账号互相覆盖 bug)", cnt)
	}
	for _, seed := range seeds {
		var got types.RDSMetric
		filter := bson.M{"rds_id": rdsID, "date": date, "account_id": seed.AccountID}
		if err := coll.FindOne(ctx, filter).Decode(&got); err != nil {
			t.Fatalf("decode acct%d: %v", seed.AccountID, err)
		}
		if got.CPUPercent != seed.CPUPercent || got.Provider != seed.Provider {
			t.Fatalf("acct%d metric = %+v, want cpu %v (跨账号覆盖丢失)", seed.AccountID, got, seed.CPUPercent)
		}
	}

	// 账号内同日重写仍幂等(同一账号同实例同日只留一行,值覆盖)
	update := seeds[1]
	update.CPUPercent = 50.5
	if err := d.UpsertMetric(ctx, update); err != nil {
		t.Fatalf("re-upsert acct2: %v", err)
	}
	cnt, err = coll.CountDocuments(ctx, bson.M{"rds_id": rdsID, "date": date})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 3 {
		t.Fatalf("after re-upsert rows = %d, want 3", cnt)
	}
	var got types.RDSMetric
	if err := coll.FindOne(ctx, bson.M{"rds_id": rdsID, "date": date, "account_id": 2}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.CPUPercent != 50.5 {
		t.Fatalf("acct2 re-upsert cpu = %v, want 50.5", got.CPUPercent)
	}
}

// 首写生效 vs 覆盖更新活体验证(T2 AC-3:今日行首写生效不覆盖/昨日行补采覆盖更新):
//   - BulkInsertIfAbsent 同键二次写入:首次值保留(已有行不被覆盖,不产生脏行);
//   - BulkUpsertMetrics 同键二次写入:值覆盖为最新(次日补采昨日行的语义)。
func TestRDSMetricInsertIfAbsentLive(t *testing.T) {
	d, coll := liveRDSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	base := types.RDSMetric{
		RdsID: "test-insert-absent-rds-1", InstanceName: "首写生效库", Date: "2026-09-14",
		CPUPercent: 41.8, MemoryPercent: 60.2, DiskPercent: 22.2, Connections: 50,
		Engine: "postgresql", AccountID: 9903, Provider: "huawei",
	}
	// 1. 首写:插入成功
	if err := d.BulkInsertIfAbsent(ctx, []types.RDSMetric{base}); err != nil {
		t.Fatalf("insert-if-absent first: %v", err)
	}
	// 2. 同键二次首写:不得覆盖已有行(首写生效)
	retry := base
	retry.CPUPercent = 99
	retry.Connections = 999
	if err := d.BulkInsertIfAbsent(ctx, []types.RDSMetric{retry}); err != nil {
		t.Fatalf("insert-if-absent second: %v", err)
	}
	var got types.RDSMetric
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "rds_id": base.RdsID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CPUPercent != 41.8 || got.Connections != 50 {
		t.Fatalf("首写生效失败,二次写入覆盖了已有行: cpu=%v connections=%v (want 41.8/50)", got.CPUPercent, got.Connections)
	}
	// 行数仍为 1(同日重复采集不产生脏行)
	cnt, err := coll.CountDocuments(ctx, bson.M{"account_id": base.AccountID, "rds_id": base.RdsID})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("rows = %d, want 1 (首写批重放产生脏行)", cnt)
	}

	// 3. 覆盖更新路径(BulkUpsertMetrics)同键重写:值覆盖为最新(昨日补采语义)
	override := base
	override.CPUPercent = 55
	override.DiskPercent = 30
	if err := d.BulkUpsertMetrics(ctx, []types.RDSMetric{override}); err != nil {
		t.Fatalf("bulk upsert override: %v", err)
	}
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "rds_id": base.RdsID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode override: %v", err)
	}
	if got.CPUPercent != 55 || got.DiskPercent != 30 {
		t.Fatalf("覆盖更新失败: cpu=%v disk=%v (want 55/30)", got.CPUPercent, got.DiskPercent)
	}

	// 4. 首写批与覆盖批互不串扰:覆盖后再次首写仍不回退已有值
	if err := d.BulkInsertIfAbsent(ctx, []types.RDSMetric{retry}); err != nil {
		t.Fatalf("insert-if-absent after override: %v", err)
	}
	if err := coll.FindOne(ctx, bson.M{"account_id": base.AccountID, "rds_id": base.RdsID, "date": base.Date}).Decode(&got); err != nil {
		t.Fatalf("decode final: %v", err)
	}
	if got.CPUPercent != 55 {
		t.Fatalf("覆盖后首写批误改已有行: cpu=%v (want 55)", got.CPUPercent)
	}
}

// 批量写入活体验证:单批多条一次落库、同键二次批量重放幂等不重复(值覆盖)、
// 全 0 行整批内照常落库并打标;读取方法 ListByRDS/ListByAccounts 窗口与排序验证。
func TestRDSMetricBulkAndReadLive(t *testing.T) {
	d, coll := liveRDSMetricDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	batch := []types.RDSMetric{
		{RdsID: "test-bulk-rds-1", Date: "2026-09-12", CPUPercent: 10, MemoryPercent: 40, DiskPercent: 30, Connections: 10, Engine: "mysql", AccountID: 9902, Provider: "aliyun"},
		{RdsID: "test-bulk-rds-1", Date: "2026-09-13", CPUPercent: 20, MemoryPercent: 50, DiskPercent: 31, Connections: 20, Engine: "mysql", AccountID: 9902, Provider: "aliyun"},
		{RdsID: "test-bulk-rds-1", Date: "2026-09-14", CPUPercent: 30, MemoryPercent: 60, DiskPercent: 32, Connections: 30, Engine: "mysql", AccountID: 9902, Provider: "aliyun"},
		{RdsID: "test-bulk-rds-zero", Date: "2026-09-14", AccountID: 9902, Provider: "aws"},
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
	var gotZero types.RDSMetric
	if err := coll.FindOne(ctx, bson.M{"rds_id": "test-bulk-rds-zero"}).Decode(&gotZero); err != nil {
		t.Fatal(err)
	}
	if gotZero.QcStatus != types.RDSMetricQcZeroException {
		t.Fatalf("bulk zero row qc_status = %q, want zero_exception", gotZero.QcStatus)
	}

	// 同键二次批量重放(值覆盖,行数不增)
	replay := []types.RDSMetric{
		{RdsID: "test-bulk-rds-1", Date: "2026-09-13", CPUPercent: 21, MemoryPercent: 51, DiskPercent: 41, Connections: 21, Engine: "mysql", AccountID: 9902, Provider: "aliyun"},
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
	var got types.RDSMetric
	if err := coll.FindOne(ctx, bson.M{"rds_id": "test-bulk-rds-1", "date": "2026-09-13"}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.CPUPercent != 21 {
		t.Fatalf("replayed cpu = %v, want 21", got.CPUPercent)
	}

	// ListByRDS:近 N 天窗口 + date 升序(窗口相对当日,fixture 日期在近 30 天内)
	rows, err := d.ListByRDS(ctx, 9902, "test-bulk-rds-1", 30)
	if err != nil {
		t.Fatalf("ListByRDS: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("ListByRDS rows = %d, want 3 (近 30 天窗口须覆盖 fixture 日期)", len(rows))
	}
	if rows[0].Date != "2026-09-12" || rows[2].Date != "2026-09-14" {
		t.Fatalf("ListByRDS 未按 date 升序: %s..%s", rows[0].Date, rows[2].Date)
	}

	// ListByAccounts:跨实例取回(含零值行),engine 元数据保留
	acctRows, err := d.ListByAccounts(ctx, []int64{9902}, 30)
	if err != nil {
		t.Fatalf("ListByAccounts: %v", err)
	}
	if len(acctRows) != 4 {
		t.Fatalf("ListByAccounts rows = %d, want 4", len(acctRows))
	}
	foundZero := false
	for _, r := range acctRows {
		if r.RdsID == "test-bulk-rds-zero" && r.QcStatus == types.RDSMetricQcZeroException {
			foundZero = true
		}
	}
	if !foundZero {
		t.Fatal("ListByAccounts 未取回全 0 异常行")
	}

	// CountMetricsByProviders:窗口内行存在即「成功采集」证据(健康监控读取口径)
	counts, err := d.CountMetricsByProviders(ctx, []string{"aliyun", "aws"}, "2026-09-01")
	if err != nil {
		t.Fatalf("CountMetricsByProviders: %v", err)
	}
	if counts["aliyun"] < 3 || counts["aws"] < 1 {
		t.Fatalf("provider counts = %+v, want aliyun>=3 aws>=1", counts)
	}
}
