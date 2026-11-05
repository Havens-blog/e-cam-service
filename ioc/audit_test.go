package ioc

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== renderAuditEntryToEvent 映射 表驱动测试 ====================

func TestRenderAuditEntryToEvent(t *testing.T) {
	entry := middleware.AuditEntry{
		OperationType: "api_asset_create",
		OperatorID:    "42",
		OperatorName:  "系统管理员",
		TenantID:      3,
		HTTPMethod:    "POST",
		APIPath:       "/api/v1/cam/assets",
		RequestBody:   `{"name":"vm-1"}`,
		StatusCode:    201,
		Result:        "success",
		RequestID:     "req-1",
		DurationMs:    88,
		ClientIP:      "10.0.0.8",
		UserAgent:     "UA/1.0",
		Ctime:         1700000000000,
	}

	e := renderAuditEntryToEvent(entry)

	// 骨架字段逐项断言（统一 envelope 的映射契约）
	assert.Equal(t, auditlog.ActorTypeHuman, e.ActorType)
	assert.Equal(t, "42", e.ActorID)
	assert.Equal(t, "系统管理员", e.ActorName)
	assert.Equal(t, int64(1700000000000), e.At)
	assert.Equal(t, int64(3), e.TenantID)
	assert.Equal(t, "10.0.0.8", e.ClientIP)
	assert.Equal(t, "UA/1.0", e.UserAgent)
	assert.Equal(t, auditlog.ServiceCAM, e.Service)
	assert.Equal(t, auditlog.DomainAudit, e.Domain)
	assert.Equal(t, "create", e.Action)
	assert.Equal(t, "api", e.ResourceType)
	assert.Equal(t, "/api/v1/cam/assets", e.ResourceID)
	assert.Equal(t, auditlog.ResultSuccess, e.Result)
	assert.Equal(t, 201, e.StatusCode)
	assert.Equal(t, int64(88), e.DurationMs)
	// 旧字段全部收进 Detail（统一 envelope 不为 API 级审计单开列）
	assert.Equal(t, "POST", e.Detail["http_method"])
	assert.Equal(t, "/api/v1/cam/assets", e.Detail["api_path"])
	assert.Equal(t, `{"name":"vm-1"}`, e.Detail["request_body"])
	assert.Equal(t, "req-1", e.Detail["request_id"])
	assert.Equal(t, "api_asset_create", e.Detail["operation_type"])
	// 映射后的事件必须通过 SDK 骨架校验（契约自洽）
	assert.NoError(t, e.Validate())
}

// ==================== actionFromHTTPMethod 表驱动测试 ====================

func TestActionFromHTTPMethod(t *testing.T) {
	testCases := []struct {
		method string
		want   string
	}{
		{method: "POST", want: "create"},
		{method: "PUT", want: "update"},
		{method: "PATCH", want: "update"},
		{method: "DELETE", want: "delete"},
		{method: "GET", want: "generic"},
	}
	for _, tc := range testCases {
		assert.Equal(t, tc.want, actionFromHTTPMethod(tc.method), tc.method)
	}
}

// ==================== unifiedAuditSink 双写行为测试 ====================

type fakeEventWriter struct {
	events []*auditlog.AuditEvent
	err    error
}

func (f *fakeEventWriter) Emit(_ context.Context, events ...*auditlog.AuditEvent) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, events...)
	return nil
}

type fakeLegacySink struct {
	entries []middleware.AuditEntry
	err     error
}

func (f *fakeLegacySink) Write(_ context.Context, e middleware.AuditEntry) error {
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, e)
	return nil
}

func TestUnifiedAuditSink_Write(t *testing.T) {
	entry := middleware.AuditEntry{
		OperationType: "api_task_delete",
		OperatorID:    "7",
		OperatorName:  "ops",
		TenantID:      3,
		HTTPMethod:    "DELETE",
		APIPath:       "/api/v1/cam/tasks/9",
		Ctime:         1700000000000,
		Result:        "success",
	}

	t.Run("双写成功：旧存储与统一事件各落一次", func(t *testing.T) {
		legacy := &fakeLegacySink{}
		writer := &fakeEventWriter{}
		sink := unifiedAuditSink{legacy: legacy, writer: writer, renderAs: renderAuditEntryToEvent}

		require.NoError(t, sink.Write(context.Background(), entry))
		assert.Len(t, legacy.entries, 1, "旧 AuditLog 应写入")
		require.Len(t, writer.events, 1, "统一事件应写入")
		assert.Equal(t, "delete", writer.events[0].Action)
	})

	t.Run("旧存储失败：直接返回，不再写统一事件", func(t *testing.T) {
		legacy := &fakeLegacySink{err: errors.New("legacy down")}
		writer := &fakeEventWriter{}
		sink := unifiedAuditSink{legacy: legacy, writer: writer, renderAs: renderAuditEntryToEvent}

		err := sink.Write(context.Background(), entry)
		require.Error(t, err)
		assert.Len(t, writer.events, 0, "旧存储失败时不应写统一事件")
	})

	t.Run("统一事件失败：错误上抛由中间件记日志（审计旁路不阻塞）", func(t *testing.T) {
		legacy := &fakeLegacySink{}
		writer := &fakeEventWriter{err: errors.New("unified down")}
		sink := unifiedAuditSink{legacy: legacy, writer: writer, renderAs: renderAuditEntryToEvent}

		err := sink.Write(context.Background(), entry)
		require.Error(t, err)
		assert.Len(t, legacy.entries, 1, "旧存储不受统一事件失败影响")
	})
}
