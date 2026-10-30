// Package export 日志异步导出任务(Phase 1)。
//
// 解决"大窗口命中量远超 3000 采样上限"的 RCA 场景:任务在云引擎侧逐源分页
// 流式拉取(经 SDK LogExporter 能力),写 CSV 文件,绕开联邦网关的内存归并硬顶。
// 任务元数据落 MongoDB;文件落本地临时目录,TTL 到期清理。
package export

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cloudx-sdk/logquery"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gotomicro/ego/core/elog"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// 导出任务集合与上限常量。
const (
	taskCollection = "ecam_log_export_task"
	// DefaultMaxEntries 单任务最大导出条数(2026-10-30 口径确认:50 万)。
	DefaultMaxEntries = 500_000
	// DefaultTTL 文件与任务元数据保留时长(24h)。
	DefaultTTL = 24 * time.Hour
	// pageSize 每次 emit 回调的批大小。
	pageSize = 500
)

// TaskStatus 任务状态。
type TaskStatus string

const (
	TaskQueued   TaskStatus = "queued"
	TaskRunning  TaskStatus = "running"
	TaskDone     TaskStatus = "done"
	TaskFailed   TaskStatus = "failed"
	TaskCanceled TaskStatus = "canceled"
)

// Task 导出任务(元数据落库;刻意不存关键字 query 与字段筛选值)。
type Task struct {
	ID           string     `json:"id" bson:"id"`
	TenantID     int64      `json:"tenant_id" bson:"tenant_id"`
	OperatorID   string     `json:"operator_id" bson:"operator_id"`
	OperatorName string     `json:"operator_name" bson:"operator_name"`
	LogType      string     `json:"log_type" bson:"log_type"`
	StartTime    int64      `json:"start_time" bson:"start_time"`
	EndTime      int64      `json:"end_time" bson:"end_time"`
	Clouds       []string   `json:"clouds" bson:"clouds"`
	AccountIDs   []int64    `json:"account_ids" bson:"account_ids"`
	Regions      []string   `json:"regions" bson:"regions"`
	Resources    []string   `json:"resources" bson:"resources"`
	Status       TaskStatus `json:"status" bson:"status"`
	Total        int        `json:"total" bson:"total"`
	FileName     string     `json:"file_name" bson:"file_name"`
	Error        string     `json:"error" bson:"error"`
	CreatedAt    int64      `json:"created_at" bson:"created_at"`
	FinishedAt   int64      `json:"finished_at" bson:"finished_at"`
	Canceled     bool       `json:"canceled" bson:"canceled"`

	// 以下为内存态(不落库,不返回):关键字与字段筛选是敏感检索词,只供 runner
	// 拉取时用,任务台账只保留脱敏范围(clouds/accounts/regions/resources/时间窗)。
	query   string
	filters []logquery.FieldFilter
}

// CreateRequest 创建导出任务的请求。
type CreateRequest struct {
	LogType    string
	StartTime  int64
	EndTime    int64
	Query      string
	Clouds     []string
	AccountIDs []int64
	Regions    []string
	Resources  []string
	Filters    []logquery.FieldFilter
}

// AccountSource 云账号只读源(仓储窄接口;与 service.AccountSource 同构)。
type AccountSource interface {
	List(ctx context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error)
}

// Service 导出任务服务。
type Service struct {
	accounts   AccountSource
	db         *mongox.Mongo
	logger     *elog.Component
	exportDir  string
	maxEntries int
	ttl        time.Duration

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

// NewService 构造导出任务服务。
func NewService(accounts AccountSource, db *mongox.Mongo, logger *elog.Component, exportDir string) *Service {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	if exportDir == "" {
		exportDir = filepath.Join(os.TempDir(), "ecam-log-export")
	}
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		logger.Warn("[logquery-export] mkdir failed", elog.FieldErr(err))
	}
	return &Service{
		accounts:   accounts,
		db:         db,
		logger:     logger,
		exportDir:  exportDir,
		maxEntries: DefaultMaxEntries,
		ttl:        DefaultTTL,
		cancel:     make(map[string]context.CancelFunc),
	}
}

// collection 返回任务集合。
func (s *Service) collection() *mongo.Collection {
	return s.db.Collection(taskCollection)
}

