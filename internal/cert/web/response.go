// Package web 实现证书域 HTTP 端点（/api/v1/certs，任务 2.2 起）。
package web

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
)

// Envelope 统一响应信封（api-handbook API Overview：{success, data, error, meta}）。
// 私钥字段任何接口不返回——Data/Meta 仅承载白名单字段。
type Envelope struct {
	Success bool      `json:"success"`
	Data    any       `json:"data,omitempty"`
	Error   *APIError `json:"error,omitempty"`
	Meta    any       `json:"meta,omitempty"`
}

// APIError 错误载荷：code 为 api-handbook Error Codes 中的字符串码。
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// 通用（非证书业务）错误码：请求形态错误 / 非法 ID / 未命中 / 内部错误。
const (
	CodeInvalidRequest = "INVALID_REQUEST"
	CodeInvalidID      = "CERT_INVALID_ID"
	CodeNotFound       = "CERT_NOT_FOUND"
	CodeInternalError  = "INTERNAL_ERROR"
)

// certCodeMessages CERT_* 错误码对应的固定安全文案。
// 不回显 err.Error()（防渗透：即使底层错误信息变化也不会泄露内部细节）。
var certCodeMessages = map[string]string{
	domain.CodeCertKeyMismatch:          "certificate does not match private key",
	domain.CodeCertChainIncomplete:      "certificate chain incomplete",
	domain.CodeCertParseFail:            "certificate parse failed",
	domain.CodeCertDuplicateFingerprint: "duplicate certificate fingerprint",
	domain.CodeCertHasRefs:              "certificate has active references or is within protection period",
	domain.CodeScanInProgress:           "scan already in progress",
}

// 变更管理错误码固定文案（任务 5.11，api-handbook 变更管理错误表；409 族
// 须在 CertError 兜底分支之前显式拦截——这些哨兵同为 *CertError 实例，
// 兜底会把它们误映射为 400）。
var changeCodeMessages = map[string]string{
	domain.CodeScanStale:              "scan snapshot exceeds freshness threshold; run a scan first",
	domain.CodeSanInsufficient:        "new certificate SAN does not cover target domains",
	domain.CodeChangeInFlight:         "an active change order already exists for this certificate",
	domain.CodeNewCertFingerprintOnly: "new certificate is fingerprint-only registration (no private key to upload)",
	domain.CodeBatchNotConfirmable:    "batch continuation gate not satisfied (previous batch failed or batch verification unmet)",
	domain.CodeChangeNotCancellable:   "change order not cancellable in current state",
	domain.CodeRollbackTargetInvalid:  "rollback target invalid (deleted, expired or replaced); manual intervention required",
}

// CodeChangeStateConflict 状态机白名单外迁移的通用冲突码（5.11：confirm/
// rollback 等同步入口落在非法状态时返回 409；api-handbook 未单列该业务场景，
// 与 INVALID_REQUEST/CERT_NOT_FOUND 同属通用码层）。
const CodeChangeStateConflict = "CHANGE_STATE_CONFLICT"

// WriteOK 输出成功信封（2xx + data/meta）。
func WriteOK(c *gin.Context, status int, data, meta any) {
	c.PureJSON(status, Envelope{Success: true, Data: data, Meta: meta})
}

// WriteAPIError 输出已定性的错误信封（handler 层请求形态校验用）。
func WriteAPIError(c *gin.Context, status int, code, message string) {
	c.PureJSON(status, Envelope{Success: false, Error: &APIError{Code: code, Message: message}})
}

// WriteAPIErrorWithMeta 输出已定性错误信封并附结构化 meta（409 拦截分支：
// DeleteCert CERT_HAS_REFS / TriggerScan SCAN_IN_PROGRESS；message 取
// certCodeMessages 固定安全文案）。
func WriteAPIErrorWithMeta(c *gin.Context, status int, code string, meta any) {
	c.PureJSON(status, Envelope{
		Success: false,
		Error:   &APIError{Code: code, Message: certCodeMessages[code]},
		Meta:    meta,
	})
}

