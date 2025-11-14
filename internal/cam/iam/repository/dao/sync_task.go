package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CloudSyncTasksCollection = "cloud_sync_tasks"

// SyncTaskType 同步任务类型
type SyncTaskType string

const (
	SyncTaskTypeUserSync       SyncTaskType = "user_sync"
	SyncTaskTypePermissionSync SyncTaskType = "permission_sync"
	SyncTaskTypeGroupSync      SyncTaskType = "group_sync"
)

// SyncTargetType 同步目标类型
type SyncTargetType string

const (
	SyncTargetTypeUser  SyncTargetType = "user"
	SyncTargetTypeGroup SyncTargetType = "group"
)

// SyncTaskStatus 同步任务状态
type SyncTaskStatus string

const (
	SyncTaskStatusPending  SyncTaskStatus = "pending"
	SyncTaskStatusRunning  SyncTaskStatus = "running"
	SyncTaskStatusSuccess  SyncTaskStatus = "success"
	SyncTaskStatusFailed   SyncTaskStatus = "failed"
	SyncTaskStatusRetrying SyncTaskStatus = "retrying"
)

// SyncTask DAO层同步任务模型
type SyncTask struct {
	ID             int64          `bson:"id"`
	TaskType       SyncTaskType   `bson:"task_type"`
	TargetType     SyncTargetType `bson:"target_type"`
	TargetID       int64          `bson:"target_id"`
	CloudAccountID int64          `bson:"cloud_account_id"`
	Provider       CloudProvider  `bson:"provider"`
	Status         SyncTaskStatus `bson:"status"`
	Progress       int            `bson:"progress"`
	RetryCount     int            `bson:"retry_count"`
	MaxRetries     int            `bson:"max_retries"`
	ErrorMessage   string         `bson:"error_message"`
	StartTime      *time.Time     `bson:"start_time"`
	EndTime        *time.Time     `bson:"end_time"`
	CreateTime     time.Time      `bson:"create_time"`
	UpdateTime     time.Time      `bson:"update_time"`
	CTime          int64          `bson:"ctime"`
	UTime          int64          `bson:"utime"`
}

// SyncTaskFilter DAO层过滤条件
type SyncTaskFilter struct {
	TaskType       SyncTaskType
	Status         SyncTaskStatus
	CloudAccountID int64
	Provider       CloudProvider
	Offset         int64
	Limit          int64
}

// SyncTaskDAO 同步任务数据访问接口
type SyncTaskDAO interface {
	Create(ctx context.Context, task SyncTask) (int64, error)
	Update(ctx context.Context, task SyncTask) error
	GetByID(ctx context.Context, id int64) (SyncTask, error)
	List(ctx context.Context, filter SyncTaskFilter) ([]SyncTask, error)
	Count(ctx context.Context, filter SyncTaskFilter) (int64, error)
	UpdateStatus(ctx context.Context, id int64, status SyncTaskStatus) error
	UpdateProgress(ctx context.Context, id int64, progress int) error
	MarkAsRunning(ctx context.Context, id int64, startTime time.Time) error
	MarkAsSuccess(ctx context.Context, id int64, endTime time.Time) error
	MarkAsFailed(ctx context.Context, id int64, endTime time.Time, errorMsg string) error
	IncrementRetry(ctx context.Context, id int64) error
	ListPendingTasks(ctx context.Context, limit int64) ([]SyncTask, error)
	ListFailedRetryableTasks(ctx context.Context, limit int64) ([]SyncTask, error)
}

type syncTaskDAO struct {
	db *mongox.Mongo
}

// NewSyncTaskDAO 创建同步任务DAO
func NewSyncTaskDAO(db *mongox.Mongo) SyncTaskDAO {
	return &syncTaskDAO{
		db: db,
	}
}

// Create 创建同步任务
func (dao *syncTaskDAO) Create(ctx context.Context, task SyncTask) (int64, error) {
	now := time.Now()
	nowUnix := now.Unix()

	task.CreateTime = now
	task.UpdateTime = now
	task.CTime = nowUnix
	task.UTime = nowUnix

	if task.ID == 0 {
		task.ID = dao.db.GetIdGenerator(CloudSyncTasksCollection)
	}

	// 设置默认状态
	if task.Status == "" {
		task.Status = SyncTaskStatusPending
	}

	// 设置默认最大重试次数
	if task.MaxRetries == 0 {
		task.MaxRetries = 3
	}

	// 初始化进度
	if task.Progress == 0 {
		task.Progress = 0
	}

	_, err := dao.db.Collection(CloudSyncTasksCollection).InsertOne(ctx, task)
	if err != nil {
		return 0, err
	}

	return task.ID, nil
}

