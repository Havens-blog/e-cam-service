package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotomicro/ego/core/elog"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/budget"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
)

// errBudgetDAO 让 Create 固定返回一个"像存储层"的错误，用于验证处理器不外泄原文且返回非 nil error。
type errBudgetDAO struct {
	repository.BudgetDAO
	err error
}

func (d *errBudgetDAO) Create(_ context.Context, _ costdomain.BudgetRule) (int64, error) {
	return 0, d.err
}

// 回归（2026-11-02）：cost 模块此前把 `err.Error()` 直接写进响应体（该模块 29 处同类站点），
// 其中 7 处写法是 `return web.ErrorResult(errs.SystemError), nil` —— 后者会被 ginx 包装器判为
// "无错误" ⇒ **HTTP 200 + 错误码**（见 e-common-go/ginx/wrap.go：仅 `err != nil` 时才给 500）。
//
// 现约定：返回**非 nil error**（框架据此给 500）；响应体只给稳定文案，细节进日志。
func TestBudgetHandlerDoesNotLeakRawErrorAndReturnsError(t *testing.T) {
	svc := budget.NewBudgetService(
		&errBudgetDAO{err: errors.New("mongo: no documents in result")},
		nil, nil, elog.DefaultLogger,
	)
	h := NewBudgetHandler(svc)

	res, err := h.CreateBudget(budgetTestCtx(""), CreateBudgetReq{
		Name:        "leak-check",
		AmountLimit: 100,
		Period:      "monthly",
		Thresholds:  []float64{80},
	})

	if err == nil {
		t.Fatal("处理器必须返回非 nil error（否则 ginx 包装器会给 HTTP 200 + 错误码）")
	}
	if strings.Contains(res.Msg, "mongo") || strings.Contains(res.Msg, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %q", res.Msg)
	}
	if res.Code != 500 {
		t.Fatalf("响应体业务码应为 500（SystemError），实际 %d", res.Code)
	}
}
