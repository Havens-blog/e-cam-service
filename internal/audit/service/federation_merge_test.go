package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
)

// fakeFederationClient 联邦客户端桩（可注入返回值与错误）
type fakeFederationClient struct {
	events []auditlog.AuditEvent
	total  int64
	err    error
	calls  int
}

func (f *fakeFederationClient) FetchIAMEvents(ctx context.Context, tenantID int64,
	filter *dao.AuditEventFilter, offset, limit int,
) ([]auditlog.AuditEvent, int64, error) {
	f.calls++
	return f.events, f.total, f.err
}

// fakeEventDAO Mongo 侧桩（内存行 + Count）
type fakeEventDAOFed struct {
	rows []auditlog.AuditEvent
}

func (d *fakeEventDAOFed) List(ctx context.Context, filter *dao.AuditEventFilter) ([]auditlog.AuditEvent, error) {
	out := []auditlog.AuditEvent{}
	for i := range d.rows {
		if filter.TenantID != 0 && d.rows[i].TenantID != filter.TenantID {
			continue
		}
		out = append(out, d.rows[i])
	}
	// 简化分页（测试用例只断言归并结果，不做深分页）
	if int(filter.Offset) >= len(out) {
		return []auditlog.AuditEvent{}, nil
	}
	end := int(filter.Offset) + int(filter.Limit)
	if end > len(out) {
		end = len(out)
	}
	return out[int(filter.Offset):end], nil
}

func (d *fakeEventDAOFed) InitIndexes(ctx context.Context) error {
	return nil
}

func (d *fakeEventDAOFed) Count(ctx context.Context, filter *dao.AuditEventFilter) (int64, error) {
	var n int64
	for i := range d.rows {
		if filter.TenantID != 0 && d.rows[i].TenantID != filter.TenantID {
			continue
		}
		n++
	}
	return n, nil
}

func TestListEventsFederated(t *testing.T) {
	localRows := []auditlog.AuditEvent{
		{ActorID: "7", At: 1700000001000, TenantID: 3, Service: "e-cam-service", Domain: "audit", Action: "create", Result: "success"},
		{ActorID: "9", At: 1700000003000, TenantID: 3, Service: "e-cam-service", Domain: "audit", Action: "delete", Result: "success"},
	}
	iamRows := []auditlog.AuditEvent{
		{ActorID: "8", At: 1700000002000, TenantID: 3, Service: "eiam", Domain: "operation", Action: "update", Result: "success"},
	}
	filterOf := func() *dao.AuditEventFilter {
		return &dao.AuditEventFilter{TenantID: 3, Offset: 0, Limit: 20}
	}

	t.Run("双源归并：at 降序交错 + 联合 total", func(t *testing.T) {
		fed := &fakeFederationClient{events: iamRows, total: int64(len(iamRows))}
		svc := &AuditEventService{dao: &fakeEventDAOFed{rows: localRows}, logger: elog.DefaultLogger, fedClient: fed}
		events, total, degraded, err := svc.ListEventsFederated(context.Background(), filterOf())
		require.NoError(t, err)
		assert.False(t, degraded)
		assert.Equal(t, int64(3), total, "total = 本侧 + IAM 侧")
		require.Len(t, events, 3)
		// 归并排序：at 降序 3000/2000/1000
		assert.Equal(t, int64(1700000003000), events[0].At)
		assert.Equal(t, int64(1700000002000), events[1].At)
		assert.Equal(t, int64(1700000001000), events[2].At)
		assert.Equal(t, 1, fed.calls, "联邦客户端应被调用一次")
	})

	t.Run("联邦降级：e-iam 不可达 → 本侧单源 + degraded 标记", func(t *testing.T) {
		fed := &fakeFederationClient{err: errors.New("e-iam down")}
		svc := &AuditEventService{dao: &fakeEventDAOFed{rows: localRows}, logger: elog.DefaultLogger, fedClient: fed}
		events, total, degraded, err := svc.ListEventsFederated(context.Background(), filterOf())
		require.NoError(t, err)
		assert.True(t, degraded)
		assert.Equal(t, int64(2), total)
		assert.Len(t, events, 2)
	})

	t.Run("未配置联邦客户端 → 单源且 degraded=false", func(t *testing.T) {
		svc := &AuditEventService{dao: &fakeEventDAOFed{rows: localRows}, logger: elog.DefaultLogger}
		events, total, degraded, err := svc.ListEventsFederated(context.Background(), filterOf())
		require.NoError(t, err)
		assert.False(t, degraded)
		assert.Equal(t, int64(2), total)
		assert.Len(t, events, 2)
	})

	t.Run("无租户上下文时跳过联邦（fail-closed 防御）", func(t *testing.T) {
		fed := &fakeFederationClient{}
		svc := &AuditEventService{dao: &fakeEventDAOFed{rows: localRows}, logger: elog.DefaultLogger, fedClient: fed}
		f := filterOf()
		f.TenantID = 0
		_, _, degraded, err := svc.ListEventsFederated(context.Background(), f)
		require.NoError(t, err)
		assert.False(t, degraded)
		assert.Equal(t, 0, fed.calls, "无租户不应发起联邦调用")
	})
}
