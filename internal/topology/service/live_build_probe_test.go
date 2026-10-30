// Package service 直接跑 LiveTopologyBuilder.BuildFromDNS 验证 WAF→ALB 边是否重建出来(manual probe,只读)。
package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestProbeLiveBuildWAFtoALB(t *testing.T) {
	dsn := loadProbeDSN(t)
	if dsn == "" {
		// 显式给 NAS_PROBE_MONGODB_DSN 才运行；不再回退读 config/prod.yaml（避免测试连生产库）
		t.Skip("未设置 NAS_PROBE_MONGODB_DSN，跳过 live probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(dsn))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(ctx)

	// 取租户 ID（从已知拓扑节点）
	var n struct {
		TenantID int64 `bson:"tenant_id"`
	}
	if err := client.Database("ecam").Collection("ecam_topo_node").FindOne(ctx, bson.M{"_id": "inst-5949117"}).Decode(&n); err != nil {
		t.Fatalf("取 tenant_id 失败: %v", err)
	}
	t.Logf("tenant_id=%d", n.TenantID)

	// 直接构建
	builder := NewLiveTopologyBuilder(mongox.NewMongo(client, "ecam"))
	graph, err := builder.BuildFromDNS(ctx, n.TenantID, "api.jlc.com")
	if err != nil {
		t.Fatalf("BuildFromDNS: %v", err)
	}
	t.Logf("nodes=%d edges=%d", len(graph.Nodes), len(graph.Edges))

	// 检查 WAF→ALB 边
	found := false
	for _, e := range graph.Edges {
		if e.SourceID == "inst-5949117" && e.TargetID == "inst-51187306" {
			found = true
			t.Logf("✓ 找到 WAF→ALB 边: %s", e.ID)
		}
	}
	if !found {
		t.Log("✗ 未找到 WAF→ALB 边")
		// dump WAF 节点的出边
		t.Log("WAF inst-5949117 的下游边:")
		for _, e := range graph.Edges {
			if e.SourceID == "inst-5949117" {
				t.Logf("  -> %s [%s]", e.TargetID, e.Relation)
			}
		}
	}
}

// loadProbeDSN 只认显式环境变量 NAS_PROBE_MONGODB_DSN。
//
// 【为什么不再回退读配置文件】原实现在未设置该 env 时会去解析 `config/prod.yaml`，
// 并连接其中的**生产** Mongo（106.52.187.69:27017）。这带来两个后果：
//  1. 测试会触碰**生产数据**（本 probe 只读，但"默认连生产"本身不可接受）；
//  2. CI 未设置该 env 时连生产库，30s 后超时失败（`Test` 任务红灯来源之一）。
//
// 需要跑该 probe 时显式提供 DSN，例如：
//
//	NAS_PROBE_MONGODB_DSN='mongodb://user:pass@host:27017/ecam?authSource=admin' \
//	  go test ./internal/topology/service/ -run TestProbeLiveBuildWAFtoALB -v
func loadProbeDSN(t *testing.T) string {
	return os.Getenv("NAS_PROBE_MONGODB_DSN")
}
