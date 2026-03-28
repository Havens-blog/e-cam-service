package dictionary

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/mongo"
	"pgregory.net/rapid"
)

// inMemoryDAO is a simple in-memory DictDAO for property tests.
// It stores types and items in maps, keyed by ID.
type inMemoryDAO struct {
	mu     sync.Mutex
	types  map[int64]DictType
	items  map[int64]DictItem
	nextID int64
}

func newInMemoryDAO() *inMemoryDAO {
	return &inMemoryDAO{
		types:  make(map[int64]DictType),
		items:  make(map[int64]DictItem),
		nextID: 1,
	}
}

func (d *inMemoryDAO) nextAutoID() int64 {
	id := d.nextID
	d.nextID++
	return id
}

func (d *inMemoryDAO) InsertType(_ context.Context, dt DictType) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Check uniqueness: tenant_id + code
	for _, existing := range d.types {
		if existing.TenantID == dt.TenantID && existing.Code == dt.Code {
			return 0, errors.New("duplicate key")
		}
	}
	dt.ID = d.nextAutoID()
	d.types[dt.ID] = dt
	return dt.ID, nil
}

func (d *inMemoryDAO) UpdateType(_ context.Context, dt DictType) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	existing, ok := d.types[dt.ID]
	if !ok {
		return mongo.ErrNoDocuments
	}
	existing.Name = dt.Name
	existing.Description = dt.Description
	d.types[dt.ID] = existing
	return nil
}

func (d *inMemoryDAO) DeleteType(_ context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.types, id)
	return nil
}

func (d *inMemoryDAO) GetTypeByID(_ context.Context, id int64) (DictType, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dt, ok := d.types[id]
	if !ok {
		return DictType{}, mongo.ErrNoDocuments
	}
	return dt, nil
}

func (d *inMemoryDAO) GetTypeByCode(_ context.Context, tenantID, code string) (DictType, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, dt := range d.types {
		if dt.TenantID == tenantID && dt.Code == code {
			return dt, nil
		}
	}
	return DictType{}, mongo.ErrNoDocuments
}

func (d *inMemoryDAO) ListTypes(_ context.Context, filter TypeFilter) ([]DictType, int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result []DictType
	for _, dt := range d.types {
		if dt.TenantID != filter.TenantID {
			continue
		}
		if filter.Status != "" && dt.Status != filter.Status {
			continue
		}
		result = append(result, dt)
	}
	return result, int64(len(result)), nil
}

func (d *inMemoryDAO) UpdateTypeStatus(_ context.Context, id int64, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	dt, ok := d.types[id]
	if !ok {
		return mongo.ErrNoDocuments
	}
	dt.Status = status
	d.types[id] = dt
	return nil
}

func (d *inMemoryDAO) InsertItem(_ context.Context, item DictItem) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Check uniqueness: dict_type_id + value
	for _, existing := range d.items {
		if existing.DictTypeID == item.DictTypeID && existing.Value == item.Value {
			return 0, errors.New("duplicate key")
		}
	}
	item.ID = d.nextAutoID()
	d.items[item.ID] = item
	return item.ID, nil
}

func (d *inMemoryDAO) UpdateItem(_ context.Context, item DictItem) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	existing, ok := d.items[item.ID]
	if !ok {
		return mongo.ErrNoDocuments
	}
	existing.Label = item.Label
	existing.SortOrder = item.SortOrder
	existing.Status = item.Status
	existing.Extra = item.Extra
	d.items[item.ID] = existing
	return nil
}

func (d *inMemoryDAO) DeleteItem(_ context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.items, id)
	return nil
}

func (d *inMemoryDAO) GetItemByID(_ context.Context, id int64) (DictItem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	item, ok := d.items[id]
	if !ok {
		return DictItem{}, mongo.ErrNoDocuments
	}
	return item, nil
}

