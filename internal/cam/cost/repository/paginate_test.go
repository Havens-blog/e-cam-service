package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
)

// filterCaptureDAO 按调用次序返回预设页，并记录每次请求的 filter。
type filterCaptureDAO struct {
	BillDAO
	calls    int
	pages    [][]domain.UnifiedBill
	filters  []UnifiedBillFilter
	afterFn  func(call int) error
	fetchErr error
}

func (m *filterCaptureDAO) ListUnifiedBills(ctx context.Context, filter UnifiedBillFilter) ([]domain.UnifiedBill, error) {
	call := m.calls
	m.calls++
	m.filters = append(m.filters, filter)
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	if m.afterFn != nil {
		if err := m.afterFn(call); err != nil {
			return nil, err
		}
	}
	if call >= len(m.pages) {
		return nil, nil
	}
	return m.pages[call], nil
}

var billIDSeq int64

func makeBills(n int) []domain.UnifiedBill {
	bills := make([]domain.UnifiedBill, 0, n)
	for i := 0; i < n; i++ {
		billIDSeq++
		bills = append(bills, domain.UnifiedBill{ID: billIDSeq})
	}
	return bills
}

func assertNoDuplicates(t *testing.T, all []domain.UnifiedBill) {
	t.Helper()
	seen := make(map[int64]int, len(all))
	for _, b := range all {
		seen[b.ID]++
		if seen[b.ID] > 1 {
			t.Fatalf("bill %d 被回调 %d 次, 期望恰好 1 次", b.ID, seen[b.ID])
		}
	}
}

func TestPaginateUnifiedBills_SinglePage(t *testing.T) {
	dao := &filterCaptureDAO{pages: [][]domain.UnifiedBill{makeBills(3)}}
	var onPages int
	var all []domain.UnifiedBill

	err := PaginateUnifiedBills(context.Background(), dao, UnifiedBillFilter{}, 500,
		func(bills []domain.UnifiedBill) error {
			onPages++
			all = append(all, bills...)
			return nil
		})

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if onPages != 1 || len(all) != 3 {
		t.Fatalf("onPages=%d len(all)=%d, 期望 1 页 3 bills", onPages, len(all))
	}
	if dao.calls != 1 {
		t.Fatalf("DAO 调用 %d 次, 期望 1 次", dao.calls)
	}
}

func TestPaginateUnifiedBills_MultiPage(t *testing.T) {
	dao := &filterCaptureDAO{pages: [][]domain.UnifiedBill{
		makeBills(3),
		makeBills(3),
		makeBills(1),
	}}
	var all []domain.UnifiedBill

	err := PaginateUnifiedBills(context.Background(), dao, UnifiedBillFilter{}, 3,
		func(bills []domain.UnifiedBill) error {
			all = append(all, bills...)
			return nil
		})

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(all) != 7 {
		t.Fatalf("len(all)=%d, 期望 7 bills", len(all))
	}
	assertNoDuplicates(t, all)
	if dao.calls != 3 {
		t.Fatalf("DAO 调用 %d 次, 期望 3 次", dao.calls)
	}
}

func TestPaginateUnifiedBills_ExactDivision(t *testing.T) {
	dao := &filterCaptureDAO{pages: [][]domain.UnifiedBill{
		makeBills(3),
		makeBills(3),
	}}
	var onPages int
	var all []domain.UnifiedBill

	err := PaginateUnifiedBills(context.Background(), dao, UnifiedBillFilter{}, 3,
		func(bills []domain.UnifiedBill) error {
			onPages++
			all = append(all, bills...)
			return nil
		})

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(all) != 6 {
		t.Fatalf("len(all)=%d, 期望 6 bills", len(all))
	}
	assertNoDuplicates(t, all)
	// 恰好整除时最后整页后仍需再取一空页确认结束；空页不回调
	if dao.calls != 3 {
		t.Fatalf("DAO 调用 %d 次, 期望 3 次(含空页确认)", dao.calls)
	}
	if onPages != 2 {
		t.Fatalf("onPages=%d, 期望 2 次", onPages)
	}
}

