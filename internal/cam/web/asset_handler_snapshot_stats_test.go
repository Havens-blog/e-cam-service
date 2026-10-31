package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// fakeInstanceSvc 只实现 List（其余方法由内嵌 nil 接口兜底，不会被调用）
type fakeInstanceSvc struct {
	service.InstanceService
	calls []domain.InstanceFilter
	// total 由测试按 filter 决定，模拟真实计数
	totalFor func(*domain.InstanceFilter) int64
}

func (f *fakeInstanceSvc) List(_ context.Context, filter domain.InstanceFilter) ([]domain.Instance, int64, error) {
	f.calls = append(f.calls, filter)
	if f.totalFor == nil {
		return nil, 0, nil
	}
	return nil, f.totalFor(&filter), nil
}

// TestGetSnapshotStats_口径与真实数据一致 校验统计口径：
//   - Total 为全量计数
//   - Normal 为 4 个成功状态别名之和
//   - User/Auto/Timer 为 snapshot_type 精确等值
//
// 期望值取自 2026-10-31 线上 7208 条样本的实测分布。
func TestGetSnapshotStats_CountsMatchRealDistribution(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 注意：真实数据里 110 条是 **NORMAL（全大写）**，不是 "normal"。
	// 若实现只枚举小写，Normal 会算成 7098（漏 110），且看起来完全合理 —— 故此处按真实大小写建模。
	dist := map[string]int64{
		"status:accomplished": 2408,
		"status:available":    3080,
		"status:completed":    1610,
		"status:NORMAL":       110,
		"snapshot_type:user":  2752,
		"snapshot_type:auto":  2714,
		"snapshot_type:timer": 1686,
	}

	svc := &fakeInstanceSvc{}
	svc.totalFor = func(f *domain.InstanceFilter) int64 {
		if len(f.Attributes) == 0 {
			return 7208 // 全量
		}
		for k, v := range f.Attributes {
			if total, ok := dist[k+":"+toStr(v)]; ok {
				return total
			}
		}
		return 0
	}

	h := &AssetHandler{instanceSvc: svc}
	r := gin.New()
	r.GET("/assets/snapshot/stats", func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(3))
		h.GetSnapshotStats(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/snapshot/stats", http.NoBody)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data SnapshotStatsResp `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, w.Body.String())
	}

	want := SnapshotStatsResp{Total: 7208, Normal: 7208, User: 2752, Auto: 2714, Timer: 1686}
	if resp.Data != want {
		t.Fatalf("stats = %+v, want %+v", resp.Data, want)
	}

	// 口径守护：Normal 必须等于 4 个别名之和，而不是单次查询
	if len(svc.calls) < 1+len(snapshotSuccessStatuses)+3 {
		t.Fatalf("expected at least %d count queries, got %d", 1+len(snapshotSuccessStatuses)+3, len(svc.calls))
	}
}

// TestGetSnapshotStats_RequiresTenant 缺少租户时返回 400（与其它资产接口一致）
func TestGetSnapshotStats_RequiresTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AssetHandler{instanceSvc: &fakeInstanceSvc{}}
	r := gin.New()
	r.GET("/assets/snapshot/stats", h.GetSnapshotStats)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/snapshot/stats", http.NoBody))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func toStr(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
