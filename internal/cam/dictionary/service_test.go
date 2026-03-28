package dictionary

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// mockDictDAO 用于测试的 DictDAO mock
type mockDictDAO struct {
	insertTypeFn               func(ctx context.Context, dt DictType) (int64, error)
	updateTypeFn               func(ctx context.Context, dt DictType) error
	deleteTypeFn               func(ctx context.Context, id int64) error
	getTypeByIDFn              func(ctx context.Context, id int64) (DictType, error)
	getTypeByCodeFn            func(ctx context.Context, tenantID, code string) (DictType, error)
	listTypesFn                func(ctx context.Context, filter TypeFilter) ([]DictType, int64, error)
	updateTypeStatusFn         func(ctx context.Context, id int64, status string) error
	insertItemFn               func(ctx context.Context, item DictItem) (int64, error)
	updateItemFn               func(ctx context.Context, item DictItem) error
	deleteItemFn               func(ctx context.Context, id int64) error
	getItemByIDFn              func(ctx context.Context, id int64) (DictItem, error)
	getItemByValueFn           func(ctx context.Context, typeID int64, value string) (DictItem, error)
	listItemsByTypeIDFn        func(ctx context.Context, typeID int64) ([]DictItem, error)
	listEnabledItemsByTypeIDFn func(ctx context.Context, typeID int64) ([]DictItem, error)
	countItemsByTypeIDFn       func(ctx context.Context, typeID int64) (int64, error)
	updateItemStatusFn         func(ctx context.Context, id int64, status string) error
}

func (m *mockDictDAO) InsertType(ctx context.Context, dt DictType) (int64, error) {
	if m.insertTypeFn != nil {
		return m.insertTypeFn(ctx, dt)
	}
	return 1, nil
}
func (m *mockDictDAO) UpdateType(ctx context.Context, dt DictType) error {
	if m.updateTypeFn != nil {
		return m.updateTypeFn(ctx, dt)
	}
	return nil
}
func (m *mockDictDAO) DeleteType(ctx context.Context, id int64) error {
	if m.deleteTypeFn != nil {
		return m.deleteTypeFn(ctx, id)
	}
	return nil
}
func (m *mockDictDAO) GetTypeByID(ctx context.Context, id int64) (DictType, error) {
	if m.getTypeByIDFn != nil {
		return m.getTypeByIDFn(ctx, id)
	}
	return DictType{}, mongo.ErrNoDocuments
}
func (m *mockDictDAO) GetTypeByCode(ctx context.Context, tenantID, code string) (DictType, error) {
	if m.getTypeByCodeFn != nil {
		return m.getTypeByCodeFn(ctx, tenantID, code)
	}
	return DictType{}, mongo.ErrNoDocuments
}
func (m *mockDictDAO) ListTypes(ctx context.Context, filter TypeFilter) ([]DictType, int64, error) {
	if m.listTypesFn != nil {
		return m.listTypesFn(ctx, filter)
	}
	return nil, 0, nil
}
func (m *mockDictDAO) UpdateTypeStatus(ctx context.Context, id int64, status string) error {
	if m.updateTypeStatusFn != nil {
		return m.updateTypeStatusFn(ctx, id, status)
	}
	return nil
}
func (m *mockDictDAO) InsertItem(ctx context.Context, item DictItem) (int64, error) {
	if m.insertItemFn != nil {
		return m.insertItemFn(ctx, item)
	}
	return 1, nil
}
func (m *mockDictDAO) UpdateItem(ctx context.Context, item DictItem) error {
	if m.updateItemFn != nil {
		return m.updateItemFn(ctx, item)
	}
	return nil
}
func (m *mockDictDAO) DeleteItem(ctx context.Context, id int64) error {
	if m.deleteItemFn != nil {
		return m.deleteItemFn(ctx, id)
	}
	return nil
}
func (m *mockDictDAO) GetItemByID(ctx context.Context, id int64) (DictItem, error) {
	if m.getItemByIDFn != nil {
		return m.getItemByIDFn(ctx, id)
	}
	return DictItem{}, mongo.ErrNoDocuments
}
func (m *mockDictDAO) GetItemByValue(ctx context.Context, typeID int64, value string) (DictItem, error) {
	if m.getItemByValueFn != nil {
		return m.getItemByValueFn(ctx, typeID, value)
	}
	return DictItem{}, mongo.ErrNoDocuments
}
func (m *mockDictDAO) ListItemsByTypeID(ctx context.Context, typeID int64) ([]DictItem, error) {
	if m.listItemsByTypeIDFn != nil {
		return m.listItemsByTypeIDFn(ctx, typeID)
	}
	return nil, nil
}
func (m *mockDictDAO) ListEnabledItemsByTypeID(ctx context.Context, typeID int64) ([]DictItem, error) {
	if m.listEnabledItemsByTypeIDFn != nil {
		return m.listEnabledItemsByTypeIDFn(ctx, typeID)
	}
	return nil, nil
}
func (m *mockDictDAO) CountItemsByTypeID(ctx context.Context, typeID int64) (int64, error) {
	if m.countItemsByTypeIDFn != nil {
		return m.countItemsByTypeIDFn(ctx, typeID)
	}
	return 0, nil
}
func (m *mockDictDAO) UpdateItemStatus(ctx context.Context, id int64, status string) error {
	if m.updateItemStatusFn != nil {
		return m.updateItemStatusFn(ctx, id, status)
	}
	return nil
}