// Create 校验并创建任务,异步启动 runner;返回任务 ID。
func (s *Service) Create(ctx context.Context, tenantID int64, operatorID, operatorName string, req CreateRequest) (string, error) {
	if !logquery.IsValidLogType(logquery.LogType(req.LogType)) {
		return "", fmt.Errorf("invalid log type: %s", req.LogType)
	}
	if req.EndTime <= req.StartTime {
		return "", fmt.Errorf("invalid time window: end %d <= start %d", req.EndTime, req.StartTime)
	}

	task := &Task{
		ID:           uuid.New().String(),
		TenantID:     tenantID,
		OperatorID:   operatorID,
		OperatorName: operatorName,
		LogType:      req.LogType,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		Clouds:       append([]string(nil), req.Clouds...),
		AccountIDs:   append([]int64(nil), req.AccountIDs...),
		Regions:      append([]string(nil), req.Regions...),
		Resources:    append([]string(nil), req.Resources...),
		Status:       TaskQueued,
		CreatedAt:    time.Now().UnixMilli(),
		query:        req.Query,
		filters:      append([]logquery.FieldFilter(nil), req.Filters...),
	}
	if _, err := s.collection().InsertOne(ctx, task); err != nil {
		return "", fmt.Errorf("insert export task: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancel[task.ID] = cancel
	s.mu.Unlock()
	go s.run(runCtx, task)

	return task.ID, nil
}

// Get 查询任务(租户隔离)。
func (s *Service) Get(ctx context.Context, tenantID int64, taskID string) (*Task, error) {
	var t Task
	if err := s.collection().FindOne(ctx, bson.M{"id": taskID, "tenant_id": tenantID}).Decode(&t); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("export task not found")
		}
		return nil, err
	}
	return &t, nil
}

// Cancel 取消任务(仅 queued/running 可取消)。
func (s *Service) Cancel(ctx context.Context, tenantID int64, taskID string) error {
	var t Task
	if err := s.collection().FindOne(ctx, bson.M{"id": taskID, "tenant_id": tenantID}).Decode(&t); err != nil {
		return fmt.Errorf("export task not found")
	}
	if t.Status != TaskQueued && t.Status != TaskRunning {
		return fmt.Errorf("task not cancelable: %s", t.Status)
	}
	s.mu.Lock()
	cancel, ok := s.cancel[taskID]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	_, err := s.collection().UpdateOne(ctx,
		bson.M{"id": taskID, "tenant_id": tenantID},
		bson.M{"$set": bson.M{"status": TaskCanceled, "canceled": true, "finished_at": time.Now().UnixMilli()}},
	)
	return err
}

// updateStatus 更新任务状态。
func (s *Service) updateStatus(ctx context.Context, taskID string, status TaskStatus, total int, errMsg string) {
	set := bson.M{"status": status, "total": total, "finished_at": time.Now().UnixMilli()}
	if errMsg != "" {
		set["error"] = errMsg
	}
	if _, err := s.collection().UpdateOne(ctx, bson.M{"id": taskID}, bson.M{"$set": set}); err != nil {
		s.logger.Warn("[logquery-export] update status failed", elog.FieldErr(err), elog.String("task", taskID))
	}
}

// run 异步执行:逐账号逐源分页拉取,流式写 CSV。
func (s *Service) run(ctx context.Context, task *Task) {
	defer func() {
		s.mu.Lock()
		delete(s.cancel, task.ID)
		s.mu.Unlock()
	}()

	s.collection().UpdateOne(ctx, bson.M{"id": task.ID}, bson.M{"$set": bson.M{"status": TaskRunning}})

	fileName := task.ID + ".csv"
	filePath := filepath.Join(s.exportDir, fileName)
	f, err := os.Create(filePath)
	if err != nil {
		s.updateStatus(ctx, task.ID, TaskFailed, 0, err.Error())
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write([]string{"timestamp", "cloud", "account_name", "region", "resource_id", "content"}); err != nil {
		s.updateStatus(ctx, task.ID, TaskFailed, 0, err.Error())
		return
	}

	total, err := s.pullAll(ctx, task, w)
	if err != nil {
		if ctx.Err() != nil {
			s.updateStatus(ctx, task.ID, TaskCanceled, total, "")
		} else {
			s.updateStatus(ctx, task.ID, TaskFailed, total, err.Error())
		}
		return
	}
	w.Flush()
	if err := w.Error(); err != nil {
		s.updateStatus(ctx, task.ID, TaskFailed, total, err.Error())
		return
	}

	if _, err := s.collection().UpdateOne(ctx,
		bson.M{"id": task.ID},
		bson.M{"$set": bson.M{"status": TaskDone, "total": total, "file_name": fileName, "finished_at": time.Now().UnixMilli()}},
	); err != nil {
		s.logger.Warn("[logquery-export] update done failed", elog.FieldErr(err))
	}
	s.logger.Info("[logquery-export] task done", elog.String("task", task.ID), elog.Int("total", total))
}

// pullAll 拉取全部命中日志并写 CSV,返回总条数。
func (s *Service) pullAll(ctx context.Context, task *Task, w *csv.Writer) (int, error) {
	accounts, err := s.activeAccounts(ctx, task)
	if err != nil {
		return 0, err
	}
	total := 0
	for i := range accounts {
		acc := accounts[i]
		if ctx.Err() != nil {
			break
		}
		creator, err := logquery.GetProvider(acc.Provider, logquery.LogType(task.LogType))
		if err != nil {
			continue // 未注册 (云,类型) 组合跳过
		}
		p, err := creator(&acc)
		if err != nil {
			continue
		}
		params := logquery.SearchParams{
			StartTime: task.StartTime,
			EndTime:   task.EndTime,
			Query:     task.query,
			Limit:     pageSize,
			Regions:   task.Regions,
			Resources: task.Resources,
			Filters:   task.filters,
		}
		// 优先流式导出能力;无此能力回退 Search(采样,能力边界)。
		if ex, ok := p.(logquery.LogExporter); ok {
			n, err := ex.Export(ctx, &acc, params, func(batch []logquery.LogEntry) error {
				return s.writeBatch(w, &total, batch)
			})
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				s.logger.Warn("[logquery-export] export account failed",
					elog.String("cloud", string(acc.Provider)), elog.Int64("account", acc.ID), elog.FieldErr(err))
				continue
			}
			_ = n
		} else {
			entries, err := p.Search(ctx, &acc, params)
			if err != nil {
				s.logger.Warn("[logquery-export] search fallback failed",
					elog.String("cloud", string(acc.Provider)), elog.Int64("account", acc.ID), elog.FieldErr(err))
				continue
			}
			if err := s.writeBatch(w, &total, entries); err != nil {
				return total, err
			}
		}
		if total >= s.maxEntries {
			break
		}
	}
	return total, nil
}

