// 云端发现导入会话仓储内存假实现（cert-cloud-discovery-import 任务 2 起供
// service/web 层测试共享：任务 4 服务编排与任务 5 会话端点注入）。
// 纯存储语义：模拟 DEFAULT 填充与 RecordItemResult/MarkFinished 的字段效果，
// 与真实仓储原子语义一致，不做业务校验（业务校验属 service 层）。
package certtest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// FakeDiscoveryImportSessionRepo 云端发现导入会话内存假实现。
type FakeDiscoveryImportSessionRepo struct {
	mu       sync.Mutex
	sessions map[string]*domain.DiscoveryImportSession
}

// NewFakeDiscoveryImportSessionRepo 创建空会话假实现。
func NewFakeDiscoveryImportSessionRepo() *FakeDiscoveryImportSessionRepo {
	return &FakeDiscoveryImportSessionRepo{sessions: map[string]*domain.DiscoveryImportSession{}}
}

// Create 写入会话（模拟 DEFAULT 填充：createdAt=now、status=running、items 空数组）
// 并返回会话 ID。
func (f *FakeDiscoveryImportSessionRepo) Create(_ context.Context, s *domain.DiscoveryImportSession) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if s.Status == "" {
		s.Status = domain.DiscoveryImportRunning
	}
	if s.Items == nil {
		s.Items = []domain.DiscoveryImportItem{}
	}
	if s.ID.IsZero() {
		s.ID = primitive.NewObjectID()
	}
	stored := *s
	stored.Items = append([]domain.DiscoveryImportItem(nil), s.Items...)
	f.sessions[s.ID.Hex()] = &stored
	return s.ID.Hex(), nil
}

// GetByID 查询会话；非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (f *FakeDiscoveryImportSessionRepo) GetByID(_ context.Context, id string) (domain.DiscoveryImportSession, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return domain.DiscoveryImportSession{}, fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[oid.Hex()]
	if !ok {
		return domain.DiscoveryImportSession{}, mongo.ErrNoDocuments
	}
	return cloneDiscoverySession(s), nil
}

// RecordItemResult 记录单条目结果并递增 progress（与真实仓储原子语义一致）。
func (f *FakeDiscoveryImportSessionRepo) RecordItemResult(_ context.Context, id string, itemIndex int, result domain.DiscoveryItemResult, mappedCertID, errorReason string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[oid.Hex()]
	if !ok {
		return mongo.ErrNoDocuments
	}
	if itemIndex < 0 || itemIndex >= len(s.Items) {
		return fmt.Errorf("certtest: item index %d out of range (%d items)", itemIndex, len(s.Items))
	}
	s.Items[itemIndex].Result = result
	s.Items[itemIndex].MappedCertID = mappedCertID
	s.Items[itemIndex].ErrorReason = errorReason
	switch result {
	case domain.DiscoveryItemSuccess:
		s.Progress.Succeeded++
	case domain.DiscoveryItemFailed:
		s.Progress.Failed++
	}
	return nil
}

// MarkFinished 终态收敛（按失败计数，与真实仓储管道更新语义一致）：
// failed>0 → partial_failed，否则 completed；finishedAt=now。
func (f *FakeDiscoveryImportSessionRepo) MarkFinished(_ context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[oid.Hex()]
	if !ok {
		return mongo.ErrNoDocuments
	}
	if s.Progress.Failed > 0 {
		s.Status = domain.DiscoveryImportPartialFailed
	} else {
		s.Status = domain.DiscoveryImportCompleted
	}
	now := time.Now()
	s.FinishedAt = &now
	return nil
}

// cloneDiscoverySession 深拷贝（隔离切片/指针状态）。
func cloneDiscoverySession(s *domain.DiscoveryImportSession) domain.DiscoveryImportSession {
	out := *s
	out.Items = append([]domain.DiscoveryImportItem(nil), s.Items...)
	if s.FinishedAt != nil {
		fa := *s.FinishedAt
		out.FinishedAt = &fa
	}
	return out
}
