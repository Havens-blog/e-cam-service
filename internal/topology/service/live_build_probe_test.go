// Package service 直接跑 LiveTopologyBuilder.BuildFromDNS 验证 WAF→ALB 边是否重建出来(manual probe,只读)。
package service

import (
	"context"
	"os"
	"strings"
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
		t.Skip("无 mongo DSN")
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

func loadProbeDSN(t *testing.T) string {
	if v := os.Getenv("NAS_PROBE_MONGODB_DSN"); v != "" {
		return v
	}
	raw, err := os.ReadFile("../../../config/prod.yaml")
	if err != nil {
		t.Logf("未找到 config/prod.yaml: %v", err)
		return ""
	}
	var dsn, user, pass string
	inMongo := false
	for _, line := range strings.Split(string(raw), "\n") {
		tr := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(tr, "mongodb:"):
			inMongo = true
		case inMongo && strings.HasPrefix(tr, "dsn:"):
			dsn = strings.TrimSpace(strings.TrimPrefix(tr, "dsn:"))
		case inMongo && strings.HasPrefix(tr, "username:"):
			user = strings.TrimSpace(strings.TrimPrefix(tr, "username:"))
		case inMongo && strings.HasPrefix(tr, "password:"):
			pass = strings.TrimSpace(strings.TrimPrefix(tr, "password:"))
		case inMongo && !strings.HasPrefix(tr, " ") && tr != "" && !strings.HasPrefix(tr, "dsn:") && !strings.HasPrefix(tr, "username:") && !strings.HasPrefix(tr, "password:") && !strings.HasPrefix(tr, "db:"):
			inMongo = false
		}
	}
	if dsn != "" && user != "" {
		dsn = strings.Replace(dsn, "mongodb://", "mongodb://"+user+":"+pass+"@", 1)
	}
	return dsn
}