func TestPaginateUnifiedBills_TailPageEarlyStop(t *testing.T) {
	dao := &filterCaptureDAO{pages: [][]domain.UnifiedBill{
		makeBills(2),
		makeBills(1),
	}}
	var all []domain.UnifiedBill

	err := PaginateUnifiedBills(context.Background(), dao, UnifiedBillFilter{}, 2,
		func(bills []domain.UnifiedBill) error {
			all = append(all, bills...)
			return nil
		})

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// 尾页取回 1 < 页大小 2, 早停不再取下一页
	if dao.calls != 2 {
		t.Fatalf("DAO 调用 %d 次, 期望早停于 2 次", dao.calls)
	}
	if len(all) != 3 {
		t.Fatalf("len(all)=%d, 期望 3 bills", len(all))
	}
	assertNoDuplicates(t, all)
}

func TestPaginateUnifiedBills_EmptyResult(t *testing.T) {
	dao := &filterCaptureDAO{}
	var onPages int

	err := PaginateUnifiedBills(context.Background(), dao, UnifiedBillFilter{}, 500,
		func(bills []domain.UnifiedBill) error {
			onPages++
			return nil
		})

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if onPages != 0 {
		t.Fatalf("onPages=%d, 期望空结果零回调", onPages)
	}
	if dao.calls != 1 {
		t.Fatalf("DAO 调用 %d 次, 期望 1 次", dao.calls)
	}
}

func TestPaginateUnifiedBills_CallbackErrorAborts(t *testing.T) {
	dao := &filterCaptureDAO{pages: [][]domain.UnifiedBill{
		makeBills(2),
		makeBills(2),
	}}
	cbErr := errors.New("callback failed")
	var onPages int

	err := PaginateUnifiedBills(context.Background(), dao, UnifiedBillFilter{}, 2,
		func([]domain.UnifiedBill) error {
			onPages++
			return cbErr
		})

	if !errors.Is(err, cbErr) {
		t.Fatalf("err=%v, 期望回调错误原样回传", err)
	}
	if onPages != 1 || dao.calls != 1 {
		t.Fatalf("onPages=%d dao.calls=%d, 期望回调出错后中止不再取页", onPages, dao.calls)
	}
}

func TestPaginateUnifiedBills_DAOReturnsError(t *testing.T) {
	daoErr := errors.New("mongo down")
	dao := &filterCaptureDAO{fetchErr: daoErr}

	err := PaginateUnifiedBills(context.Background(), dao, UnifiedBillFilter{}, 500,
		func([]domain.UnifiedBill) error { return nil })

	if !errors.Is(err, daoErr) {
		t.Fatalf("err=%v, 期望 DAO 错误原样回传", err)
	}
}

func TestPaginateUnifiedBills_OffsetLimitPerRequest(t *testing.T) {
	dao := &filterCaptureDAO{pages: [][]domain.UnifiedBill{
		makeBills(2),
		makeBills(2),
	}}
	base := UnifiedBillFilter{TenantID: 42, Provider: "aliyun", StartDate: "2026-09-01", EndDate: "2026-09-30"}

	err := PaginateUnifiedBills(context.Background(), dao, base, 2,
		func([]domain.UnifiedBill) error { return nil })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// 两页均满(取回 == 页大小)，第 3 次取回空页才确认结束
	if len(dao.filters) != 3 {
		t.Fatalf("捕获 %d 个 filter, 期望 3(含空页确认)", len(dao.filters))
	}
	for i, f := range dao.filters {
		wantOffset := int64(i) * 2
		if f.Offset != wantOffset {
			t.Fatalf("第 %d 页 Offset=%d, 期望 %d", i, f.Offset, wantOffset)
		}
		if f.Limit != 2 {
			t.Fatalf("第 %d 页 Limit=%d, 期望 2", i, f.Limit)
		}
		if f.TenantID != 42 || f.Provider != "aliyun" || f.StartDate != "2026-09-01" || f.EndDate != "2026-09-30" {
			t.Fatalf("第 %d 页 filter 业务条件漂移: %+v", i, f)
		}
	}
}

func TestPaginateUnifiedBills_DefaultPageSize(t *testing.T) {
	dao := &filterCaptureDAO{}
	err := PaginateUnifiedBills(context.Background(), dao, UnifiedBillFilter{}, 0,
		func([]domain.UnifiedBill) error { return nil })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(dao.filters) != 1 || dao.filters[0].Limit != DefaultUnifiedBillPageSize {
		t.Fatalf("pageSize=0 应回退默认 %d, 实际 Limit=%d", DefaultUnifiedBillPageSize, dao.filters[0].Limit)
	}
}

func ExamplePaginateUnifiedBills() {
	fmt.Println(DefaultUnifiedBillPageSize)
	// Output: 500
}
