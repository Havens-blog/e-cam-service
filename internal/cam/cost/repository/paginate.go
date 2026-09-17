package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
)

// DefaultUnifiedBillPageSize 分页遍历统一账单的默认页大小（包级可调）。
const DefaultUnifiedBillPageSize int64 = 500

// PaginateUnifiedBillPage 单页回调：入参为本页取回的账单，返回 error 时中止遍历并回传。
type PaginateUnifiedBillPage func([]domain.UnifiedBill) error

// PaginateUnifiedBills 按页分页遍历 ListUnifiedBills，逐页回调处理。
//
// 页大小 pageSize <= 0 时使用 DefaultUnifiedBillPageSize；filter 中的业务条件
// （租户/账号/账期等）每页保持不变，Offset/Limit 由本函数控制：
// 第 n 页请求 Offset = base + n*pageSize、Limit = pageSize。
// 取回数 < 页大小即早停（尾页）；空页不触发回调。
// DAO 接口签名不动，复用既有 Offset/Limit 与 billing_date desc, ctime desc 稳定排序。
func PaginateUnifiedBills(ctx context.Context, dao BillDAO, filter UnifiedBillFilter, pageSize int64, onPage PaginateUnifiedBillPage) error {
	if pageSize <= 0 {
		pageSize = DefaultUnifiedBillPageSize
	}
	offset := filter.Offset
	for {
		pageFilter := filter
		pageFilter.Offset = offset
		pageFilter.Limit = pageSize

		bills, err := dao.ListUnifiedBills(ctx, pageFilter)
		if err != nil {
			return err
		}
		if len(bills) == 0 {
			return nil
		}
		if err := onPage(bills); err != nil {
			return err
		}
		if int64(len(bills)) < pageSize {
			return nil
		}
		offset += pageSize
	}
}
