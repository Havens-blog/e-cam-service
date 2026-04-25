package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	AlertRulesCollection     = "ecam_alert_rule"
	AlertEventsCollection    = "ecam_alert_event"
	NotifyChannelsCollection = "ecam_notification_channel"
)

// AlertDAO 告警数据访问接口
type AlertDAO interface {
	// 告警规则
	CreateRule(ctx context.Context, rule domain.AlertRule) (int64, error)
	UpdateRule(ctx context.Context, rule domain.AlertRule) error
	GetRuleByID(ctx context.Context, id int64) (domain.AlertRule, error)
	ListRules(ctx context.Context, filter domain.AlertRuleFilter) ([]domain.AlertRule, int64, error)
	DeleteRule(ctx context.Context, id int64) error

	// 告警事件
	CreateEvent(ctx context.Context, event domain.AlertEvent) (int64, error)
	UpdateEventStatus(ctx context.Context, id int64, status domain.EventStatus) error
	ListEvents(ctx context.Context, filter domain.AlertEventFilter) ([]domain.AlertEvent, int64, error)
	GetPendingEvents(ctx context.Context, limit int) ([]domain.AlertEvent, error)
	IncrementRetry(ctx context.Context, id int64) error

	// 通知渠道
	CreateChannel(ctx context.Context, ch domain.NotificationChannel) (int64, error)
	UpdateChannel(ctx context.Context, ch domain.NotificationChannel) error
	GetChannelByID(ctx context.Context, id int64) (domain.NotificationChannel, error)
	ListChannels(ctx context.Context, filter domain.ChannelFilter) ([]domain.NotificationChannel, int64, error)
	DeleteChannel(ctx context.Context, id int64) error
	GetChannelsByIDs(ctx context.Context, ids []int64) ([]domain.NotificationChannel, error)

	// 索引初始化
	InitIndexes(ctx context.Context) error
}

type alertDAO struct {
	db *mongox.Mongo
}

func NewAlertDAO(db *mongox.Mongo) AlertDAO {
	return &alertDAO{db: db}
}

// InitIndexes 初始化索引
func (d *alertDAO) InitIndexes(ctx context.Context) error {
	// 告警规则索引
	rulesIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "type", Value: 1}}},
		{Keys: bson.D{{Key: "enabled", Value: 1}}},
	}
	if _, err := d.db.Collection(AlertRulesCollection).Indexes().CreateMany(ctx, rulesIndexes); err != nil {
		return err
	}

	// 告警事件索引
	eventsIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "type", Value: 1}, {Key: "create_time", Value: -1}}},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "create_time", Value: 1}}},
	}
	if _, err := d.db.Collection(AlertEventsCollection).Indexes().CreateMany(ctx, eventsIndexes); err != nil {
		return err
	}

	// 通知渠道索引
	channelIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}}},
	}
	_, err := d.db.Collection(NotifyChannelsCollection).Indexes().CreateMany(ctx, channelIndexes)
	return err
}

// ========== 告警规则 ==========

func (d *alertDAO) CreateRule(ctx context.Context, rule domain.AlertRule) (int64, error) {
	now := time.Now()
	rule.CreateTime = now
	rule.UpdateTime = now
	if rule.ID == 0 {
		rule.ID = d.db.GetIdGenerator(AlertRulesCollection)
	}
	_, err := d.db.Collection(AlertRulesCollection).InsertOne(ctx, rule)
	return rule.ID, err
}

