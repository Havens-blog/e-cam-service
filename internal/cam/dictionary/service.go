package dictionary

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"go.mongodb.org/mongo-driver/mongo"
)

// 数据字典相关错误码
var (
	ErrDictTypeNotFound    = errs.ErrorCode{Code: 404020, Msg: "dictionary type not found"}
	ErrDictTypeCodeExists  = errs.ErrorCode{Code: 409020, Msg: "dictionary type code already exists"}
	ErrDictTypeHasItems    = errs.ErrorCode{Code: 400020, Msg: "dictionary type has items, cannot delete"}
	ErrDictItemNotFound    = errs.ErrorCode{Code: 404021, Msg: "dictionary item not found"}
	ErrDictItemValueExists = errs.ErrorCode{Code: 409021, Msg: "dictionary item value already exists"}
)

const cacheTTL = 5 * time.Minute

// DictService 业务逻辑层接口
type DictService interface {
	// 字典类型
	CreateType(ctx context.Context, tenantID string, req CreateTypeReq) (DictType, error)
	UpdateType(ctx context.Context, tenantID string, id int64, req UpdateTypeReq) error
	DeleteType(ctx context.Context, tenantID string, id int64) error
	ListTypes(ctx context.Context, tenantID string, filter TypeFilter) ([]DictType, int64, error)
	UpdateTypeStatus(ctx context.Context, tenantID string, id int64, status string) error

	// 字典项
	CreateItem(ctx context.Context, tenantID string, typeID int64, req CreateItemReq) (DictItem, error)
	UpdateItem(ctx context.Context, tenantID string, id int64, req UpdateItemReq) error
	DeleteItem(ctx context.Context, tenantID string, id int64) error
	ListItems(ctx context.Context, tenantID string, typeID int64) ([]DictItem, error)
	UpdateItemStatus(ctx context.Context, tenantID string, id int64, status string) error

	// 数据查询
	GetByCode(ctx context.Context, tenantID string, code string) ([]DictItem, error)
	BatchGetByCodes(ctx context.Context, tenantID string, codes []string) (map[string][]DictItem, error)
}

// cacheEntry 缓存条目
type cacheEntry struct {
	items    []DictItem
	expireAt time.Time
}

// dictService DictService 实现
type dictService struct {
	dao   DictDAO
	cache sync.Map
}

// NewDictService 创建 DictService 实例
func NewDictService(dao DictDAO) DictService {
	return &dictService{dao: dao}
}

// ==================== 字典类型操作 ====================

func (s *dictService) CreateType(ctx context.Context, tenantID string, req CreateTypeReq) (DictType, error) {
	// 校验 code 唯一性
	_, err := s.dao.GetTypeByCode(ctx, tenantID, req.Code)
	if err == nil {
		return DictType{}, ErrDictTypeCodeExists
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return DictType{}, err
	}

	now := time.Now().UnixMilli()
	dt := DictType{
		Code:        req.Code,
		Name:        req.Name,
		Description: req.Description,
		Status:      "enabled",
		TenantID:    tenantID,
		Ctime:       now,
		Utime:       now,
	}

	id, err := s.dao.InsertType(ctx, dt)
	if err != nil {
		return DictType{}, err
	}
	dt.ID = id
	return dt, nil
}

func (s *dictService) UpdateType(ctx context.Context, tenantID string, id int64, req UpdateTypeReq) error {
	dt := DictType{
		ID:          id,
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
	}
	return s.dao.UpdateType(ctx, dt)
}

func (s *dictService) DeleteType(ctx context.Context, tenantID string, id int64) error {
	count, err := s.dao.CountItemsByTypeID(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrDictTypeHasItems
	}
	return s.dao.DeleteType(ctx, id)
}

func (s *dictService) ListTypes(ctx context.Context, tenantID string, filter TypeFilter) ([]DictType, int64, error) {
	filter.TenantID = tenantID
	return s.dao.ListTypes(ctx, filter)
}

func (s *dictService) UpdateTypeStatus(ctx context.Context, tenantID string, id int64, status string) error {
	err := s.dao.UpdateTypeStatus(ctx, id, status)
	if err != nil {
		return err
	}
	// 失效缓存：需要查找该 type 的 code
	s.invalidateCacheByTypeID(ctx, tenantID, id)
	return nil
}

// ==================== 字典项操作 ====================

