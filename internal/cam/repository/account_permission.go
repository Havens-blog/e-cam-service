package repository

import (
	"context"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
)

// AccountPermissionRepository 云账号权限体检快照仓储
type AccountPermissionRepository interface {
	// Save 保存（按账号 upsert）
	Save(ctx context.Context, check camdomain.PermissionCheck) error
	// GetByAccountID 读取最近一次快照；未体检过返回 (nil, nil)
	GetByAccountID(ctx context.Context, accountID int64) (*camdomain.PermissionCheck, error)
}

type accountPermissionRepository struct {
	dao dao.AccountPermissionCheckDAO
}

// NewAccountPermissionRepository 创建仓储
func NewAccountPermissionRepository(d dao.AccountPermissionCheckDAO) AccountPermissionRepository {
	return &accountPermissionRepository{dao: d}
}

// Save 保存快照
func (r *accountPermissionRepository) Save(ctx context.Context, check camdomain.PermissionCheck) error {
	items := make([]dao.AccountPermissionItem, 0, len(check.Items))
	for _, it := range check.Items {
		items = append(items, dao.AccountPermissionItem{
			AssetType: it.AssetType,
			AssetName: it.AssetName,
			Status:    it.Status,
			Code:      it.Code,
			Message:   it.Message,
		})
	}

	return r.dao.Save(ctx, dao.AccountPermissionCheck{
		AccountID:        check.AccountID,
		Provider:         check.Provider,
		IdentityType:     check.IdentityType,
		IsPrimaryAccount: check.IsPrimaryAccount,
		Items:            items,
		Summary: dao.AccountPermissionSummary{
			OK:          check.Summary.OK,
			Denied:      check.Summary.Denied,
			Error:       check.Summary.Error,
			Unsupported: check.Summary.Unsupported,
		},
		CheckedAt: check.CheckedAt,
	})
}

// GetByAccountID 读取快照
func (r *accountPermissionRepository) GetByAccountID(ctx context.Context, accountID int64) (*camdomain.PermissionCheck, error) {
	doc, found, err := r.dao.GetByAccountID(ctx, accountID)
	if err != nil || !found {
		return nil, err
	}

	items := make([]camdomain.PermissionItem, 0, len(doc.Items))
	for _, it := range doc.Items {
		items = append(items, camdomain.PermissionItem{
			AssetType: it.AssetType,
			AssetName: it.AssetName,
			Status:    it.Status,
			Code:      it.Code,
			Message:   it.Message,
		})
	}

	return &camdomain.PermissionCheck{
		AccountID:        doc.AccountID,
		Provider:         doc.Provider,
		IdentityType:     doc.IdentityType,
		IsPrimaryAccount: doc.IsPrimaryAccount,
		Items:            items,
		Summary: camdomain.PermissionSummary{
			OK:          doc.Summary.OK,
			Denied:      doc.Summary.Denied,
			Error:       doc.Summary.Error,
			Unsupported: doc.Summary.Unsupported,
		},
		CheckedAt: doc.CheckedAt,
	}, nil
}
