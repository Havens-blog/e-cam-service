package tag

import (
	"context"
	"errors"
	"strings"
)

// ==================== 标签绑定/解绑 ====================

// ValidateTagKeys 校验标签键值，拒绝空字符串和纯空白字符串
func ValidateTagKeys(tags map[string]string) error {
	for k, v := range tags {
		if strings.TrimSpace(k) == "" {
			return ErrTagKeyEmpty
		}
		if strings.TrimSpace(v) == "" {
			return ErrTagValueEmpty
		}
	}
	return nil
}

// ValidateTagKeysList 校验标签键列表
func ValidateTagKeysList(keys []string) error {
	for _, k := range keys {
		if strings.TrimSpace(k) == "" {
			return ErrTagKeyEmpty
		}
	}
	return nil
}

// BindTags 为资源绑定标签
func (s *tagService) BindTags(ctx context.Context, tenantID int64, req BindTagsReq) (*BatchResult, error) {
	if err := ValidateTagKeys(req.Tags); err != nil {
		return nil, err
	}

	total := len(req.Resources)
	result := &BatchResult{
		Total:    total,
		Failures: make([]FailureDetail, 0),
	}

	for _, res := range req.Resources {
		err := s.bindSingleResource(ctx, res, req.Tags)
		if err != nil {
			result.FailedCount++
			result.Failures = append(result.Failures, FailureDetail{
				ResourceID: res.ResourceID,
				Error:      err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	return result, nil
}

func (s *tagService) bindSingleResource(ctx context.Context, res ResourceRef, tags map[string]string) error {
	account, err := s.accountSvc.GetAccountWithCredentials(ctx, res.AccountID)
	if err != nil {
		return err
	}

	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return err
	}

	tagAdapter := adapter.Tag()
	if tagAdapter == nil {
		return errors.New("tag adapter not supported for provider: " + string(account.Provider))
	}

	return tagAdapter.TagResource(ctx, res.Region, res.ResourceType, res.ResourceID, tags)
}

// UnbindTags 解绑资源标签
func (s *tagService) UnbindTags(ctx context.Context, tenantID int64, req UnbindTagsReq) (*BatchResult, error) {
	if err := ValidateTagKeysList(req.TagKeys); err != nil {
		return nil, err
	}

	total := len(req.Resources)
	result := &BatchResult{
		Total:    total,
		Failures: make([]FailureDetail, 0),
	}

	for _, res := range req.Resources {
		err := s.unbindSingleResource(ctx, res, req.TagKeys)
		if err != nil {
			result.FailedCount++
			result.Failures = append(result.Failures, FailureDetail{
				ResourceID: res.ResourceID,
				Error:      err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	return result, nil
}

func (s *tagService) unbindSingleResource(ctx context.Context, res ResourceRef, tagKeys []string) error {
	account, err := s.accountSvc.GetAccountWithCredentials(ctx, res.AccountID)
	if err != nil {
		return err
	}

	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return err
	}

	tagAdapter := adapter.Tag()
	if tagAdapter == nil {
		return errors.New("tag adapter not supported for provider: " + string(account.Provider))
	}

	return tagAdapter.UntagResource(ctx, res.Region, res.ResourceType, res.ResourceID, tagKeys)
}