func (s *dictService) CreateItem(ctx context.Context, tenantID string, typeID int64, req CreateItemReq) (DictItem, error) {
	// 校验 value 唯一性
	_, err := s.dao.GetItemByValue(ctx, typeID, req.Value)
	if err == nil {
		return DictItem{}, ErrDictItemValueExists
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return DictItem{}, err
	}

	now := time.Now().UnixMilli()
	item := DictItem{
		DictTypeID: typeID,
		Value:      req.Value,
		Label:      req.Label,
		SortOrder:  req.SortOrder,
		Status:     "enabled",
		Extra:      req.Extra,
		Ctime:      now,
		Utime:      now,
	}

	id, err := s.dao.InsertItem(ctx, item)
	if err != nil {
		return DictItem{}, err
	}
	item.ID = id

	// 失效缓存
	s.invalidateCacheByTypeID(ctx, tenantID, typeID)
	return item, nil
}

func (s *dictService) UpdateItem(ctx context.Context, tenantID string, id int64, req UpdateItemReq) error {
	// 获取 item 以找到 typeID 用于缓存失效
	item, err := s.dao.GetItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrDictItemNotFound
		}
		return err
	}

	updated := DictItem{
		ID:        id,
		Label:     req.Label,
		SortOrder: req.SortOrder,
		Status:    req.Status,
		Extra:     req.Extra,
	}
	if err := s.dao.UpdateItem(ctx, updated); err != nil {
		return err
	}

	// 失效缓存
	s.invalidateCacheByTypeID(ctx, tenantID, item.DictTypeID)
	return nil
}

func (s *dictService) DeleteItem(ctx context.Context, tenantID string, id int64) error {
	// 获取 item 以找到 typeID 用于缓存失效
	item, err := s.dao.GetItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrDictItemNotFound
		}
		return err
	}

	if err := s.dao.DeleteItem(ctx, id); err != nil {
		return err
	}

	// 失效缓存
	s.invalidateCacheByTypeID(ctx, tenantID, item.DictTypeID)
	return nil
}

func (s *dictService) ListItems(ctx context.Context, tenantID string, typeID int64) ([]DictItem, error) {
	return s.dao.ListItemsByTypeID(ctx, typeID)
}

func (s *dictService) UpdateItemStatus(ctx context.Context, tenantID string, id int64, status string) error {
	// 获取 item 以找到 typeID 用于缓存失效
	item, err := s.dao.GetItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrDictItemNotFound
		}
		return err
	}

	if err := s.dao.UpdateItemStatus(ctx, id, status); err != nil {
		return err
	}

	// 失效缓存
	s.invalidateCacheByTypeID(ctx, tenantID, item.DictTypeID)
	return nil
}

// ==================== 数据查询 ====================

func (s *dictService) GetByCode(ctx context.Context, tenantID string, code string) ([]DictItem, error) {
	cacheKey := fmt.Sprintf("%s:%s", tenantID, code)

	// 查缓存
	if val, ok := s.cache.Load(cacheKey); ok {
		entry := val.(*cacheEntry)
		if time.Now().Before(entry.expireAt) {
			return entry.items, nil
		}
		// 过期，删除
		s.cache.Delete(cacheKey)
	}

	// 查 DictType
	dt, err := s.dao.GetTypeByCode(ctx, tenantID, code)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return []DictItem{}, nil
		}
		return nil, err
	}

	// 禁用类型返回空列表
	if dt.Status != "enabled" {
		return []DictItem{}, nil
	}

	// 查询启用状态的字典项
	items, err := s.dao.ListEnabledItemsByTypeID(ctx, dt.ID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []DictItem{}
	}

	// 按 sort_order 排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].SortOrder < items[j].SortOrder
	})

	// 写入缓存
	s.cache.Store(cacheKey, &cacheEntry{
		items:    items,
		expireAt: time.Now().Add(cacheTTL),
	})

	return items, nil
}

func (s *dictService) BatchGetByCodes(ctx context.Context, tenantID string, codes []string) (map[string][]DictItem, error) {
	result := make(map[string][]DictItem, len(codes))
	for _, code := range codes {
		items, err := s.GetByCode(ctx, tenantID, code)
		if err != nil {
			return nil, err
		}
		result[code] = items
	}
	return result, nil
}

// ==================== 缓存辅助 ====================

// invalidateCacheByTypeID 根据 typeID 查找对应的 code 并清除缓存
func (s *dictService) invalidateCacheByTypeID(ctx context.Context, tenantID string, typeID int64) {
	dt, err := s.dao.GetTypeByID(ctx, typeID)
	if err != nil {
		return
	}
	cacheKey := fmt.Sprintf("%s:%s", tenantID, dt.Code)
	s.cache.Delete(cacheKey)
}
