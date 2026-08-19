package web

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// CertHandler 证书导入/批量导入/补传私钥端点（/api/v1/certs）。
type CertHandler struct {
	svc service.ImportService
}

// NewCertHandler 创建证书导入 handler。
func NewCertHandler(svc service.ImportService) *CertHandler {
	return &CertHandler{svc: svc}
}

// RegisterRoutes 注册导入相关端点（角色门卫按 api-handbook Auth 列，7.2）：
// 导入/批量导入/批量会话轮询/补传私钥均限运维工程师。
func (h *CertHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("", RequireRoles(RoleOpsEngineer), h.ImportCert)
	g.POST("/batch", RequireRoles(RoleOpsEngineer), h.ImportBatch)
	g.GET("/batch/:batchId", RequireRoles(RoleOpsEngineer), h.GetBatch)
	g.POST("/:id/key", RequireRoles(RoleOpsEngineer), h.UploadKey)
}

// ImportCertVO 导入/补传成功响应数据（Hard Rule：仅 certId/fingerprint/hostingStatus）。
type ImportCertVO struct {
	CertID        string `json:"certId"`
	Fingerprint   string `json:"fingerprint"`
	HostingStatus string `json:"hostingStatus"`
}

// expectedDomainMeta expectedDomain 提示性比对结果（响应 meta 字段，不拦截）。
type expectedDomainMeta struct {
	ExpectedDomainMissing []string `json:"expectedDomainMissing"`
}

// BatchVO 批量导入会话同构响应（POST /batch 202 与 GET /batch/:batchId 共用）。
type BatchVO struct {
	BatchID  string          `json:"batchId"`
	Status   string          `json:"status"`
	Files    []BatchFileVO   `json:"files"`
	Progress BatchProgressVO `json:"progress"`
}

// BatchFileVO 批量导入逐文件结果（失败行含 errorReason，可单独重试）。
type BatchFileVO struct {
	FileName    string `json:"fileName"`
	Result      string `json:"result"`
	CertID      string `json:"certId,omitempty"`
	ErrorReason string `json:"errorReason,omitempty"`
}

// BatchProgressVO 批量导入进度 {total,done,failed}。
type BatchProgressVO struct {
	Total  int `json:"total"`
	Done   int `json:"done"`
	Failed int `json:"failed"`
}

// ImportCert POST /api/v1/certs —— 单张导入（multipart certFile + 可选 keyFile + 可选 expectedDomain）。
func (h *CertHandler) ImportCert(c *gin.Context) {
	certPEM, err := readFileField(c, "certFile", true)
	if err != nil {
		return // readFileField 已输出错误信封
	}
	keyPEM, err := readFileField(c, "keyFile", false)
	if err != nil {
		return
	}
	defer domain.Zeroize(&keyPEM) // 明文私钥用后清零（Hard Rule）

	res, err := h.svc.ImportCert(c.Request.Context(), certPEM, keyPEM,
		strings.TrimSpace(c.PostForm("expectedDomain")))
	if err != nil {
		WriteError(c, err)
		return
	}

	var meta any
	if len(res.ExpectedDomainMissing) > 0 {
		meta = expectedDomainMeta{ExpectedDomainMissing: res.ExpectedDomainMissing}
	}
	WriteOK(c, http.StatusOK, toImportVO(res), meta)
}

