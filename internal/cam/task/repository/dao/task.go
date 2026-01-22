package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollectionTask = "tasks"
)

// TaskDAO 任务数据访问对象
type TaskDAO interface {
	// Create 创建任务
	Create(ctx context.Context, task domain.Task) error

	// GetByID 根据ID获取任务
	GetByID(ctx context.Context, id string) (domain.Task, error)

	// Update 更新任务
	Update(ctx context.Context, task domain.Task) error

	// UpdateStatus 更新任务状态
	UpdateStatus(ctx context.Context, id string, status domain.TaskStatus, message string) error

	// UpdateProgress 更新任务进度
	UpdateProgress(ctx context.Context, id string, progress int, message string) error

	// List 获取任务列表
	List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, error)

	// Count 统计任务数量
	Count(ctx context.Context, filter domain.TaskFilter) (int64, error)

	// Delete 删除任务
	Delete(ctx context.Context, id string) error
}

type taskDAO struct {
	db *mongox.Mongo
}

// NewTaskDAO 创建任务DAO
func NewTaskDAO(db *mongox.Mongo) TaskDAO {
	return &taskDAO{db: db}
}

// Create 创建任务
func (dao *taskDAO) Create(ctx context.Context, task domain.Task) error {
	collection := dao.db.Collection(CollectionTask)
	_, err := collection.InsertOne(ctx, task)
	return err
}

// GetByID 根据ID获取任务
func (dao *taskDAO) GetByID(ctx context.Context, id string) (domain.Task, error) {
	collection := dao.db.Collection(CollectionTask)

	var task domain.Task
	err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&task)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.Task{}, nil
		}
		return domain.Task{}, err
	}

	return task, nil
}

// Update 更新任务
func (dao *taskDAO) Update(ctx context.Context, task domain.Task) error {
	collection := dao.db.Collection(CollectionTask)

	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": task.ID},
		bson.M{"$set": task},
	)

	return err
}

// UpdateStatus 更新任务状态
func (dao *taskDAO) UpdateStatus(ctx context.Context, id string, status domain.TaskStatus, message string) error {
	collection := dao.db.Collection(CollectionTask)

	update := bson.M{
		"status":  status,
		"message": message,
	}

	// 如果状态是运行中，设置开始时间
	if status == domain.TaskStatusRunning {
		now := time.Now()
		update["started_at"] = now
	}

	// 如果状态是完成或失败，设置完成时间和计算时长
	if status == domain.TaskStatusCompleted || status == domain.TaskStatusFailed || status == domain.TaskStatusCancelled {
		now := time.Now()
		update["completed_at"] = now

		// 获取任务以计算时长
		var task domain.Task
		err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&task)
		if err == nil && task.StartedAt != nil {
			duration := now.Sub(*task.StartedAt).Seconds()
			update["duration"] = int64(duration)
		}
	}

	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": update},
	)

	return err
}

// UpdateProgress 更新任务进度
func (dao *taskDAO) UpdateProgress(ctx context.Context, id string, progress int, message string) error {
	collection := dao.db.Collection(CollectionTask)

	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{
			"progress": progress,
			"message":  message,
		}},
	)

	return err
}

// List 获取任务列表
func (dao *taskDAO) List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, error) {
	collection := dao.db.Collection(CollectionTask)

	// 构建查询条件
	query := bson.M{}

	if filter.Type != "" {
		query["type"] = filter.Type
	}

	if filter.Status != "" {
		query["status"] = filter.Status
	}

	if filter.CreatedBy != "" {
		query["created_by"] = filter.CreatedBy
	}

	if !filter.StartDate.IsZero() || !filter.EndDate.IsZero() {
		dateQuery := bson.M{}
		if !filter.StartDate.IsZero() {
			dateQuery["$gte"] = filter.StartDate
		}
		if !filter.EndDate.IsZero() {
			dateQuery["$lte"] = filter.EndDate
		}
		query["created_at"] = dateQuery
	}

	// 设置分页和排序
	opts := options.Find()
	opts.SetSort(bson.D{{Key: "created_at", Value: -1}}) // 按创建时间倒序

	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}

	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}

	cursor, err := collection.Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tasks []domain.Task
	if err = cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}

	return tasks, nil
}

// Count 统计任务数量
func (dao *taskDAO) Count(ctx context.Context, filter domain.TaskFilter) (int64, error) {
	collection := dao.db.Collection(CollectionTask)

	// 构建查询条件
	query := bson.M{}

	if filter.Type != "" {
		query["type"] = filter.Type
	}

	if filter.Status != "" {
		query["status"] = filter.Status
	}

	if filter.CreatedBy != "" {
		query["created_by"] = filter.CreatedBy
	}

	if !filter.StartDate.IsZero() || !filter.EndDate.IsZero() {
		dateQuery := bson.M{}
		if !filter.StartDate.IsZero() {
			dateQuery["$gte"] = filter.StartDate
		}
		if !filter.EndDate.IsZero() {
			dateQuery["$lte"] = filter.EndDate
		}
		query["created_at"] = dateQuery
	}

	return collection.CountDocuments(ctx, query)
}

// Delete 删除任务
func (dao *taskDAO) Delete(ctx context.Context, id string) error {
	collection := dao.db.Collection(CollectionTask)
	_, err := collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// InitTaskIndexes 初始化任务索引
func InitTaskIndexes(db *mongox.Mongo) error {
	collection := db.Collection(CollectionTask)

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "type", Value: 1},
				{Key: "status", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "created_by", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "created_at", Value: -1},
			},
		},
	}

	_, err := collection.Indexes().CreateMany(context.Background(), indexes)
	return err
}
