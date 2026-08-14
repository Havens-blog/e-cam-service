package tag

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

// ==================== 标签策略 CRUD ====================

// CreatePolicy 创建标签策略
func (s *tagService) CreatePolicy(ctx context.Context, tenantID int64, req CreatePolicyReq) (TagPolicy, error) {
	if strings.TrimSpace(req.Name) == "" {
		return TagPolicy{}, ErrPolicyNameEmpty
	}
	if len(req.RequiredKeys) == 0 {
		return TagPolicy{}, ErrPolicyKeysEmpty
	}

	policy := TagPolicy{
		Name:                req.Name,
		Description:         req.Description,
		RequiredKeys:        req.RequiredKeys,
		KeyValueConstraints: req.KeyValueConstraints,
		ResourceTypes:       req.ResourceTypes,
		Status:              "enabled",
		TenantID:            tenantID,
	}

	id, err := s.dao.InsertPolicy(ctx, policy)
	if err != nil {
		return TagPolicy{}, err
	}
	policy.ID = id
	return policy, nil
}

// ListPolicies 查询标签策略列表
func (s *tagService) ListPolicies(ctx context.Context, tenantID int64, filter PolicyFilter) ([]TagPolicy, int64, error) {
	filter.TenantID = tenantID
	return s.dao.ListPolicies(ctx, filter)
}

// UpdatePolicy 更新标签策略
func (s *tagService) UpdatePolicy(ctx context.Context, tenantID int64, id int64, req UpdatePolicyReq) error {
	existing, err := s.dao.GetPolicyByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrPolicyNotFound
		}
		return err
	}

	if existing.TenantID != tenantID {
		return ErrPolicyNotFound
	}

	// Apply partial updates
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.RequiredKeys != nil {
		existing.RequiredKeys = *req.RequiredKeys
	}
	if req.KeyValueConstraints != nil {
		existing.KeyValueConstraints = *req.KeyValueConstraints
	}
	if req.ResourceTypes != nil {
		existing.ResourceTypes = *req.ResourceTypes
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}
	existing.Utime = time.Now().UnixMilli()

	return s.dao.UpdatePolicy(ctx, existing)
}

// DeletePolicy 删除标签策略
func (s *tagService) DeletePolicy(ctx context.Context, tenantID int64, id int64) error {
	existing, err := s.dao.GetPolicyByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrPolicyNotFound
		}
		return err
	}
	if existing.TenantID != tenantID {
		return ErrPolicyNotFound
	}
	return s.dao.DeletePolicy(ctx, id)
}
