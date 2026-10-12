package service

import (
	"context"
	"testing"

	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/gotomicro/ego/core/elog"
)

// mockAuditRepo captures the AuditLog passed to Create for assertions.
type mockAuditRepo struct {
	captured domain.AuditLog
}

func (m *mockAuditRepo) Create(ctx context.Context, log domain.AuditLog) (int64, error) {
	m.captured = log
	return 1, nil
}
func (m *mockAuditRepo) GetByID(ctx context.Context, id int64) (domain.AuditLog, error) {
	return domain.AuditLog{}, nil
}
func (m *mockAuditRepo) List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLog, int64, error) {
	return nil, 0, nil
}
func (m *mockAuditRepo) CountByOperationType(ctx context.Context, filter domain.AuditLogFilter) (map[domain.AuditOperationType]int64, error) {
	return nil, nil
}
func (m *mockAuditRepo) CountByCloudPlatform(ctx context.Context, filter domain.AuditLogFilter) (map[domain.CloudProvider]int64, error) {
	return nil, nil
}
func (m *mockAuditRepo) CountByResult(ctx context.Context, filter domain.AuditLogFilter) (map[domain.AuditResult]int64, error) {
	return nil, nil
}
func (m *mockAuditRepo) ListTopOperators(ctx context.Context, filter domain.AuditLogFilter, limit int) ([]domain.OperatorStats, error) {
	return nil, nil
}

func newServiceWithMockAudit() (*userGroupService, *mockAuditRepo) {
	repo := &mockAuditRepo{}
	svc := &userGroupService{
		auditRepo: repo,
		logger:    elog.DefaultLogger,
	}
	return svc, repo
}

// TestLogPolicyUpdate_OperatorFromContext verifies that logPolicyUpdate reads
// the operator from ctx via OperatorFromContext and falls back to "system"
// when no operator is present.
func TestLogPolicyUpdate_OperatorFromContext(t *testing.T) {
	tests := []struct {
		name       string
		ctx        context.Context
		wantOperator string
	}{
		{
			name:         "operator present in ctx",
			ctx:          certservice.WithOperator(context.Background(), "alice"),
			wantOperator: "alice",
		},
		{
			name:         "no operator in ctx — fallback to system",
			ctx:          context.Background(),
			wantOperator: "system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newServiceWithMockAudit()

			err := svc.logPolicyUpdate(tt.ctx, 42, "test-group", nil, nil)
			if err != nil {
				t.Fatalf("logPolicyUpdate returned error: %v", err)
			}

			if repo.captured.OperatorID != tt.wantOperator {
				t.Errorf("OperatorID = %q, want %q", repo.captured.OperatorID, tt.wantOperator)
			}
			if repo.captured.OperatorName != tt.wantOperator {
				t.Errorf("OperatorName = %q, want %q", repo.captured.OperatorName, tt.wantOperator)
			}
		})
	}
}
