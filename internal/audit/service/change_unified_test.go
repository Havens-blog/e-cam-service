package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
)

// ==================== 映射口径 表驱动测试 ====================

func TestToChangeEventFromRecord(t *testing.T) {
	base := domain.ChangeRecord{
		AssetID: "i-123", AssetName: "web-1", ModelUID: "ecs",
		TenantID: 3, AccountID: 1, Provider: "qcloud", Region: "ap-guangzhou",
		ChangeSource: "sync_task", ChangeTaskID: "task-9", Ctime: 1700000000000,
	}
	testCases := []struct {
		name       string
		mutate     func(r *domain.ChangeRecord)
		wantAction string
		wantActor  auditlog.ActorType
		wantActorI string
	}{
		{
			name:       "字段修改 → update + system(taskID)",
			mutate:     func(r *domain.ChangeRecord) { r.FieldName = "spec"; r.OldValue = `"4c"`; r.NewValue = `"8c"` },
			wantAction: "update", wantActor: auditlog.ActorTypeSystem, wantActorI: "task-9",
		},
		{
			name:       "新增字段 → create",
			mutate:     func(r *domain.ChangeRecord) { r.FieldName = "tag"; r.OldValue = ""; r.NewValue = `"x"` },
			wantAction: "create", wantActor: auditlog.ActorTypeSystem, wantActorI: "task-9",
		},
		{
			name:       "删除字段 → delete",
			mutate:     func(r *domain.ChangeRecord) { r.FieldName = "tag"; r.OldValue = `"x"`; r.NewValue = "" },
			wantAction: "delete", wantActor: auditlog.ActorTypeSystem, wantActorI: "task-9",
		},
		{
			name: "手工变更 → human + unknown（旧存储未记录操作者，如实标注）",
			mutate: func(r *domain.ChangeRecord) {
				r.ChangeSource = "manual"
				r.ChangeTaskID = ""
				r.FieldName = "spec"
				r.OldValue = "a"
				r.NewValue = "b"
			},
			wantAction: "update", wantActor: auditlog.ActorTypeHuman, wantActorI: "unknown",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			ev := toChangeEventFromRecord(&r)
			assert.Equal(t, auditlog.ServiceCAM, ev.Service)
			assert.Equal(t, auditlog.DomainChange, ev.Domain)
			assert.Equal(t, tc.wantAction, ev.Action)
			assert.Equal(t, tc.wantActor, ev.ActorType)
			assert.Equal(t, tc.wantActorI, ev.ActorID)
			assert.Equal(t, "ecs", ev.ResourceType)
			assert.Equal(t, "i-123", ev.ResourceID)
			assert.Equal(t, "urn:cam:asset:ecs:i-123", ev.ResourceURN)
			assert.Equal(t, auditlog.ResultSuccess, ev.Result)
			assert.Equal(t, int64(1700000000000), ev.At)
			assert.Equal(t, int64(3), ev.TenantID)
			// Detail 收齐字段级差异载荷
			assert.Equal(t, r.FieldName, ev.Detail["field_name"])
			assert.Equal(t, r.NewValue, ev.Detail["new_value"])
			// 映射后必须通过 SDK 骨架校验
			assert.NoError(t, ev.Validate())
		})
	}
}