func (d *inMemoryDAO) GetItemByValue(_ context.Context, typeID int64, value string) (DictItem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, item := range d.items {
		if item.DictTypeID == typeID && item.Value == value {
			return item, nil
		}
	}
	return DictItem{}, mongo.ErrNoDocuments
}

func (d *inMemoryDAO) ListItemsByTypeID(_ context.Context, typeID int64) ([]DictItem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result []DictItem
	for _, item := range d.items {
		if item.DictTypeID == typeID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SortOrder < result[j].SortOrder })
	return result, nil
}

func (d *inMemoryDAO) ListEnabledItemsByTypeID(_ context.Context, typeID int64) ([]DictItem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result []DictItem
	for _, item := range d.items {
		if item.DictTypeID == typeID && item.Status == "enabled" {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SortOrder < result[j].SortOrder })
	return result, nil
}

func (d *inMemoryDAO) CountItemsByTypeID(_ context.Context, typeID int64) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var count int64
	for _, item := range d.items {
		if item.DictTypeID == typeID {
			count++
		}
	}
	return count, nil
}

func (d *inMemoryDAO) UpdateItemStatus(_ context.Context, id int64, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	item, ok := d.items[id]
	if !ok {
		return mongo.ErrNoDocuments
	}
	item.Status = status
	d.items[id] = item
	return nil
}

// ==================== Generators ====================

func genCode() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9_]{2,15}`)
}

func genName() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z\x{4e00}-\x{9fff}]{1,20}`)
}