// WriteError 将领域/仓储错误映射为 HTTP status + code 信封（统一 mapper，
// 任务 2.3/3.6/4.5/5.11 复用）。映射规则（tech-design Error Handling）：
//   - CertError（CERT_KEY_MISMATCH/CERT_CHAIN_INCOMPLETE/CERT_PARSE_FAIL）→ 400
//   - ErrDuplicateFingerprint → 409 CERT_DUPLICATE_FINGERPRINT
//   - mongo.ErrNoDocuments → 404 CERT_NOT_FOUND；ErrInvalidID → 400 CERT_INVALID_ID
//   - 其余 → 500 INTERNAL_ERROR（详情仅进日志，不进响应）
func WriteError(c *gin.Context, err error) {
	status, apiErr := mapError(err)
	if status == http.StatusInternalServerError {
		slog.Error("cert api internal error", slog.Any("err", err))
	}
	c.PureJSON(status, Envelope{Success: false, Error: apiErr})
}

// mapError 错误 → (HTTP status, APIError)。消息为固定安全文案。
func mapError(err error) (int, *APIError) {
	switch {
	case errors.Is(err, domain.ErrDuplicateFingerprint):
		return http.StatusConflict, &APIError{
			Code:    domain.CodeCertDuplicateFingerprint,
			Message: certCodeMessages[domain.CodeCertDuplicateFingerprint],
		}
	case errors.Is(err, domain.ErrScanInProgress):
		// 任务 3.6：立即扫描防重（handler 层 *service.ScanInProgressError 附快照信息）
		return http.StatusConflict, &APIError{
			Code:    domain.CodeScanInProgress,
			Message: certCodeMessages[domain.CodeScanInProgress],
		}
	case errors.Is(err, domain.ErrInvalidID):
		return http.StatusBadRequest, &APIError{
			Code:    CodeInvalidID,
			Message: "invalid document id",
		}
	case errors.Is(err, mongo.ErrNoDocuments):
		return http.StatusNotFound, &APIError{
			Code:    CodeNotFound,
			Message: "resource not found",
		}
	case errors.Is(err, service.ErrEmptyBatch):
		return http.StatusBadRequest, &APIError{
			Code:    CodeInvalidRequest,
			Message: "batch import contains no files",
		}
	case errors.Is(err, deployer.ErrInvalidBatchConf):
		// 任务 5.11：Confirm batchConf 越界（batchSize<=0 / maxBatchRatio 越界）
		return http.StatusBadRequest, &APIError{
			Code:    CodeInvalidRequest,
			Message: "invalid batch configuration",
		}
	case errors.Is(err, service.ErrRollbackScopeInvalid):
		// 任务 5.11：回滚范围请求侧错误（空/外单/全非成功项）——仅成功项可回滚
		return http.StatusBadRequest, &APIError{
			Code:    CodeInvalidRequest,
			Message: "rollback request contains no rollbackable success items",
		}
	case errors.Is(err, domain.ErrChangeInFlight):
		return conflict(domain.CodeChangeInFlight)
	case errors.Is(err, domain.ErrScanStale):
		return conflict(domain.CodeScanStale)
	case errors.Is(err, domain.ErrSanInsufficient):
		return conflict(domain.CodeSanInsufficient)
	case errors.Is(err, domain.ErrNewCertFingerprintOnly):
		return conflict(domain.CodeNewCertFingerprintOnly)
	case errors.Is(err, domain.ErrBatchNotConfirmable):
		return conflict(domain.CodeBatchNotConfirmable)
	case errors.Is(err, domain.ErrChangeNotCancellable):
		return conflict(domain.CodeChangeNotCancellable)
	case errors.Is(err, domain.ErrRollbackTargetInvalid):
		return conflict(domain.CodeRollbackTargetInvalid)
	}
	var trans *domain.InvalidTransitionError
	if errors.As(err, &trans) {
		return http.StatusConflict, &APIError{
			Code:    CodeChangeStateConflict,
			Message: fmt.Sprintf("change order state transition not allowed (%s)", trans.From),
		}
	}
	if ce, ok := domain.AsCertError(err); ok {
		msg, known := certCodeMessages[ce.Code()]
		if !known {
			msg = "certificate validation failed"
		}
		return http.StatusBadRequest, &APIError{Code: ce.Code(), Message: msg}
	}
	return http.StatusInternalServerError, &APIError{
		Code:    CodeInternalError,
		Message: "internal server error",
	}
}

// conflict 变更管理 409 族错误信封（固定安全文案，任务 5.11）。
func conflict(code string) (int, *APIError) {
	return http.StatusConflict, &APIError{Code: code, Message: changeCodeMessages[code]}
}
