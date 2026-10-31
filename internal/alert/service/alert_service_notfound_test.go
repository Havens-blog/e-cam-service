package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	"go.mongodb.org/mongo-driver/mongo"
)

// stubAlertDAO 只实现本测试需要的两个方法，其余通过**嵌入接口**满足（nil 调用即 panic，未用到就不会触发）。
type stubAlertDAO struct {
	dao.AlertDAO
	getRuleFn    func(ctx context.Context, id int64) (domain.AlertRule, error)
	getChannelFn func(ctx context.Context, id int64) (domain.NotificationChannel, error)
}

func (s *stubAlertDAO) GetRuleByID(ctx context.Context, id int64) (domain.AlertRule, error) {
	if s.getRuleFn != nil {
		return s.getRuleFn(ctx, id)
	}
	return domain.AlertRule{}, nil
}

func (s *stubAlertDAO) GetChannelByID(ctx context.Context, id int64) (domain.NotificationChannel, error) {
	if s.getChannelFn != nil {
		return s.getChannelFn(ctx, id)
	}
	return domain.NotificationChannel{}, nil
}

// 回归：告警规则/渠道不存在时，**不得**把 mongo 原始错误冒给调用方。
//
// 实测缺陷（2026-10-31）：
//
//	GET /api/v1/cam/alert/rules/999999    → 500 {"code":500,"msg":"mongo: no documents in result"}
//	GET /api/v1/cam/alert/channels/999999 → 500 {"code":500,"msg":"mongo: no documents in result"}
//
// 根因：GetRule/GetChannel 是 DAO 的纯透传（未归一领域错误），handler 又直接 err.Error() 当文案。
func TestGetRule_NotFoundIsDomainError(t *testing.T) {
	svc := &AlertService{dao: &stubAlertDAO{getRuleFn: func(context.Context, int64) (domain.AlertRule, error) {
		return domain.AlertRule{}, mongo.ErrNoDocuments
	}}}

	_, err := svc.GetRule(context.Background(), 999999)

	if !errors.Is(err, ErrAlertRuleNotFound) {
		t.Fatalf("应为 ErrAlertRuleNotFound，实际: %v", err)
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatal("不得把 mongo.ErrNoDocuments 冒出去")
	}
	if got := err.Error(); contains(got, "mongo") {
		t.Fatalf("错误文案不得包含存储实现细节，实际: %s", got)
	}
}

func TestGetChannel_NotFoundIsDomainError(t *testing.T) {
	svc := &AlertService{dao: &stubAlertDAO{getChannelFn: func(context.Context, int64) (domain.NotificationChannel, error) {
		return domain.NotificationChannel{}, mongo.ErrNoDocuments
	}}}

	_, err := svc.GetChannel(context.Background(), 999999)

	if !errors.Is(err, ErrAlertChannelNotFound) {
		t.Fatalf("应为 ErrAlertChannelNotFound，实际: %v", err)
	}
	if got := err.Error(); contains(got, "mongo") {
		t.Fatalf("错误文案不得包含存储实现细节，实际: %s", got)
	}
}

// 其他存储错误保持原样返回（交由 API 层按 500 处理），避免误判为"不存在"。
func TestGetRule_OtherErrorPassThrough(t *testing.T) {
	boom := errors.New("connection reset by peer")
	svc := &AlertService{dao: &stubAlertDAO{getRuleFn: func(context.Context, int64) (domain.AlertRule, error) {
		return domain.AlertRule{}, boom
	}}}

	_, err := svc.GetRule(context.Background(), 1)
	if !errors.Is(err, boom) {
		t.Fatalf("其他错误应原样返回，实际: %v", err)
	}
	if errors.Is(err, ErrAlertRuleNotFound) {
		t.Fatal("非 not-found 不得被误判为不存在")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