func TestToChangeEventFromOrderEntry(t *testing.T) {
	success := true
	failed := false
	testCases := []struct {
		name       string
		entry      domain.ChangeOrderAuditEntry
		wantActor  auditlog.ActorType
		wantResult auditlog.Result
	}{
		{
			name:      "人工确认 → human + success",
			entry:     domain.ChangeOrderAuditEntry{OrderID: "CO-1", Actor: "admin", Action: "confirm", At: 1700000000000},
			wantActor: auditlog.ActorTypeHuman, wantResult: auditlog.ResultSuccess,
		},
		{
			name: "executor 动作 → executor 类型",
			entry: domain.ChangeOrderAuditEntry{
				OrderID: "CO-1", Actor: "executor-1", Action: "execute",
				At: 1700000000000, Success: &success,
			},
			wantActor: auditlog.ActorTypeExecutor, wantResult: auditlog.ResultSuccess,
		},
		{
			name: "scheduler 清理失败 → scheduler + failed",
			entry: domain.ChangeOrderAuditEntry{
				OrderID: "CO-1", Actor: "scheduler-orphan", Action: "orphan_cleanup",
				At: 1700000000000, Success: &failed,
			},
			wantActor: auditlog.ActorTypeScheduler, wantResult: auditlog.ResultFailed,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ev := toChangeEventFromOrderEntry(&tc.entry)
			assert.Equal(t, tc.wantActor, ev.ActorType)
			assert.Equal(t, tc.wantResult, ev.Result)
			assert.Equal(t, auditlog.DomainChange, ev.Domain)
			assert.Equal(t, "change_order", ev.ResourceType)
			assert.Equal(t, "CO-1", ev.ResourceID)
			assert.Equal(t, tc.entry.Action, ev.Action)
			assert.NoError(t, ev.Validate())
		})
	}
	t.Run("DedupKey 透传", func(t *testing.T) {
		ev := toChangeEventFromOrderEntry(&domain.ChangeOrderAuditEntry{
			OrderID: "CO-2", Actor: "admin", Action: "orphan_cleanup",
			At: 1700000000000, DedupKey: "dedup-1",
		})
		assert.Equal(t, "dedup-1", ev.DedupKey)
	})
}

// ==================== 双写旁路行为测试 ====================

type fakeEventWriter struct {
	events []*auditlog.AuditEvent
	err    error
}

func (f *fakeEventWriter) Emit(ctx context.Context, events ...*auditlog.AuditEvent) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, events...)
	return nil
}

func TestTrackChanges_UnifiedEventDualWrite(t *testing.T) {
	newTrackerWith := func(w auditlog.Writer) *ChangeTracker {
		return &ChangeTracker{dao: &fakeChangeDAO{}, logger: elog.DefaultLogger, events: w, ignoreFields: defaultIgnoreFields}
	}
	t.Run("变更落库后旁路 Emit 统一事件", func(t *testing.T) {
		w := &fakeEventWriter{}
		tr := newTrackerWith(w)
		n, err := tr.TrackChanges(context.Background(),
			domain.ChangeMetadata{AssetID: "i-1", AssetName: "web", ModelUID: "ecs", TenantID: 3, ChangeSource: "sync_task", ChangeTaskID: "t-1"},
			map[string]interface{}{"spec": "4c"}, map[string]interface{}{"spec": "8c"})
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.Len(t, w.events, 1)
		assert.Equal(t, auditlog.DomainChange, w.events[0].Domain)
		assert.Equal(t, "update", w.events[0].Action)
	})
	t.Run("统一事件失败不影响旧存储主链路", func(t *testing.T) {
		w := &fakeEventWriter{err: errors.New("mongo down")}
		tr := newTrackerWith(w)
		n, err := tr.TrackChanges(context.Background(),
			domain.ChangeMetadata{AssetID: "i-1", ModelUID: "ecs", TenantID: 3, ChangeSource: "sync_task", ChangeTaskID: "t-1"},
			map[string]interface{}{"spec": "4c"}, map[string]interface{}{"spec": "8c"})
		// 旧存储写入成功（fakeChangeDAO 无错误），统一事件失败被旁路吞掉
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})
	t.Run("writer 为 nil 时跳过双写", func(t *testing.T) {
		tr := newTrackerWith(nil)
		n, err := tr.TrackChanges(context.Background(),
			domain.ChangeMetadata{AssetID: "i-1", ModelUID: "ecs", TenantID: 3, ChangeSource: "sync_task", ChangeTaskID: "t-1"},
			map[string]interface{}{"spec": "4c"}, map[string]interface{}{"spec": "8c"})
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})
}
