package service

import (
	"context"
	"errors"
	"testing"

	"github.com/gotomicro/ego/core/elog"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
)

// fakeChangeDAO 可配置各统计方法是否报错，用于验证 GetSummary 的容错行为。
type fakeChangeDAO struct {
	total      int64
	byModel    map[string]int64
	byField    map[string]int64
	byProvider map[string]int64

	errTotal      error
	errByModel    error
	errByField    error
	errByProvider error
}

func (f *fakeChangeDAO) BatchCreate(ctx context.Context, records []domain.ChangeRecord) error {
	return nil
}

func (f *fakeChangeDAO) List(ctx context.Context, filter domain.ChangeFilter) ([]domain.ChangeRecord, error) {
	return nil, nil
}

func (f *fakeChangeDAO) Count(ctx context.Context, filter domain.ChangeFilter) (int64, error) {
	return f.total, f.errTotal
}

func (f *fakeChangeDAO) CountByModelUID(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error) {
	return f.byModel, f.errByModel
}

func (f *fakeChangeDAO) CountByField(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error) {
	return f.byField, f.errByField
}

func (f *fakeChangeDAO) CountByProvider(ctx context.Context, filter domain.ChangeFilter) (map[string]int64, error) {
	return f.byProvider, f.errByProvider
}

func (f *fakeChangeDAO) InitIndexes(ctx context.Context) error {
	return nil
}

func newTracker(f *fakeChangeDAO) *ChangeTracker {
	return NewChangeTracker(f, elog.DefaultLogger)
}

// TestGetSummary_AllSuccess 全部统计成功时，四项都应返回。
func TestGetSummary_AllSuccess(t *testing.T) {
	dao := &fakeChangeDAO{
		total:      100,
		byModel:    map[string]int64{"ecs": 60},
		byField:    map[string]int64{"status": 40},
		byProvider: map[string]int64{"aliyun": 100},
	}

	s, err := newTracker(dao).GetSummary(context.Background(), domain.ChangeFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Total != 100 {
		t.Fatalf("total = %d, want 100", s.Total)
	}
	if s.ByResourceType["ecs"] != 60 || s.ByField["status"] != 40 || s.ByProvider["aliyun"] != 100 {
		t.Fatalf("unexpected maps: %+v", s)
	}
}

// TestGetSummary_PartialFailure 部分统计失败时应返回成功项，而不是整体报错。
// 这是本次改动的核心：540 万条记录上聚合易超时，不能因一项失败让整个统计不可用。
func TestGetSummary_PartialFailure(t *testing.T) {
	dao := &fakeChangeDAO{
		total:         77,
		byModel:       nil,
		byField:       map[string]int64{"status": 12},
		byProvider:    map[string]int64{"tencent": 77},
		errByModel:    errors.New("socket was unexpectedly closed: EOF"),
		errByProvider: errors.New("aggregate timeout"),
	}

	s, err := newTracker(dao).GetSummary(context.Background(), domain.ChangeFilter{})
	if err != nil {
		t.Fatalf("partial failure should not return error, got %v", err)
	}
	if s.Total != 77 {
		t.Fatalf("total = %d, want 77", s.Total)
	}
	if s.ByField["status"] != 12 {
		t.Fatalf("by_field lost, got %+v", s.ByField)
	}
	// 失败项应为空，不应被写入脏数据
	if len(s.ByResourceType) != 0 {
		t.Fatalf("by_resource_type should be empty on failure, got %+v", s.ByResourceType)
	}
	if len(s.ByProvider) != 0 {
		t.Fatalf("by_provider should be empty on failure, got %+v", s.ByProvider)
	}
}

// TestGetSummary_AllFailed 全部统计失败时应返回错误，避免用空数据误导调用方。
func TestGetSummary_AllFailed(t *testing.T) {
	e := errors.New("connection reset")
	dao := &fakeChangeDAO{
		errTotal:      e,
		errByModel:    e,
		errByField:    e,
		errByProvider: e,
	}

	s, err := newTracker(dao).GetSummary(context.Background(), domain.ChangeFilter{})
	if err == nil {
		t.Fatal("all-failed should return error")
	}
	if s != nil {
		t.Fatalf("summary should be nil on total failure, got %+v", s)
	}
}