func genValue() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9_]{1,15}`)
}

func genTenantID() *rapid.Generator[string] {
	return rapid.StringMatching(`tenant_[a-z]{3,8}`)
}

// ==================== Property Tests ====================

// Feature: data-dictionary, Property 1: 创建字典类型/字典项的往返一致性
// For any valid input, creating a DictType/DictItem and then querying returns the same field values.
//
// **Validates: Requirements 1.1, 1.6, 2.1, 2.6**
func TestProperty_CreationRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dao := newInMemoryDAO()
		svc := NewDictService(dao)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		// Test DictType round-trip
		code := genCode().Draw(rt, "code")
		name := genName().Draw(rt, "name")
		desc := rapid.String().Draw(rt, "desc")

		dt, err := svc.CreateType(ctx, tenantID, CreateTypeReq{
			Code:        code,
			Name:        name,
			Description: desc,
		})
		assert.NoError(rt, err)
		assert.Equal(rt, code, dt.Code)
		assert.Equal(rt, name, dt.Name)
		assert.Equal(rt, desc, dt.Description)
		assert.Equal(rt, "enabled", dt.Status)
		assert.Equal(rt, tenantID, dt.TenantID)

		// Test DictItem round-trip
		value := genValue().Draw(rt, "value")
		label := genName().Draw(rt, "label")
		sortOrder := rapid.IntRange(0, 1000).Draw(rt, "sortOrder")

		item, err := svc.CreateItem(ctx, tenantID, dt.ID, CreateItemReq{
			Value:     value,
			Label:     label,
			SortOrder: sortOrder,
		})
		assert.NoError(rt, err)
		assert.Equal(rt, value, item.Value)
		assert.Equal(rt, label, item.Label)
		assert.Equal(rt, sortOrder, item.SortOrder)
		assert.Equal(rt, "enabled", item.Status)
		assert.Equal(rt, dt.ID, item.DictTypeID)
	})
}

// Feature: data-dictionary, Property 2: 唯一性约束与明确错误码
// Duplicate code/value creation returns specific error codes (not generic DB errors).
//
// **Validates: Requirements 1.2, 2.2, 8.3**
func TestProperty_UniquenessEnforcement(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dao := newInMemoryDAO()
		svc := NewDictService(dao)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		code := genCode().Draw(rt, "code")
		name := genName().Draw(rt, "name")

		// First creation succeeds
		dt, err := svc.CreateType(ctx, tenantID, CreateTypeReq{Code: code, Name: name})
		assert.NoError(rt, err)

		// Duplicate code creation fails with specific error
		_, err = svc.CreateType(ctx, tenantID, CreateTypeReq{Code: code, Name: "other"})
		assert.ErrorIs(rt, err, ErrDictTypeCodeExists)

		// Item uniqueness
		value := genValue().Draw(rt, "value")
		_, err = svc.CreateItem(ctx, tenantID, dt.ID, CreateItemReq{Value: value, Label: "label1"})
		assert.NoError(rt, err)

		// Duplicate value creation fails with specific error
		_, err = svc.CreateItem(ctx, tenantID, dt.ID, CreateItemReq{Value: value, Label: "label2"})
		assert.ErrorIs(rt, err, ErrDictItemValueExists)
	})
}

// Feature: data-dictionary, Property 3: 不可变字段在更新后保持不变
// After update, code/value fields remain unchanged.
//
// **Validates: Requirements 1.3, 2.3**
func TestProperty_ImmutableFieldsPreserved(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dao := newInMemoryDAO()
		svc := NewDictService(dao)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		code := genCode().Draw(rt, "code")
		dt, err := svc.CreateType(ctx, tenantID, CreateTypeReq{Code: code, Name: "original"})
		assert.NoError(rt, err)

		// Update type
		newName := genName().Draw(rt, "newName")
		err = svc.UpdateType(ctx, tenantID, dt.ID, UpdateTypeReq{Name: newName, Description: "updated"})
		assert.NoError(rt, err)

		// Verify code is unchanged by querying via DAO
		updated, err := dao.GetTypeByID(ctx, dt.ID)
		assert.NoError(rt, err)
		assert.Equal(rt, code, updated.Code, "code must be immutable after update")

		// Item immutability
		value := genValue().Draw(rt, "value")
		item, err := svc.CreateItem(ctx, tenantID, dt.ID, CreateItemReq{Value: value, Label: "original"})
		assert.NoError(rt, err)

		newLabel := genName().Draw(rt, "newLabel")
		err = svc.UpdateItem(ctx, tenantID, item.ID, UpdateItemReq{Label: newLabel, SortOrder: 99, Status: "enabled"})
		assert.NoError(rt, err)

		updatedItem, err := dao.GetItemByID(ctx, item.ID)
		assert.NoError(rt, err)
		assert.Equal(rt, value, updatedItem.Value, "value must be immutable after update")
	})
}

// Feature: data-dictionary, Property 4: 含字典项的类型不可删除
// A type with items cannot be deleted; after removing all items, deletion succeeds.
//
// **Validates: Requirements 1.4**
func TestProperty_TypeWithItemsCannotBeDeleted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dao := newInMemoryDAO()
		svc := NewDictService(dao)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		code := genCode().Draw(rt, "code")
		dt, err := svc.CreateType(ctx, tenantID, CreateTypeReq{Code: code, Name: "test"})
		assert.NoError(rt, err)

		// Add at least one item
		numItems := rapid.IntRange(1, 5).Draw(rt, "numItems")
		var itemIDs []int64
		for i := 0; i < numItems; i++ {
			value := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "itemValue")
			item, err := svc.CreateItem(ctx, tenantID, dt.ID, CreateItemReq{
				Value: value + rapid.StringMatching(`[0-9]{3}`).Draw(rt, "suffix"),
				Label: "label",
			})
			if err != nil {
				continue // skip duplicates
			}
			itemIDs = append(itemIDs, item.ID)
		}

		if len(itemIDs) == 0 {
			return // no items created, skip
		}

		// Delete type should fail
		err = svc.DeleteType(ctx, tenantID, dt.ID)
		assert.ErrorIs(rt, err, ErrDictTypeHasItems)

		// Delete all items
		for _, id := range itemIDs {
			err = svc.DeleteItem(ctx, tenantID, id)
			assert.NoError(rt, err)
		}

		// Now delete type should succeed
		err = svc.DeleteType(ctx, tenantID, dt.ID)
		assert.NoError(rt, err)
	})
}

// Feature: data-dictionary, Property 5: 租户数据隔离
// Data created under tenant A is not visible under tenant B.
//
// **Validates: Requirements 6.1**
func TestProperty_TenantDataIsolation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dao := newInMemoryDAO()
		svc := NewDictService(dao)
		ctx := context.Background()

		tenantA := "tenant_aaa"
		tenantB := "tenant_bbb"
		code := genCode().Draw(rt, "code")

		// Create type under tenant A
		dt, err := svc.CreateType(ctx, tenantA, CreateTypeReq{Code: code, Name: "test"})
		assert.NoError(rt, err)

		// Create item under tenant A
		_, err = svc.CreateItem(ctx, tenantA, dt.ID, CreateItemReq{Value: "val1", Label: "label1"})
		assert.NoError(rt, err)

		// Tenant B should not see tenant A's types
		typesB, countB, err := svc.ListTypes(ctx, tenantB, TypeFilter{})
		assert.NoError(rt, err)
		assert.Equal(rt, int64(0), countB)
		assert.Empty(rt, typesB)

		// Tenant B should not see tenant A's data via GetByCode
		itemsB, err := svc.GetByCode(ctx, tenantB, code)
		assert.NoError(rt, err)
		assert.Empty(rt, itemsB)

		// Tenant A should see its own data
		typesA, countA, err := svc.ListTypes(ctx, tenantA, TypeFilter{})
		assert.NoError(rt, err)
		assert.Equal(rt, int64(1), countA)
		assert.Len(rt, typesA, 1)
	})
}

// Feature: data-dictionary, Property 6: 按 code 查询仅返回启用状态的字典项且按 sort_order 排序
// Disabled items are excluded, results are sorted by sort_order ascending, disabled type returns empty.
//
// **Validates: Requirements 3.1, 4.2, 5.2**
func TestProperty_GetByCodeEnabledAndSorted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dao := newInMemoryDAO()
		svc := NewDictService(dao)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		code := genCode().Draw(rt, "code")
		dt, err := svc.CreateType(ctx, tenantID, CreateTypeReq{Code: code, Name: "test"})
		assert.NoError(rt, err)

		// Create items with mixed statuses
		numItems := rapid.IntRange(2, 8).Draw(rt, "numItems")
		var enabledCount int
		for i := 0; i < numItems; i++ {
			sortOrder := rapid.IntRange(0, 100).Draw(rt, "sortOrder")
			item, err := svc.CreateItem(ctx, tenantID, dt.ID, CreateItemReq{
				Value:     rapid.StringMatching(`[a-z]{3}[0-9]{3}`).Draw(rt, "value"),
				Label:     "label",
				SortOrder: sortOrder,
			})
			if err != nil {
				continue // skip duplicates
			}

			// Randomly disable some items
			if rapid.Bool().Draw(rt, "disable") {
				_ = svc.UpdateItemStatus(ctx, tenantID, item.ID, "disabled")
			} else {
				enabledCount++
			}
		}

		// GetByCode should only return enabled items
		items, err := svc.GetByCode(ctx, tenantID, code)
		assert.NoError(rt, err)

		// All returned items should be enabled
		for _, item := range items {
			assert.Equal(rt, "enabled", item.Status)
		}

		// Items should be sorted by sort_order ascending
		for i := 1; i < len(items); i++ {
			assert.LessOrEqual(rt, items[i-1].SortOrder, items[i].SortOrder,
				"items should be sorted by sort_order ascending")
		}

		// Test disabled type returns empty
		err = svc.UpdateTypeStatus(ctx, tenantID, dt.ID, "disabled")
		assert.NoError(rt, err)

		items, err = svc.GetByCode(ctx, tenantID, code)
		assert.NoError(rt, err)
		assert.Empty(rt, items, "disabled type should return empty list")
	})
}