// ImportBatch POST /api/v1/certs/batch —— 批量导入（multipart 多文件 certFiles +
// 逐文件可选私钥 keyFiles，按去扩展名基名配对），202 返回会话句柄。
func (h *CertHandler) ImportBatch(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "multipart form required")
		return
	}
	certHeaders := form.File["certFiles"]
	if len(certHeaders) == 0 {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "certFiles is required")
		return
	}
	// 私钥按文件基名（去最后一个扩展名、小写）与证书配对；同名冲突取首个。
	keyByBase := make(map[string]*multipart.FileHeader, len(form.File["keyFiles"]))
	for _, kh := range form.File["keyFiles"] {
		base := fileBase(kh.Filename)
		if _, exists := keyByBase[base]; !exists {
			keyByBase[base] = kh
		}
	}

	files := make([]service.BatchFileInput, 0, len(certHeaders))
	for _, ch := range certHeaders {
		certPEM, err := readFileHeader(c, ch)
		if err != nil {
			return
		}
		input := service.BatchFileInput{FileName: ch.Filename, CertPEM: certPEM}
		if kh, ok := keyByBase[fileBase(ch.Filename)]; ok {
			keyPEM, err := readFileHeader(c, kh)
			if err != nil {
				return
			}
			input.KeyPEM = keyPEM
		}
		files = append(files, input)
	}

	operator := middleware.GetUsername(c)
	if operator == "" {
		operator = "unknown"
	}
	batchID, err := h.svc.ImportBatch(c.Request.Context(), files, operator)
	if err != nil {
		WriteError(c, err)
		return
	}

	// 202 快照：会话已持久化，初始形态 running/pending（终态经轮询获取）。
	vo := BatchVO{
		BatchID:  batchID,
		Status:   string(domain.BatchSessionRunning),
		Files:    make([]BatchFileVO, 0, len(files)),
		Progress: BatchProgressVO{Total: len(files)},
	}
	for _, f := range files {
		vo.Files = append(vo.Files, BatchFileVO{
			FileName: f.FileName,
			Result:   string(domain.BatchFilePending),
		})
	}
	WriteOK(c, http.StatusAccepted, vo, nil)
}

// GetBatch GET /api/v1/certs/batch/:batchId —— 批量导入进度轮询（同构响应）。
func (h *CertHandler) GetBatch(c *gin.Context) {
	sess, err := h.svc.GetBatchSession(c.Request.Context(), c.Param("batchId"))
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toBatchVO(sess), nil)
}

// UploadKey POST /api/v1/certs/:id/key —— 补传私钥升级完整托管（multipart keyFile）。
func (h *CertHandler) UploadKey(c *gin.Context) {
	keyPEM, err := readFileField(c, "keyFile", true)
	if err != nil {
		return
	}
	defer domain.Zeroize(&keyPEM) // 明文私钥用后清零（Hard Rule）

	res, err := h.svc.UploadKey(c.Request.Context(), c.Param("id"), keyPEM)
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toImportVO(res), nil)
}

// toImportVO 服务结果 → 响应 VO（仅三字段，剔除一切内部/敏感载荷）。
func toImportVO(res service.ImportResult) ImportCertVO {
	return ImportCertVO{
		CertID:        res.CertID,
		Fingerprint:   res.Fingerprint,
		HostingStatus: string(res.HostingStatus),
	}
}

// toBatchVO 会话文档 → 同构响应 VO。
func toBatchVO(sess domain.CertBatchSession) BatchVO {
	vo := BatchVO{
		BatchID:  sess.ID.Hex(),
		Status:   string(sess.Status),
		Files:    make([]BatchFileVO, 0, len(sess.Files)),
		Progress: BatchProgressVO{Total: sess.Progress.Total, Done: sess.Progress.Done, Failed: sess.Progress.Failed},
	}
	for _, f := range sess.Files {
		vo.Files = append(vo.Files, BatchFileVO{
			FileName:    f.FileName,
			Result:      string(f.Result),
			CertID:      f.CertID,
			ErrorReason: f.ErrorReason,
		})
	}
	return vo
}

// readFileField 读取必填/可选文件字段；缺失或读取失败时输出错误信封并返回错误。
func readFileField(c *gin.Context, field string, required bool) ([]byte, error) {
	fh, err := c.FormFile(field)
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) && !required {
			return nil, nil
		}
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, field+" is required")
		return nil, err
	}
	data, err := readFileHeader(c, fh)
	if err != nil {
		return nil, err
	}
	if required && len(data) == 0 {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, field+" is empty")
		return nil, errors.New(field + " is empty")
	}
	return data, nil
}

// readFileHeader 读取单个文件头内容；读取失败输出 400 错误信封。
func readFileHeader(c *gin.Context, fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "cannot open uploaded file "+fh.Filename)
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "cannot read uploaded file "+fh.Filename)
		return nil, err
	}
	return data, nil
}

// fileBase 文件基名：去路径与最后一个扩展名，小写（证书/私钥同名配对键）。
func fileBase(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	return strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base)))
}