func (d *alertDAO) UpdateRule(ctx context.Context, rule domain.AlertRule) error {
	rule.UpdateTime = time.Now()
	filter := bson.M{"id": rule.ID}
	update := bson.M{"$set": rule}
	_, err := d.db.Collection(AlertRulesCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *alertDAO) GetRuleByID(ctx context.Context, id int64) (domain.AlertRule, error) {
	var rule domain.AlertRule
	err := d.db.Collection(AlertRulesCollection).FindOne(ctx, bson.M{"id": id}).Decode(&rule)
	return rule, err
}

func (d *alertDAO) ListRules(ctx context.Context, filter domain.AlertRuleFilter) ([]domain.AlertRule, int64, error) {
	query := d.buildRuleQuery(filter)

	total, err := d.db.Collection(AlertRulesCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
		opts.SetSkip(filter.Offset)
	}

	cursor, err := d.db.Collection(AlertRulesCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var rules []domain.AlertRule
	if err := cursor.All(ctx, &rules); err != nil {
		return nil, 0, err
	}
	return rules, total, nil
}

func (d *alertDAO) DeleteRule(ctx context.Context, id int64) error {
	_, err := d.db.Collection(AlertRulesCollection).DeleteOne(ctx, bson.M{"id": id})
	return err
}

func (d *alertDAO) buildRuleQuery(filter domain.AlertRuleFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Type != "" {
		query["type"] = filter.Type
	}
	if filter.Enabled != nil {
		query["enabled"] = *filter.Enabled
	}
	return query
}

// ========== 告警事件 ==========

func (d *alertDAO) CreateEvent(ctx context.Context, event domain.AlertEvent) (int64, error) {
	event.CreateTime = time.Now()
	if event.Status == "" {
		event.Status = domain.EventStatusPending
	}
	if event.ID == 0 {
		event.ID = d.db.GetIdGenerator(AlertEventsCollection)
	}
	_, err := d.db.Collection(AlertEventsCollection).InsertOne(ctx, event)
	return event.ID, err
}

func (d *alertDAO) UpdateEventStatus(ctx context.Context, id int64, status domain.EventStatus) error {
	update := bson.M{"$set": bson.M{"status": status}}
	if status == domain.EventStatusSent {
		now := time.Now()
		update["$set"].(bson.M)["sent_at"] = &now
	}
	_, err := d.db.Collection(AlertEventsCollection).UpdateOne(ctx, bson.M{"id": id}, update)
	return err
}

func (d *alertDAO) ListEvents(ctx context.Context, filter domain.AlertEventFilter) ([]domain.AlertEvent, int64, error) {
	query := d.buildEventQuery(filter)

	total, err := d.db.Collection(AlertEventsCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
		opts.SetSkip(filter.Offset)
	}

	cursor, err := d.db.Collection(AlertEventsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var events []domain.AlertEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

func (d *alertDAO) GetPendingEvents(ctx context.Context, limit int) ([]domain.AlertEvent, error) {
	query := bson.M{
		"status":      domain.EventStatusPending,
		"retry_count": bson.M{"$lt": 3},
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "create_time", Value: 1}}).
		SetLimit(int64(limit))

	cursor, err := d.db.Collection(AlertEventsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var events []domain.AlertEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (d *alertDAO) IncrementRetry(ctx context.Context, id int64) error {
	update := bson.M{"$inc": bson.M{"retry_count": 1}}
	_, err := d.db.Collection(AlertEventsCollection).UpdateOne(ctx, bson.M{"id": id}, update)
	return err
}

func (d *alertDAO) buildEventQuery(filter domain.AlertEventFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Type != "" {
		query["type"] = filter.Type
	}
	if filter.Severity != "" {
		query["severity"] = filter.Severity
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.RuleID > 0 {
		query["rule_id"] = filter.RuleID
	}
	return query
}

// ========== 通知渠道 ==========

func (d *alertDAO) CreateChannel(ctx context.Context, ch domain.NotificationChannel) (int64, error) {
	now := time.Now()
	ch.CreateTime = now
	ch.UpdateTime = now
	if ch.ID == 0 {
		ch.ID = d.db.GetIdGenerator(NotifyChannelsCollection)
	}
	_, err := d.db.Collection(NotifyChannelsCollection).InsertOne(ctx, ch)
	return ch.ID, err
}

func (d *alertDAO) UpdateChannel(ctx context.Context, ch domain.NotificationChannel) error {
	ch.UpdateTime = time.Now()
	filter := bson.M{"id": ch.ID}
	update := bson.M{"$set": ch}
	_, err := d.db.Collection(NotifyChannelsCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *alertDAO) GetChannelByID(ctx context.Context, id int64) (domain.NotificationChannel, error) {
	var ch domain.NotificationChannel
	err := d.db.Collection(NotifyChannelsCollection).FindOne(ctx, bson.M{"id": id}).Decode(&ch)
	return ch, err
}

func (d *alertDAO) ListChannels(ctx context.Context, filter domain.ChannelFilter) ([]domain.NotificationChannel, int64, error) {
	query := d.buildChannelQuery(filter)

	total, err := d.db.Collection(NotifyChannelsCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
		opts.SetSkip(filter.Offset)
	}

	cursor, err := d.db.Collection(NotifyChannelsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var channels []domain.NotificationChannel
	if err := cursor.All(ctx, &channels); err != nil {
		return nil, 0, err
	}
	return channels, total, nil
}

func (d *alertDAO) DeleteChannel(ctx context.Context, id int64) error {
	_, err := d.db.Collection(NotifyChannelsCollection).DeleteOne(ctx, bson.M{"id": id})
	return err
}

func (d *alertDAO) GetChannelsByIDs(ctx context.Context, ids []int64) ([]domain.NotificationChannel, error) {
	query := bson.M{"id": bson.M{"$in": ids}, "enabled": true}
	cursor, err := d.db.Collection(NotifyChannelsCollection).Find(ctx, query)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var channels []domain.NotificationChannel
	if err := cursor.All(ctx, &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

func (d *alertDAO) buildChannelQuery(filter domain.ChannelFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Type != "" {
		query["type"] = filter.Type
	}
	if filter.Enabled != nil {
		query["enabled"] = *filter.Enabled
	}
	return query
}