// Update 更新同步任务
func (dao *syncTaskDAO) Update(ctx context.Context, task SyncTask) error {
	task.UpdateTime = time.Now()
	task.UTime = task.UpdateTime.Unix()

	filter := bson.M{"id": task.ID}
	update := bson.M{"$set": task}

	_, err := dao.db.Collection(CloudSyncTasksCollection).UpdateOne(ctx, filter, update)
	return err
}

// GetByID 根据ID获取同步任务
func (dao *syncTaskDAO) GetByID(ctx context.Context, id int64) (SyncTask, error) {
	var task SyncTask
	filter := bson.M{"id": id}

	err := dao.db.Collection(CloudSyncTasksCollection).FindOne(ctx, filter).Decode(&task)
	return task, err
}

// List 获取同步任务列表
func (dao *syncTaskDAO) List(ctx context.Context, filter SyncTaskFilter) ([]SyncTask, error) {
	var tasks []SyncTask

	// 构建查询条件
	query := bson.M{}
	if filter.TaskType != "" {
		query["task_type"] = filter.TaskType
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.CloudAccountID > 0 {
		query["cloud_account_id"] = filter.CloudAccountID
	}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}

	// 设置分页选项
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.M{"ctime": -1})

	cursor, err := dao.db.Collection(CloudSyncTasksCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &tasks)
	return tasks, err
}

// Count 统计同步任务数量
func (dao *syncTaskDAO) Count(ctx context.Context, filter SyncTaskFilter) (int64, error) {
	// 构建查询条件
	query := bson.M{}
	if filter.TaskType != "" {
		query["task_type"] = filter.TaskType
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.CloudAccountID > 0 {
		query["cloud_account_id"] = filter.CloudAccountID
	}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}

	return dao.db.Collection(CloudSyncTasksCollection).CountDocuments(ctx, query)
}

// UpdateStatus 更新任务状态
func (dao *syncTaskDAO) UpdateStatus(ctx context.Context, id int64, status SyncTaskStatus) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"status":      status,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudSyncTasksCollection).UpdateOne(ctx, filter, update)
	return err
}

// UpdateProgress 更新任务进度
func (dao *syncTaskDAO) UpdateProgress(ctx context.Context, id int64, progress int) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"progress":    progress,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudSyncTasksCollection).UpdateOne(ctx, filter, update)
	return err
}

// MarkAsRunning 标记任务为执行中
func (dao *syncTaskDAO) MarkAsRunning(ctx context.Context, id int64, startTime time.Time) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"status":      SyncTaskStatusRunning,
			"start_time":  startTime,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudSyncTasksCollection).UpdateOne(ctx, filter, update)
	return err
}

// MarkAsSuccess 标记任务为成功
func (dao *syncTaskDAO) MarkAsSuccess(ctx context.Context, id int64, endTime time.Time) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"status":      SyncTaskStatusSuccess,
			"progress":    100,
			"end_time":    endTime,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudSyncTasksCollection).UpdateOne(ctx, filter, update)
	return err
}

// MarkAsFailed 标记任务为失败
func (dao *syncTaskDAO) MarkAsFailed(ctx context.Context, id int64, endTime time.Time, errorMsg string) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"status":        SyncTaskStatusFailed,
			"end_time":      endTime,
			"error_message": errorMsg,
			"update_time":   time.Now(),
			"utime":         time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudSyncTasksCollection).UpdateOne(ctx, filter, update)
	return err
}

// IncrementRetry 增加重试次数
func (dao *syncTaskDAO) IncrementRetry(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$inc": bson.M{
			"retry_count": 1,
		},
		"$set": bson.M{
			"status":      SyncTaskStatusRetrying,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudSyncTasksCollection).UpdateOne(ctx, filter, update)
	return err
}

// ListPendingTasks 获取待执行的任务列表
func (dao *syncTaskDAO) ListPendingTasks(ctx context.Context, limit int64) ([]SyncTask, error) {
	var tasks []SyncTask

	query := bson.M{"status": SyncTaskStatusPending}
	opts := options.Find().SetLimit(limit).SetSort(bson.M{"ctime": 1})

	cursor, err := dao.db.Collection(CloudSyncTasksCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &tasks)
	return tasks, err
}

// ListFailedRetryableTasks 获取失败但可重试的任务列表
func (dao *syncTaskDAO) ListFailedRetryableTasks(ctx context.Context, limit int64) ([]SyncTask, error) {
	var tasks []SyncTask

	query := bson.M{
		"status": SyncTaskStatusFailed,
		"$expr": bson.M{
			"$lt": []interface{}{"$retry_count", "$max_retries"},
		},
	}
	opts := options.Find().SetLimit(limit).SetSort(bson.M{"ctime": 1})

	cursor, err := dao.db.Collection(CloudSyncTasksCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &tasks)
	return tasks, err
}