// ==================== 单元测试 ====================

func TestCreateType_Success(t *testing.T) {
	dao := &mockDictDAO{
		getTypeByCodeFn: func(_ context.Context, _, _ string) (DictType, error) {
			return DictType{}, mongo.ErrNoDocuments
		},
		insertTypeFn: func(_ context.Context, dt DictType) (int64, error) {
			assert.Equal(t, "enabled", dt.Status)
			assert.NotZero(t, dt.Ctime)
			assert.NotZero(t, dt.Utime)
			return 100, nil
		},
	}
	svc := NewDictService(dao)

	dt, err := svc.CreateType(context.Background(), "tenant1", CreateTypeReq{
		Code: "env_type",
		Name: "环境类型",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(100), dt.ID)
	assert.Equal(t, "env_type", dt.Code)
	assert.Equal(t, "环境类型", dt.Name)
	assert.Equal(t, "enabled", dt.Status)
	assert.Equal(t, "tenant1", dt.TenantID)
}

func TestCreateType_DuplicateCode(t *testing.T) {
	dao := &mockDictDAO{
		getTypeByCodeFn: func(_ context.Context, _, _ string) (DictType, error) {
			return DictType{ID: 1, Code: "env_type"}, nil // already exists
		},
	}
	svc := NewDictService(dao)

	_, err := svc.CreateType(context.Background(), "tenant1", CreateTypeReq{
		Code: "env_type",
		Name: "环境类型",
	})
	assert.ErrorIs(t, err, ErrDictTypeCodeExists)
}

func TestDeleteType_HasItems(t *testing.T) {
	dao := &mockDictDAO{
		countItemsByTypeIDFn: func(_ context.Context, _ int64) (int64, error) {
			return 3, nil
		},
	}
	svc := NewDictService(dao)

	err := svc.DeleteType(context.Background(), "tenant1", 1)
	assert.ErrorIs(t, err, ErrDictTypeHasItems)
}

func TestDeleteType_NoItems_Success(t *testing.T) {
	deleted := false
	dao := &mockDictDAO{
		countItemsByTypeIDFn: func(_ context.Context, _ int64) (int64, error) {
			return 0, nil
		},
		deleteTypeFn: func(_ context.Context, id int64) error {
			deleted = true
			assert.Equal(t, int64(1), id)
			return nil
		},
	}
	svc := NewDictService(dao)

	err := svc.DeleteType(context.Background(), "tenant1", 1)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestUpdateType_CodeImmutable(t *testing.T) {
	var capturedDT DictType
	dao := &mockDictDAO{
		updateTypeFn: func(_ context.Context, dt DictType) error {
			capturedDT = dt
			return nil
		},
	}
	svc := NewDictService(dao)

	err := svc.UpdateType(context.Background(), "tenant1", 1, UpdateTypeReq{
		Name:        "新名称",
		Description: "新描述",
	})
	require.NoError(t, err)
	// UpdateType only passes name and description, code is not set
	assert.Equal(t, "", capturedDT.Code)
	assert.Equal(t, "新名称", capturedDT.Name)
	assert.Equal(t, "新描述", capturedDT.Description)
}

func TestGetByCode_DisabledType(t *testing.T) {
	dao := &mockDictDAO{
		getTypeByCodeFn: func(_ context.Context, _, _ string) (DictType, error) {
			return DictType{ID: 1, Code: "env_type", Status: "disabled"}, nil
		},
	}
	svc := NewDictService(dao)

	items, err := svc.GetByCode(context.Background(), "tenant1", "env_type")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestGetByCode_CacheHit(t *testing.T) {
	queryCount := 0
	dao := &mockDictDAO{
		getTypeByCodeFn: func(_ context.Context, _, _ string) (DictType, error) {
			queryCount++
			return DictType{ID: 1, Code: "env_type", Status: "enabled"}, nil
		},
		listEnabledItemsByTypeIDFn: func(_ context.Context, _ int64) ([]DictItem, error) {
			return []DictItem{{ID: 1, Value: "prod", Label: "生产", SortOrder: 1}}, nil
		},
	}
	svc := NewDictService(dao)
	ctx := context.Background()

	// First call - cache miss
	items1, err := svc.GetByCode(ctx, "tenant1", "env_type")
	require.NoError(t, err)
	assert.Len(t, items1, 1)
	assert.Equal(t, 1, queryCount)

	// Second call - cache hit
	items2, err := svc.GetByCode(ctx, "tenant1", "env_type")
	require.NoError(t, err)
	assert.Len(t, items2, 1)
	assert.Equal(t, 1, queryCount) // no additional DB query
}

func TestGetByCode_CacheInvalidation(t *testing.T) {
	getByCodeCount := 0
	dao := &mockDictDAO{
		getTypeByCodeFn: func(_ context.Context, _, _ string) (DictType, error) {
			getByCodeCount++
			return DictType{ID: 1, Code: "env_type", Status: "enabled", TenantID: "tenant1"}, nil
		},
		listEnabledItemsByTypeIDFn: func(_ context.Context, _ int64) ([]DictItem, error) {
			return []DictItem{{ID: 1, Value: "prod", Label: "生产", SortOrder: 1}}, nil
		},
		getItemByValueFn: func(_ context.Context, _ int64, _ string) (DictItem, error) {
			return DictItem{}, mongo.ErrNoDocuments
		},
		insertItemFn: func(_ context.Context, item DictItem) (int64, error) {
			return 2, nil
		},
		getTypeByIDFn: func(_ context.Context, id int64) (DictType, error) {
			return DictType{ID: 1, Code: "env_type", TenantID: "tenant1"}, nil
		},
	}
	svc := NewDictService(dao)
	ctx := context.Background()

	// Populate cache
	_, err := svc.GetByCode(ctx, "tenant1", "env_type")
	require.NoError(t, err)
	assert.Equal(t, 1, getByCodeCount)

	// CreateItem should invalidate cache
	_, err = svc.CreateItem(ctx, "tenant1", 1, CreateItemReq{Value: "test", Label: "测试"})
	require.NoError(t, err)

	// Next GetByCode should query DB again (cache was invalidated)
	_, err = svc.GetByCode(ctx, "tenant1", "env_type")
	require.NoError(t, err)
	assert.Equal(t, 2, getByCodeCount) // 1 (first GetByCode) + 1 (second GetByCode after invalidation)
}

func TestCreateItem_DuplicateValue(t *testing.T) {
	dao := &mockDictDAO{
		getItemByValueFn: func(_ context.Context, _ int64, _ string) (DictItem, error) {
			return DictItem{ID: 1, Value: "prod"}, nil // already exists
		},
	}
	svc := NewDictService(dao)

	_, err := svc.CreateItem(context.Background(), "tenant1", 1, CreateItemReq{
		Value: "prod",
		Label: "生产",
	})
	assert.ErrorIs(t, err, ErrDictItemValueExists)
}

func TestDeleteItem_Success(t *testing.T) {
	deleted := false
	dao := &mockDictDAO{
		getItemByIDFn: func(_ context.Context, id int64) (DictItem, error) {
			return DictItem{ID: id, DictTypeID: 1, Value: "prod"}, nil
		},
		deleteItemFn: func(_ context.Context, id int64) error {
			deleted = true
			assert.Equal(t, int64(10), id)
			return nil
		},
		getTypeByIDFn: func(_ context.Context, id int64) (DictType, error) {
			return DictType{ID: 1, Code: "env_type"}, nil
		},
	}
	svc := NewDictService(dao)

	err := svc.DeleteItem(context.Background(), "tenant1", 10)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestUpdateItemStatus_InvalidatesCache(t *testing.T) {
	getByCodeCount := 0
	dao := &mockDictDAO{
		getTypeByCodeFn: func(_ context.Context, _, _ string) (DictType, error) {
			getByCodeCount++
			return DictType{ID: 1, Code: "env_type", Status: "enabled", TenantID: "tenant1"}, nil
		},
		listEnabledItemsByTypeIDFn: func(_ context.Context, _ int64) ([]DictItem, error) {
			return []DictItem{{ID: 10, Value: "prod", Label: "生产", SortOrder: 1, DictTypeID: 1}}, nil
		},
		getItemByIDFn: func(_ context.Context, id int64) (DictItem, error) {
			return DictItem{ID: id, DictTypeID: 1, Value: "prod"}, nil
		},
		updateItemStatusFn: func(_ context.Context, _ int64, _ string) error {
			return nil
		},
		getTypeByIDFn: func(_ context.Context, id int64) (DictType, error) {
			return DictType{ID: 1, Code: "env_type", TenantID: "tenant1"}, nil
		},
	}
	svc := NewDictService(dao)
	ctx := context.Background()

	// Populate cache
	_, err := svc.GetByCode(ctx, "tenant1", "env_type")
	require.NoError(t, err)
	assert.Equal(t, 1, getByCodeCount)

	// UpdateItemStatus should invalidate cache
	err = svc.UpdateItemStatus(ctx, "tenant1", 10, "disabled")
	require.NoError(t, err)

	// Next GetByCode should query DB again (cache was invalidated)
	_, err = svc.GetByCode(ctx, "tenant1", "env_type")
	require.NoError(t, err)
	assert.Equal(t, 2, getByCodeCount) // 1 (first) + 1 (second after invalidation)
}