// writeBatch 写一批条目,受 maxEntries 硬顶。
func (s *Service) writeBatch(w *csv.Writer, total *int, batch []logquery.LogEntry) error {
	for _, e := range batch {
		if *total >= s.maxEntries {
			return nil // 达到硬顶,静默停止(任务仍 done)
		}
		meta := e.GetMeta()
		content, _ := json.Marshal(e)
		if err := w.Write([]string{
			time.UnixMilli(e.GetTimestamp()).Format(time.RFC3339),
			string(meta.Cloud),
			meta.AccountName,
			meta.Region,
			meta.ResourceID,
			string(content),
		}); err != nil {
			return err
		}
		*total++
	}
	return nil
}

// activeAccounts 租户内活跃账号(按云/账号过滤)。
func (s *Service) activeAccounts(ctx context.Context, task *Task) ([]domain.CloudAccount, error) {
	filter := domain.CloudAccountFilter{
		Status:   domain.CloudAccountStatusActive,
		TenantID: task.TenantID,
	}
	if len(task.Clouds) == 1 {
		filter.Provider = domain.CloudProvider(task.Clouds[0])
	}
	accounts, _, err := s.accounts.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(task.Clouds) > 1 {
		allowed := make(map[domain.CloudProvider]bool, len(task.Clouds))
		for _, c := range task.Clouds {
			allowed[domain.CloudProvider(c)] = true
		}
		out := accounts[:0]
		for _, a := range accounts {
			if allowed[a.Provider] {
				out = append(out, a)
			}
		}
		accounts = out
	}
	if len(task.AccountIDs) > 0 {
		want := make(map[int64]bool, len(task.AccountIDs))
		for _, id := range task.AccountIDs {
			want[id] = true
		}
		out := accounts[:0]
		for _, a := range accounts {
			if want[a.ID] {
				out = append(out, a)
			}
		}
		accounts = out
	}
	return accounts, nil
}

// FilePath 任务文件绝对路径(download 端点用;校验任务状态后调用)。
func (s *Service) FilePath(ctx context.Context, tenantID int64, taskID string) (string, error) {
	t, err := s.Get(ctx, tenantID, taskID)
	if err != nil {
		return "", err
	}
	if t.Status != TaskDone || t.FileName == "" {
		return "", fmt.Errorf("export task not ready: %s", t.Status)
	}
	return filepath.Join(s.exportDir, t.FileName), nil
}

// Cleanup 清理过期任务与文件(后台定时调用)。
func (s *Service) Cleanup(ctx context.Context) error {
	cutoff := time.Now().Add(-s.ttl).UnixMilli()
	cur, err := s.collection().Find(ctx, bson.M{"created_at": bson.M{"$lt": cutoff}})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)
	var expired []Task
	if err := cur.All(ctx, &expired); err != nil {
		return err
	}
	for _, t := range expired {
		if t.FileName != "" {
			_ = os.Remove(filepath.Join(s.exportDir, t.FileName))
		}
	}
	_, err = s.collection().DeleteMany(ctx, bson.M{"created_at": bson.M{"$lt": cutoff}})
	return err
}

// StartCleanupLoop 后台清理循环(进程生命周期;ctx 控制停止)。
func (s *Service) StartCleanupLoop(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.Cleanup(context.Background()); err != nil {
					s.logger.Warn("[logquery-export] cleanup failed", elog.FieldErr(err))
				}
			}
		}
	}()
}

// CanExport 导出权限判定(与前端 canExportLogs 同口径)。
// isAdmin / 持 cam:logs:export / 空清单默认放行(向后兼容)。
func CanExport(isAdmin bool, codes []string) bool {
	if isAdmin {
		return true
	}
	for _, c := range codes {
		if c == "cam:logs:export" {
			return true
		}
	}
	return len(codes) == 0
}


