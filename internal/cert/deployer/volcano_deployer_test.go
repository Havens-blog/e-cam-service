package deployer

import (
	"context"
	"errors"
	"fmt"

	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	volcanosdkalb "github.com/volcengine/volcengine-go-sdk/service/alb"
	volcanosdkcdn "github.com/volcengine/volcengine-go-sdk/service/cdn"
	volcanosdkcsv "github.com/volcengine/volcengine-go-sdk/service/certificateservice"
	volcanosdkclb "github.com/volcengine/volcengine-go-sdk/service/clb"
	volcanosdkwaf "github.com/volcengine/volcengine-go-sdk/service/waf"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

// ---------------------------------------------------------------------
// 测试替身：fake 火山证书库 SDK（volcanoCertLibraryAPI 窄接口）
// ---------------------------------------------------------------------

// fakeVolcanoLib mock 火山四证书库 SDK 客户端束：记录逐库调用序列与上传产物
// ID，支持按调用次数注入错误（限流/一般失败）与 per-ID 查询/删除结果。
// 证书库语义对齐各库真实 API 缺省：Get/Describe 未命中=空结果（Exists=false）；
// Delete 未命中=nil（已删除=成功）；显式注入错误经 err/errFn 覆盖。
type fakeVolcanoLib struct {
	mu sync.Mutex

	// 上传（csv ImportCertificate / cdn AddCertificate / waf UploadWafServiceCertificate / alb UploadCertificate）
	importIDs    []string // csv 逐次 InstanceId；耗尽沿用末值（缺省 inst-9001）
	importErrFn  func(call int) error
	cdnCertIDs   []string // cdn 逐次 CertId（缺省 cert-cdn-9001）
	cdnAddErrFn  func(call int) error
	wafNextID    int32 // waf 逐次 Id 自增（缺省起 31）
	wafUpErrFn   func(call int) error
	albCertIDs   []string // alb/nlb 逐次 CertificateId（缺省 cert-alb-9001）
	albUpErrFn   func(call int) error
	csvReqs      []*volcanosdkcsv.ImportCertificateInput
	cdnAddReqs   []*volcanosdkcdn.AddCertificateInput
	wafUpReqs    []*volcanosdkwaf.UploadWafServiceCertificateInput
	albUpReqs    []*volcanosdkalb.UploadCertificateInput
	lastChainPEM string // csv 最近一次上传的证书链（私钥不记录——Hard Rule）
	lastKeySeen  string // 最近一次上传收到的私钥明文（仅内存，断言传递透明性后由 GC 回收）

	// 查询（csv CertificateGetInstance / cdn ListCertInfo / waf ListWafServiceCertificate / alb DescribeCertificates）
	csvGetReqs      []*volcanosdkcsv.CertificateGetInstanceInput
	csvGet          map[string]fakeVolcanoCSVInstance // instanceID → 查询结果（未命中=not found 错误）
	getErr          map[string]error                  // instanceID → 显式注入查询错误（非 not-found 语义）
	cdnListReqs     []*volcanosdkcdn.ListCertInfoInput
	cdnList         map[string][]*volcanosdkcdn.CertInfoForListCertInfoOutput // certID → 命中条目
	wafList         []*volcanosdkwaf.DataForListWafServiceCertificateOutput
	albDescribeReqs []*volcanosdkalb.DescribeCertificatesInput
	albDescribe     map[string][]*volcanosdkalb.CertificateForDescribeCertificatesOutput // certID → 命中条目

	// 删除（csv CertificateDeleteInstance / cdn DeleteCdnCertificate / waf DeleteWafServiceCertificate / alb DeleteCertificate）
	deletes []string // "product:rawID" 逐次
	delErr  map[string]error

	// 绑定（任务 2：cdn BatchDeployCert / waf UpdateDomain / alb ModifyListenerAttributes / nlb ModifyNLBListenerAttributes）
	cdnBindReqs    []*volcanosdkcdn.BatchDeployCertInput
	cdnBindErrFn   func(call int) error
	cdnBindResult  []*volcanosdkcdn.DeployResultForBatchDeployCertOutput // 逐域部署结果（缺省全 success）
	wafUpdateReqs  []*volcanosdkwaf.UpdateDomainInput
	wafUpdateErrFn func(call int) error
	albModifyReqs  []*volcanosdkalb.ModifyListenerAttributesInput
	albModifyErrFn func(call int) error
	nlbModifyReqs  []*volcanosdkclb.ModifyNLBListenerAttributesInput
	nlbModifyErrFn func(call int) error

	// 引用枚举（任务 2 ListReferences 活体重查；fake 按入参分页切片——翻页分支可测）
	cdnCertInfoList []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput      // ListCdnCertInfo 全量
	wafDomains      map[string][]*volcanosdkwaf.DataForListDomainOutput    // region → ListDomain 全量
	wafDomainErr    error                                                  // ListDomain 显式错误
	albListeners    []*volcanosdkalb.ListenerForDescribeListenersOutput    // DescribeListeners 全量
	albRuleReqs     []string                                               // DescribeRules 逐次 listenerId
	albRules        map[string][]*volcanosdkalb.RuleForDescribeRulesOutput // listenerID → 规则
	albRulesErr     error                                                  // DescribeRules 显式错误（served 置空不阻塞）
	nlbListeners    []*volcanosdkclb.ListenerForDescribeNLBListenersOutput // DescribeNLBListeners 全量
	cdnBindCalls    int
	wafListCalls    int
}

type fakeVolcanoCSVInstance struct {
	status   string
	revoked  bool
	chain    []string // 证书链 PEM 列表（叶在前）
	chainErr bool     // chain 缺失/不可解析
}

func (f *fakeVolcanoLib) ImportCertificateWithContext(_ context.Context, input *volcanosdkcsv.ImportCertificateInput, _ ...request.Option) (*volcanosdkcsv.ImportCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.csvReqs) + 1
	f.csvReqs = append(f.csvReqs, input)
	if input.CertificateInfo != nil {
		f.lastChainPEM = *input.CertificateInfo.CertificateChain
		f.lastKeySeen = *input.CertificateInfo.PrivateKey
	}
	if f.importErrFn != nil {
		if err := f.importErrFn(n); err != nil {
			return nil, err
		}
	}
	id := "inst-9001"
	if len(f.importIDs) >= n && f.importIDs[n-1] != "" {
		id = f.importIDs[n-1]
	} else if len(f.importIDs) > 0 {
		id = f.importIDs[len(f.importIDs)-1]
	}
	return &volcanosdkcsv.ImportCertificateOutput{InstanceId: strPtr(id)}, nil
}

func (f *fakeVolcanoLib) CertificateGetInstanceWithContext(_ context.Context, input *volcanosdkcsv.CertificateGetInstanceInput, _ ...request.Option) (*volcanosdkcsv.CertificateGetInstanceOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.csvGetReqs = append(f.csvGetReqs, input)
	id := *input.InstanceId
	if err := f.getErr[id]; err != nil {
		return nil, err
	}
	inst, ok := f.csvGet[id]
	if !ok {
		return nil, errors.New("Volcengine: instance " + id + " not found")
	}
	out := &volcanosdkcsv.CertificateGetInstanceOutput{
		Status:               strPtr(inst.status),
		IsCertificateRevoked: boolPtr(inst.revoked),
	}
	if !inst.chainErr {
		detail := &volcanosdkcsv.CertificateDetailForCertificateGetInstanceOutput{}
		for _, pemStr := range inst.chain {
			s := pemStr
			detail.Chain = append(detail.Chain, &s)
		}
		out.CertificateDetail = detail
	}
	return out, nil
}

func (f *fakeVolcanoLib) CertificateDeleteInstanceWithContext(_ context.Context, input *volcanosdkcsv.CertificateDeleteInstanceInput, _ ...request.Option) (*volcanosdkcsv.CertificateDeleteInstanceOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, volcanoProductCSV+":"+*input.InstanceId)
	if err := f.delErr[volcanoProductCSV+":"+*input.InstanceId]; err != nil {
		return nil, err
	}
	return &volcanosdkcsv.CertificateDeleteInstanceOutput{}, nil
}

func (f *fakeVolcanoLib) AddCertificateWithContext(_ context.Context, input *volcanosdkcdn.AddCertificateInput, _ ...request.Option) (*volcanosdkcdn.AddCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.cdnAddReqs) + 1
	f.cdnAddReqs = append(f.cdnAddReqs, input)
	f.lastChainPEM = *input.Certificate
	f.lastKeySeen = *input.PrivateKey
	if f.cdnAddErrFn != nil {
		if err := f.cdnAddErrFn(n); err != nil {
			return nil, err
		}
	}
	id := "cert-cdn-9001"
	if len(f.cdnCertIDs) >= n && f.cdnCertIDs[n-1] != "" {
		id = f.cdnCertIDs[n-1]
	} else if len(f.cdnCertIDs) > 0 {
		id = f.cdnCertIDs[len(f.cdnCertIDs)-1]
	}
	return &volcanosdkcdn.AddCertificateOutput{CertId: strPtr(id)}, nil
}

func (f *fakeVolcanoLib) ListCertInfoWithContext(_ context.Context, input *volcanosdkcdn.ListCertInfoInput, _ ...request.Option) (*volcanosdkcdn.ListCertInfoOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cdnListReqs = append(f.cdnListReqs, input)
	items := f.cdnList[*input.CertId]
	return &volcanosdkcdn.ListCertInfoOutput{CertInfo: items}, nil
}

func (f *fakeVolcanoLib) DeleteCdnCertificateWithContext(_ context.Context, input *volcanosdkcdn.DeleteCdnCertificateInput, _ ...request.Option) (*volcanosdkcdn.DeleteCdnCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, volcanoProductCDN+":"+*input.CertId)
	if err := f.delErr[volcanoProductCDN+":"+*input.CertId]; err != nil {
		return nil, err
	}
	return &volcanosdkcdn.DeleteCdnCertificateOutput{}, nil
}

func (f *fakeVolcanoLib) UploadWafServiceCertificateWithContext(_ context.Context, input *volcanosdkwaf.UploadWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.UploadWafServiceCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.wafUpReqs) + 1
	f.wafUpReqs = append(f.wafUpReqs, input)
	f.lastChainPEM = *input.PublicKey
	f.lastKeySeen = *input.PrivateKey
	if f.wafUpErrFn != nil {
		if err := f.wafUpErrFn(n); err != nil {
			return nil, err
		}
	}
	if f.wafNextID == 0 {
		f.wafNextID = 31 // 缺省起始 Id（自增语义与云侧一致）
	}
	f.wafNextID++
	return &volcanosdkwaf.UploadWafServiceCertificateOutput{Id: int32Ptr(f.wafNextID - 1)}, nil
}

func (f *fakeVolcanoLib) ListWafServiceCertificateWithContext(_ context.Context, _ *volcanosdkwaf.ListWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.ListWafServiceCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &volcanosdkwaf.ListWafServiceCertificateOutput{Data: f.wafList}, nil
}

func (f *fakeVolcanoLib) DeleteWafServiceCertificateWithContext(_ context.Context, input *volcanosdkwaf.DeleteWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.DeleteWafServiceCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, volcanoProductWAF+":"+*input.Id)
	if err := f.delErr[volcanoProductWAF+":"+*input.Id]; err != nil {
		return nil, err
	}
	return &volcanosdkwaf.DeleteWafServiceCertificateOutput{}, nil
}

func (f *fakeVolcanoLib) UploadCertificateWithContext(_ context.Context, input *volcanosdkalb.UploadCertificateInput, _ ...request.Option) (*volcanosdkalb.UploadCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.albUpReqs) + 1
	f.albUpReqs = append(f.albUpReqs, input)
	f.lastChainPEM = *input.PublicKey
	f.lastKeySeen = *input.PrivateKey
	if f.albUpErrFn != nil {
		if err := f.albUpErrFn(n); err != nil {
			return nil, err
		}
	}
	id := "cert-alb-9001"
	if len(f.albCertIDs) >= n && f.albCertIDs[n-1] != "" {
		id = f.albCertIDs[n-1]
	} else if len(f.albCertIDs) > 0 {
		id = f.albCertIDs[len(f.albCertIDs)-1]
	}
	return &volcanosdkalb.UploadCertificateOutput{CertificateId: strPtr(id)}, nil
}

func (f *fakeVolcanoLib) DescribeCertificatesWithContext(_ context.Context, input *volcanosdkalb.DescribeCertificatesInput, _ ...request.Option) (*volcanosdkalb.DescribeCertificatesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.albDescribeReqs = append(f.albDescribeReqs, input)
	var items []*volcanosdkalb.CertificateForDescribeCertificatesOutput
	for _, idPtr := range input.CertificateIds {
		items = append(items, f.albDescribe[*idPtr]...)
	}
	return &volcanosdkalb.DescribeCertificatesOutput{Certificates: items}, nil
}

func (f *fakeVolcanoLib) DeleteCertificateWithContext(_ context.Context, input *volcanosdkalb.DeleteCertificateInput, _ ...request.Option) (*volcanosdkalb.DeleteCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// alb 与 nlb 共用 alb 证书库删除 API（调用方经归一前缀路由至此）——记录
	// alb 口径前缀，错误注入键同时认 alb:/nlb: 两形态。
	f.deletes = append(f.deletes, volcanoProductALB+":"+*input.CertificateId)
	if err := f.delErr[volcanoProductALB+":"+*input.CertificateId]; err != nil {
		return nil, err
	}
	if err := f.delErr[volcanoProductNLB+":"+*input.CertificateId]; err != nil {
		return nil, err
	}
	return &volcanosdkalb.DeleteCertificateOutput{}, nil
}

// ---------------------------------------------------------------------
// 测试替身：fake 火山 SDK 绑定/引用枚举方法（任务 2 绑定层）
// ---------------------------------------------------------------------

// BatchDeployCertWithContext 记录请求并返回逐域部署结果（cdnBindResult 缺省 nil
// =云侧整体成功；注入条目按 Status 判定逐域成败）。
func (f *fakeVolcanoLib) BatchDeployCertWithContext(_ context.Context, input *volcanosdkcdn.BatchDeployCertInput, _ ...request.Option) (*volcanosdkcdn.BatchDeployCertOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cdnBindCalls++
	f.cdnBindReqs = append(f.cdnBindReqs, input)
	if f.cdnBindErrFn != nil {
		if err := f.cdnBindErrFn(f.cdnBindCalls); err != nil {
			return nil, err
		}
	}
	return &volcanosdkcdn.BatchDeployCertOutput{DeployResult: f.cdnBindResult}, nil
}

// ListDomainWithContext 按 region 取全量并按 Page/PageSize 切片（翻页分支可测）。
func (f *fakeVolcanoLib) ListDomainWithContext(_ context.Context, input *volcanosdkwaf.ListDomainInput, _ ...request.Option) (*volcanosdkwaf.ListDomainOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wafListCalls++
	if f.wafDomainErr != nil {
		return nil, f.wafDomainErr
	}
	items := f.wafDomains[volcengine.StringValue(input.Region)]
	pageSize := int64(100)
	if input.PageSize != nil {
		pageSize = int64(*input.PageSize)
	}
	pageNum := int64(1)
	if input.Page != nil {
		pageNum = int64(*input.Page)
	}
	start := (pageNum - 1) * pageSize
	if start >= int64(len(items)) {
		return &volcanosdkwaf.ListDomainOutput{}, nil
	}
	end := start + pageSize
	if end > int64(len(items)) {
		end = int64(len(items))
	}
	return &volcanosdkwaf.ListDomainOutput{Data: items[start:end]}, nil
}

func (f *fakeVolcanoLib) UpdateDomainWithContext(_ context.Context, input *volcanosdkwaf.UpdateDomainInput, _ ...request.Option) (*volcanosdkwaf.UpdateDomainOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wafUpdateReqs = append(f.wafUpdateReqs, input)
	if f.wafUpdateErrFn != nil {
		if err := f.wafUpdateErrFn(len(f.wafUpdateReqs)); err != nil {
			return nil, err
		}
	}
	return &volcanosdkwaf.UpdateDomainOutput{}, nil
}

// DescribeListenersWithContext 双形态：ListenerIds 过滤（绑定定点定位，忽略
// 分页）与全量分页（引用枚举，PageNumber/PageSize 切片）。
func (f *fakeVolcanoLib) DescribeListenersWithContext(_ context.Context, input *volcanosdkalb.DescribeListenersInput, _ ...request.Option) (*volcanosdkalb.DescribeListenersOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if input.ListenerIds != nil {
		wantListener := volcengine.StringValue(input.ListenerIds[0])
		wantLB := volcengine.StringValue(input.LoadBalancerId)
		for _, l := range f.albListeners {
			if l == nil || volcengine.StringValue(l.ListenerId) != wantListener {
				continue
			}
			if wantLB != "" && volcengine.StringValue(l.LoadBalancerId) != wantLB {
				continue
			}
			return &volcanosdkalb.DescribeListenersOutput{Listeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{l}}, nil
		}
		return &volcanosdkalb.DescribeListenersOutput{}, nil
	}
	pageSize := int64(100)
	if input.PageSize != nil {
		pageSize = *input.PageSize
	}
	pageNum := int64(1)
	if input.PageNumber != nil {
		pageNum = *input.PageNumber
	}
	start := (pageNum - 1) * pageSize
	if start >= int64(len(f.albListeners)) {
		return &volcanosdkalb.DescribeListenersOutput{}, nil
	}
	end := start + pageSize
	if end > int64(len(f.albListeners)) {
		end = int64(len(f.albListeners))
	}
	return &volcanosdkalb.DescribeListenersOutput{Listeners: f.albListeners[start:end]}, nil
}

func (f *fakeVolcanoLib) DescribeRulesWithContext(_ context.Context, input *volcanosdkalb.DescribeRulesInput, _ ...request.Option) (*volcanosdkalb.DescribeRulesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	listenerID := volcengine.StringValue(input.ListenerId)
	f.albRuleReqs = append(f.albRuleReqs, listenerID)
	if f.albRulesErr != nil {
		return nil, f.albRulesErr
	}
	return &volcanosdkalb.DescribeRulesOutput{Rules: f.albRules[listenerID]}, nil
}

func (f *fakeVolcanoLib) ModifyListenerAttributesWithContext(_ context.Context, input *volcanosdkalb.ModifyListenerAttributesInput, _ ...request.Option) (*volcanosdkalb.ModifyListenerAttributesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.albModifyReqs = append(f.albModifyReqs, input)
	if f.albModifyErrFn != nil {
		if err := f.albModifyErrFn(len(f.albModifyReqs)); err != nil {
			return nil, err
		}
	}
	return &volcanosdkalb.ModifyListenerAttributesOutput{}, nil
}

// DescribeNLBListenersWithContext 双形态：ListenerIds 过滤（绑定定点定位）与
// NextToken 偏移分页（token 即列表偏移量，翻页分支可测）。
func (f *fakeVolcanoLib) DescribeNLBListenersWithContext(_ context.Context, input *volcanosdkclb.DescribeNLBListenersInput, _ ...request.Option) (*volcanosdkclb.DescribeNLBListenersOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if input.ListenerIds != nil {
		wantListener := volcengine.StringValue(input.ListenerIds[0])
		wantLB := volcengine.StringValue(input.LoadBalancerId)
		for _, l := range f.nlbListeners {
			if l == nil || volcengine.StringValue(l.ListenerId) != wantListener {
				continue
			}
			if wantLB != "" && volcengine.StringValue(l.LoadBalancerId) != wantLB {
				continue
			}
			return &volcanosdkclb.DescribeNLBListenersOutput{Listeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{l}}, nil
		}
		return &volcanosdkclb.DescribeNLBListenersOutput{}, nil
	}
	offset := int64(0)
	if input.NextToken != nil {
		offset, _ = strconv.ParseInt(*input.NextToken, 10, 64)
	}
	maxResults := int64(100)
	if input.MaxResults != nil {
		maxResults = *input.MaxResults
	}
	end := offset + maxResults
	if end > int64(len(f.nlbListeners)) {
		end = int64(len(f.nlbListeners))
	}
	out := &volcanosdkclb.DescribeNLBListenersOutput{Listeners: f.nlbListeners[offset:end]}
	if end < int64(len(f.nlbListeners)) {
		out.NextToken = strPtr(strconv.FormatInt(end, 10))
	}
	return out, nil
}

func (f *fakeVolcanoLib) ModifyNLBListenerAttributesWithContext(_ context.Context, input *volcanosdkclb.ModifyNLBListenerAttributesInput, _ ...request.Option) (*volcanosdkclb.ModifyNLBListenerAttributesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nlbModifyReqs = append(f.nlbModifyReqs, input)
	if f.nlbModifyErrFn != nil {
		if err := f.nlbModifyErrFn(len(f.nlbModifyReqs)); err != nil {
			return nil, err
		}
	}
	return &volcanosdkclb.ModifyNLBListenerAttributesOutput{}, nil
}

// ListCdnCertInfoWithContext 按 PageNum/PageSize 切片（引用枚举翻页分支可测）。
func (f *fakeVolcanoLib) ListCdnCertInfoWithContext(_ context.Context, input *volcanosdkcdn.ListCdnCertInfoInput, _ ...request.Option) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pageSize := int64(100)
	if input.PageSize != nil {
		pageSize = *input.PageSize
	}
	pageNum := int64(1)
	if input.PageNum != nil {
		pageNum = *input.PageNum
	}
	start := (pageNum - 1) * pageSize
	if start >= int64(len(f.cdnCertInfoList)) {
		return &volcanosdkcdn.ListCdnCertInfoOutput{}, nil
	}
	end := start + pageSize
	if end > int64(len(f.cdnCertInfoList)) {
		end = int64(len(f.cdnCertInfoList))
	}
	return &volcanosdkcdn.ListCdnCertInfoOutput{CertInfo: f.cdnCertInfoList[start:end]}, nil
}

// ---------------------------------------------------------------------
// 测试装配
// ---------------------------------------------------------------------

// newTestVolcanoDeployer 装配被测部署器：fake 证书库 SDK + 确定性时间/随机后缀
// + 即时睡眠记录器（默认/地域级客户端工厂同 fake——绑定定位与引用枚举的地域
// 遍历同样落在 fake 上）。
func newTestVolcanoDeployer(fake *fakeVolcanoLib) (*VolcanoDeployer, *sleepRecorder) {
	d := NewVolcanoDeployer(nil)
	rec := &sleepRecorder{}
	d.sleep = rec.sleep
	d.now = func() time.Time { return time.Unix(1765432100, 0) }
	counter := 0
	d.randHex = func(int) string { counter++; return fmt.Sprintf("%04x", counter) }
	d.newClients = func(*sharedomain.CloudAccount) (volcanoCertLibraryAPI, error) {
		return fake, nil
	}
	d.newClientsForRegion = func(*sharedomain.CloudAccount, string) (volcanoCertLibraryAPI, error) {
		return fake, nil
	}
	return d, rec
}

func testVolcanoCreds() Credential {
	return Credential{
		Kind: CredentialKindCloudAK, Cloud: "volcano", AccountKey: "acc-main",
		AccessKey: "VOLC-test-ak", Secret: []byte("test-sk-plaintext"), KeyVersion: 1,
	}
}

// uploadCallsSnapshot 各库上传调用总数（csv+cdn+waf+alb）。
func (f *fakeVolcanoLib) uploadCallTotal() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.csvReqs) + len(f.cdnAddReqs) + len(f.wafUpReqs) + len(f.albUpReqs)
}

func (f *fakeVolcanoLib) deletesSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletes...)
}

// ---------------------------------------------------------------------
// AC-1：UploadCert（certificateservice 统一证书库第一段 + {product}:{id} 归一）
// ---------------------------------------------------------------------

// 第一段统一以 csv（certificateservice）口径上传：ImportCertificate 可重复导入
// （C7 重试即新副本），返回 {csv}:{instanceId} 归一云证书 ID；上传名符合 C7 公式。
func TestVolcanoDeployerUploadCSVDefault(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{}
	d, _ := newTestVolcanoDeployer(fake)

	id, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "csv:inst-9001", id, "第一段统一落 certificateservice，ID 归一 csv: 前缀")

	assert.Len(t, fake.csvReqs, 1)
	req := fake.csvReqs[0]
	assert.True(t, req.Repeatable != nil && *req.Repeatable, "可重复导入（重试即新副本）")
	assert.Equal(t, testCertPEM, *req.CertificateInfo.CertificateChain)
	assert.Equal(t, testKeyPEM, *req.CertificateInfo.PrivateKey, "私钥明文仅内存传递")
}

// 同材料重试/重复上传逐次新副本：ID 逐次不同、上传名互不相同（C7）。
func TestVolcanoDeployerUploadProducesFreshInstances(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{importIDs: []string{"inst-1", "inst-2"}}
	d, _ := newTestVolcanoDeployer(fake)

	id1, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	id2, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "csv:inst-1", id1)
	assert.Equal(t, "csv:inst-2", id2)
	assert.Len(t, fake.csvReqs, 2)
}

// 材料缺失/凭证归属云不符：显式拒绝且不触达云侧。
func TestVolcanoDeployerUploadRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{}
	d, _ := newTestVolcanoDeployer(fake)

	_, err := d.UploadCert(ctx, testVolcanoCreds(), "", []byte(testKeyPEM))
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	_, err = d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, nil)
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")

	foreign := testVolcanoCreds()
	foreign.Cloud = "aliyun"
	_, err = d.UploadCert(ctx, foreign, testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "not volcano")

	invalid := testVolcanoCreds()
	invalid.AccessKey = ""
	_, err = d.UploadCert(ctx, invalid, testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	assert.Zero(t, fake.uploadCallTotal(), "校验失败不产生云侧调用")
}

// 私钥卫生：云侧失败错误信息不携带私钥明文（Hard Rule：私钥不进日志/错误）。
func TestVolcanoDeployerUploadErrorLeaksNoKey(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{importErrFn: func(int) error { return errors.New("import denied") }}
	d, _ := newTestVolcanoDeployer(fake)

	_, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), testKeyPEM)
	assert.NotContains(t, err.Error(), "test-sk-plaintext", "凭证明文同样不入错误")
}

// 限流退避（boundedRetry 共享口径）：固定序列退避后恢复，耗尽透传哨兵。
func TestVolcanoDeployerUploadRateLimitedBackoff(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{importErrFn: func(call int) error {
		if call <= 2 {
			return fmt.Errorf("volcano api throttled: %w", cloudx.ErrCloudRateLimited)
		}
		return nil
	}}
	d, rec := newTestVolcanoDeployer(fake)

	id, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "csv:inst-9001", id)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot())

	exhaust := &fakeVolcanoLib{importErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d2, _ := newTestVolcanoDeployer(exhaust)
	_, err = d2.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "耗尽后哨兵语义保留")
	assert.Len(t, exhaust.csvReqs, 5, "默认 MaxAttempts=5，绝不无限重试")
}

// ---------------------------------------------------------------------
// AC-1：uploadForProduct 五分支 + {product}:{id} 多产品 ID 归一断言
// ---------------------------------------------------------------------

// 五分支上传产物 ID 逐产品归一：cdn/waf/alb/nlb/csv 各落对应证书库。
func TestVolcanoDeployerUploadForProductIDNormalization(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{
		importIDs:  []string{"inst-x"},
		cdnCertIDs: []string{"cert-cdn-x"},
		albCertIDs: []string{"cert-alb-x", "cert-nlb-x"},
	}
	d, _ := newTestVolcanoDeployer(fake)
	acct := &sharedomain.CloudAccount{Name: "acc-main", Provider: sharedomain.CloudProviderVolcano}

	cases := []struct {
		product string
		want    string
	}{
		{volcanoProductCSV, "csv:inst-x"},
		{volcanoProductCDN, "cdn:cert-cdn-x"},
		{volcanoProductWAF, "waf:31"},
		{volcanoProductALB, "alb:cert-alb-x"},
		{volcanoProductNLB, "nlb:cert-nlb-x"},
	}
	for _, tc := range cases {
		got, err := d.uploadForProduct(ctx, acct, tc.product, "ecam-name", testCertPEM, testKeyPEM)
		assert.NoError(t, err, tc.product)
		assert.Equal(t, tc.want, got, "产品 %s 云证书 ID 归一 {product}:{id}", tc.product)
	}
	assert.Len(t, fake.csvReqs, 1)
	assert.Len(t, fake.cdnAddReqs, 1)
	assert.Len(t, fake.wafUpReqs, 1)
	assert.Len(t, fake.albUpReqs, 2, "alb/nlb 共用 ALB 监听证书库上传 API")
}

// 未支持产品：哨兵错误。
func TestVolcanoDeployerUploadForProductUnsupported(t *testing.T) {
	d, _ := newTestVolcanoDeployer(&fakeVolcanoLib{})
	_, err := d.uploadForProduct(context.Background(), &sharedomain.CloudAccount{},
		"clb", "ecam-name", testCertPEM, testKeyPEM)
	assert.ErrorIs(t, err, ErrVolcanoProductNotSupported)
}

// ---------------------------------------------------------------------
// AC-2：GetCert（csv 链解析三要素 + 产品库在库状态路由）
// ---------------------------------------------------------------------

// csv 分支：在库=链解析指纹（SHA256 对齐台账口径）+ 有效期；已删除=Exists=false
// 非错误；revoked=Exists=false（不可作回滚目标）；链缺失=存在但指纹无法复核。
func TestVolcanoDeployerGetCertCSVBranch(t *testing.T) {
	bundle := certtest.NewBundle(t, "www.example.com", nil, nil)
	notAfter := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)
	fake := &fakeVolcanoLib{
		csvGet: map[string]fakeVolcanoCSVInstance{
			"inst-live":    {status: "Issued", chain: []string{string(bundle.CertPEM)}},
			"inst-revoked": {status: "Revoked", revoked: true, chain: []string{string(bundle.CertPEM)}},
			"inst-nochain": {status: "Issued", chainErr: true},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)
	ctx := context.Background()

	info, err := d.GetCert(ctx, testVolcanoCreds(), "csv:inst-live")
	assert.NoError(t, err)
	assert.True(t, info.Exists, "回滚目标有效性校验：在库存在性")
	assert.Equal(t, bundle.Fingerprint, info.Fingerprint, "SHA256(leaf.Raw) 对齐台账口径")
	assert.WithinDuration(t, notAfter.Add(-90*24*time.Hour).Add(8760*time.Hour), info.NotAfter, time.Minute,
		"有效期取叶证书 NotAfter")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "csv:inst-ghost")
	assert.NoError(t, err, "云侧已删除=Exists=false 非错误（幂等口径）")
	assert.False(t, info.Exists)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "csv:inst-revoked")
	assert.NoError(t, err)
	assert.False(t, info.Exists, "revoked 实例不构成有效回滚目标")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "csv:inst-nochain")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Empty(t, info.Fingerprint, "链缺失 → 指纹无法复核（上层 fail-safe）")
}

// 产品库分支：cdn（指纹 Sha256 直读 + 有效期 unix 秒）/ waf（服务证书列表反查）/
// alb、nlb（DescribeCertificates 反查）；未命中=Exists=false 非错误。
func TestVolcanoDeployerGetCertProductBranches(t *testing.T) {
	notAfter := time.Unix(1790000000, 0)
	fake := &fakeVolcanoLib{
		cdnList: map[string][]*volcanosdkcdn.CertInfoForListCertInfoOutput{
			"cert-cdn-1": {{CertId: strPtr("cert-cdn-1"), ExpireTime: int64Ptr(1790000000),
				CertFingerprint: &volcanosdkcdn.CertFingerprintForListCertInfoOutput{Sha256: strPtr(strings.Repeat("a", 64))}},
			},
			"cert-cdn-sha1": {{CertId: strPtr("cert-cdn-sha1"), ExpireTime: int64Ptr(1790000000),
				CertFingerprint: &volcanosdkcdn.CertFingerprintForListCertInfoOutput{Sha1: strPtr(strings.Repeat("b", 40))}},
			},
		},
		wafList: []*volcanosdkwaf.DataForListWafServiceCertificateOutput{
			{Id: int32Ptr(31), ExpireTime: strPtr("2026-09-01 00:00:00")},
		},
		albDescribe: map[string][]*volcanosdkalb.CertificateForDescribeCertificatesOutput{
			"cert-alb-1": {{CertificateId: strPtr("cert-alb-1"), ExpiredAt: strPtr("2026-09-01T00:00:00Z")}},
			"cert-nlb-1": {{CertificateId: strPtr("cert-nlb-1"), ExpiredAt: strPtr("2026-09-01T00:00:00Z")}},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)
	ctx := context.Background()

	info, err := d.GetCert(ctx, testVolcanoCreds(), "cdn:cert-cdn-1")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, strings.Repeat("a", 64), info.Fingerprint, "CDN 库原生 Sha256 指纹对齐口径直读")
	assert.Equal(t, notAfter, info.NotAfter)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "cdn:cert-cdn-sha1")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Empty(t, info.Fingerprint, "Sha1 形态指纹不通过对齐校验 → 无法复核")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "cdn:cert-cdn-ghost")
	assert.NoError(t, err)
	assert.False(t, info.Exists)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "waf:31")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), info.NotAfter, "WAF 服务证书有效期解析")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "waf:99")
	assert.NoError(t, err)
	assert.False(t, info.Exists)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "alb:cert-alb-1")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), info.NotAfter)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "nlb:cert-nlb-1")
	assert.NoError(t, err)
	assert.True(t, info.Exists, "nlb 与 alb 共用监听证书库查询路由")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "alb:cert-alb-ghost")
	assert.NoError(t, err)
	assert.False(t, info.Exists)
}

// 非归一 ID（无前缀/未知前缀）：显式拒绝（任务 2 绑定引用与回滚旧 ID 均为
// {product}:{id} 归一形态，解析失败 fail-fast 不猜测）。
// 元数据残缺容忍：云侧条目缺有效期/指纹字段 → Exists=true 但要素留空
// （上层按「无法复核」fail-safe 处理，不因元数据缺失误判不存在）。
func TestVolcanoDeployerGetCertToleratesPartialMetadata(t *testing.T) {
	fake := &fakeVolcanoLib{
		cdnList: map[string][]*volcanosdkcdn.CertInfoForListCertInfoOutput{
			"cert-partial": {{CertId: strPtr("cert-partial")}},
		},
		wafList: []*volcanosdkwaf.DataForListWafServiceCertificateOutput{
			{Id: int32Ptr(77), ExpireTime: strPtr("not-a-time")},
		},
		albDescribe: map[string][]*volcanosdkalb.CertificateForDescribeCertificatesOutput{
			"cert-alb-p": {{CertificateId: strPtr("cert-alb-p")}},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)
	ctx := context.Background()

	info, err := d.GetCert(ctx, testVolcanoCreds(), "cdn:cert-partial")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Zero(t, info.NotAfter, "缺 ExpireTime → 有效期零值")
	assert.Empty(t, info.Fingerprint)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "waf:77")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Zero(t, info.NotAfter, "不可解析时间形态 → 有效期零值")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "alb:cert-alb-p")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Zero(t, info.NotAfter, "缺 ExpiredAt → 有效期零值")

	d.Stop() // 无限流器接缝 no-op（进程退出安全）
}

func TestVolcanoDeployerGetCertRejectsNonNormalizedID(t *testing.T) {
	d, _ := newTestVolcanoDeployer(&fakeVolcanoLib{})
	for _, id := range []string{"inst-9001", "clb:cert-1", "csv:", "csv: "} {
		_, err := d.GetCert(context.Background(), testVolcanoCreds(), id)
		assert.ErrorContains(t, err, "not normalized", id)
	}
}

// 云侧查询失败（非 not-found）：错误透传（回滚判定 fail-safe 阻断，不误判有效）。
func TestVolcanoDeployerGetCertCloudErrorPropagates(t *testing.T) {
	fake := &fakeVolcanoLib{
		getErr: map[string]error{"inst-1": errors.New("internal server error")},
	}
	d, _ := newTestVolcanoDeployer(fake)

	_, err := d.GetCert(context.Background(), testVolcanoCreds(), "csv:inst-1")
	assert.ErrorContains(t, err, "internal server error", "not-found 归一外的查询失败按错误处理")
}

// ---------------------------------------------------------------------
// AC-3：CleanupOrphan（幂等成功 + 逐库路由）
// ---------------------------------------------------------------------

// 幂等：已删除证书双调用同结果（云侧 not-found 归一为成功，清理队列重放安全）。
func TestVolcanoDeployerCleanupOrphanIdempotent(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{
		delErr: map[string]error{"csv:inst-9001": errors.New("Volcengine: instance inst-9001 not found")},
	}
	d, _ := newTestVolcanoDeployer(fake)

	assert.NoError(t, d.CleanupOrphan(ctx, testVolcanoCreds(), "csv:inst-9001"))
	assert.NoError(t, d.CleanupOrphan(ctx, testVolcanoCreds(), "csv:inst-9001"), "双调用同结果（幂等）")
	assert.Equal(t, []string{"csv:inst-9001", "csv:inst-9001"}, fake.deletesSnapshot())
}

// 逐库路由：csv/cdn/waf/alb/nlb 删除落对应证书库 API（alb/nlb 共用 alb 服务
// 删除 API，nlb 路由经调用落在共享客户端验证）。
func TestVolcanoDeployerCleanupOrphanRoutesPerProduct(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{}
	d, _ := newTestVolcanoDeployer(fake)

	for _, id := range []string{"csv:inst-1", "cdn:cert-1", "waf:31", "alb:cert-2", "nlb:cert-3"} {
		assert.NoError(t, d.CleanupOrphan(ctx, testVolcanoCreds(), id))
	}
	assert.Equal(t, []string{"csv:inst-1", "cdn:cert-1", "waf:31", "alb:cert-2", "alb:cert-3"},
		fake.deletesSnapshot(), "五次删除全部路由成功，alb/nlb 共享删除 API")
}

// 删除持续失败（非 not-found）：错误透传 + 限流退避哨兵保留。
func TestVolcanoDeployerCleanupOrphanRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{delErr: map[string]error{
		"csv:inst-9001": fmt.Errorf("delete throttled: %w", cloudx.ErrCloudRateLimited),
	}}
	d, rec := newTestVolcanoDeployer(fake)

	err := d.CleanupOrphan(ctx, testVolcanoCreds(), "csv:inst-9001")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "补偿清理同样有界重试")
	assert.Len(t, fake.deletesSnapshot(), 5)
	assert.Len(t, rec.snapshot(), 4)
}

// 非归一 ID 清理：显式拒绝。
func TestVolcanoDeployerCleanupOrphanRejectsNonNormalizedID(t *testing.T) {
	d, _ := newTestVolcanoDeployer(&fakeVolcanoLib{})
	err := d.CleanupOrphan(context.Background(), testVolcanoCreds(), "inst-9001")
	assert.ErrorContains(t, err, "not normalized")
}

// ---------------------------------------------------------------------
// ID 归一小件 + 重试策略装配
// ---------------------------------------------------------------------

func TestVolcanoCloudCertIDNormalization(t *testing.T) {
	assert.Equal(t, "csv:inst-1", normalizeVolcanoCloudCertID(volcanoProductCSV, " inst-1 "))
	product, raw, ok := splitVolcanoCloudCertID("cdn:cert-1")
	assert.True(t, ok)
	assert.Equal(t, volcanoProductCDN, product)
	assert.Equal(t, "cert-1", raw)
	_, _, ok = splitVolcanoCloudCertID("clb:cert-1")
	assert.False(t, ok, "非火山证书库产品前缀拒绝")
	_, _, ok = splitVolcanoCloudCertID("cert-1")
	assert.False(t, ok, "无前缀拒绝")
	_, _, ok = splitVolcanoCloudCertID("cdn:")
	assert.False(t, ok, "空裸 ID 拒绝")
}

func TestVolcanoRetryPolicyOptionNormalization(t *testing.T) {
	def := DefaultRetryPolicy()
	assert.Equal(t, def, RetryPolicy{}.normalized())

	custom := RetryPolicy{
		MaxAttempts:  2,
		Backoffs:     []time.Duration{time.Second},
		MaxTotalWait: time.Second,
	}
	d := NewVolcanoDeployer(nil, WithVolcanoRetryPolicy(custom))
	assert.Equal(t, custom, d.retry)
	assert.Equal(t, def, NewVolcanoDeployer(nil, WithVolcanoRetryPolicy(RetryPolicy{})).retry,
		"零值配置回退缺省保守值")
}

// not-found 启发式：火山各库"已不存在"错误归一为幂等成功语义。
func TestVolcanoCertNotFoundHeuristic(t *testing.T) {
	assert.False(t, isVolcanoCertNotFoundErr(nil))
	assert.True(t, isVolcanoCertNotFoundErr(errors.New("instance not found")))
	assert.True(t, isVolcanoCertNotFoundErr(errors.New("The certificate cert-1 does not exist")))
	assert.True(t, isVolcanoCertNotFoundErr(errors.New("InvalidInstanceId: no such instance")))
	assert.True(t, isVolcanoCertNotFoundErr(errors.New("http 404 Not Found")))
	assert.False(t, isVolcanoCertNotFoundErr(errors.New("delete denied by policy")))
	assert.False(t, isVolcanoCertNotFoundErr(errors.New("rate throttled, retry later")))
}

// ---------------------------------------------------------------------
// 小件
// ---------------------------------------------------------------------

func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }
func int32Ptr(v int32) *int32 { return &v }
func int64Ptr(v int64) *int64 { return &v }

// ---------------------------------------------------------------------
// 任务 2：映射反查测试替身（CloudCertMappingRepository 窄桩，仅 FindByCloudCertID）
// ---------------------------------------------------------------------

type fakeVolcanoMappings struct {
	byCloudCertID map[string]string // "cloud|accountKey|cloudCertID" → fingerprint
	findCalls     int
}

func (m *fakeVolcanoMappings) Upsert(context.Context, *domain.CloudCertMapping) error {
	return errors.New("not implemented")
}

func (m *fakeVolcanoMappings) ListByFingerprint(context.Context, string) ([]domain.CloudCertMapping, error) {
	return nil, errors.New("not implemented")
}

func (m *fakeVolcanoMappings) FindByCloudCertID(_ context.Context, cloud, accountKey, cloudCertID string) (domain.CloudCertMapping, error) {
	m.findCalls++
	fp, ok := m.byCloudCertID[cloud+"|"+accountKey+"|"+cloudCertID]
	if !ok {
		return domain.CloudCertMapping{}, errors.New("no documents")
	}
	return domain.CloudCertMapping{CertFingerprint: fp, CloudCertID: cloudCertID}, nil
}

func (m *fakeVolcanoMappings) UpdateStatus(context.Context, string, domain.MappingStatus) error {
	return errors.New("not implemented")
}

func (m *fakeVolcanoMappings) ListByStatus(context.Context, domain.MappingStatus) ([]domain.CloudCertMapping, error) {
	return nil, errors.New("not implemented")
}

func (m *fakeVolcanoMappings) DeleteByID(context.Context, string) error {
	return errors.New("not implemented")
}

// ---------------------------------------------------------------------
// AC-1：BindResource 四产品分支（CDN BatchDeployCert / WAF UpdateDomain /
// ALB-NLB 监听证书置位）+ 失败可识别错误
// ---------------------------------------------------------------------

// CDN：BatchDeployCert(CertId, Domain=加速域名) 置位绑定；逐域部署结果非
// success 即失败（携带云侧 ErrorMsg）。
func TestVolcanoDeployerBindCDN(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{}
	d, _ := newTestVolcanoDeployer(fake)

	assert.NoError(t, d.BindResource(ctx, testVolcanoCreds(), volcanoProductCDN, "www.example.com", "cdn:cert-1"))
	require.Len(t, fake.cdnBindReqs, 1)
	req := fake.cdnBindReqs[0]
	assert.Equal(t, "cert-1", *req.CertId, "裸 ID 下发（归一前缀仅平台侧承载）")
	assert.Equal(t, "www.example.com", *req.Domain, "resourceID=加速域名（域名粒度，对齐扫描）")

	// 逐域部署失败：Status!=success → 错误携带域名与云侧 ErrorMsg
	failFake := &fakeVolcanoLib{cdnBindResult: []*volcanosdkcdn.DeployResultForBatchDeployCertOutput{
		{Domain: strPtr("www.example.com"), Status: strPtr("failed"), ErrorMsg: strPtr("domain not exists")},
	}}
	d2, _ := newTestVolcanoDeployer(failFake)
	err := d2.BindResource(ctx, testVolcanoCreds(), volcanoProductCDN, "www.example.com", "cdn:cert-1")
	assert.ErrorContains(t, err, "domain not exists", "可识别错误（对齐 wrapVolcanoCertErr 哨兵口径）")
	assert.ErrorContains(t, err, "cdn_batch_deploy_cert")
}

// WAF：防护域名证书替换——逐地域 ListDomain 定位（读现网 AccessMode）→
// UpdateDomain(Domain, CertificateID, AccessMode) 仅携带三字段。
func TestVolcanoDeployerBindWAF(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{wafDomains: map[string][]*volcanosdkwaf.DataForListDomainOutput{
		"cn-beijing": {
			{Domain: strPtr("app.example.com"), CertificateID: int32Ptr(31), AccessMode: int32Ptr(0)},
			{Domain: strPtr("other.example.com"), CertificateID: int32Ptr(32), AccessMode: int32Ptr(1)},
		},
	}}
	d, _ := newTestVolcanoDeployer(fake)

	assert.NoError(t, d.BindResource(ctx, testVolcanoCreds(), volcanoProductWAF, "app.example.com", "waf:31"))
	require.Len(t, fake.wafUpdateReqs, 1)
	req := fake.wafUpdateReqs[0]
	assert.Equal(t, "app.example.com", *req.Domain)
	assert.Equal(t, int32(31), *req.CertificateID)
	assert.Equal(t, int32(0), *req.AccessMode, "AccessMode 取现网值（UpdateDomain 必填）")

	// 域名不在任何地域：显式报错
	err := d.BindResource(ctx, testVolcanoCreds(), volcanoProductWAF, "ghost.example.com", "waf:31")
	assert.ErrorContains(t, err, "not found in regions")

	// 命中域名但 AccessMode 缺失：fail-fast（不猜默认值）
	noMode := &fakeVolcanoLib{wafDomains: map[string][]*volcanosdkwaf.DataForListDomainOutput{
		"cn-beijing": {{Domain: strPtr("app.example.com"), CertificateID: int32Ptr(31)}},
	}}
	d3, _ := newTestVolcanoDeployer(noMode)
	err = d3.BindResource(ctx, testVolcanoCreds(), volcanoProductWAF, "app.example.com", "waf:31")
	assert.ErrorContains(t, err, "access mode unavailable")

	// 非数字 WAF 证书 ID：fail-fast
	err = d.BindResource(ctx, testVolcanoCreds(), volcanoProductWAF, "app.example.com", "waf:cert-x")
	assert.ErrorContains(t, err, "not a waf service certificate id")
}

// ALB：监听证书置位——ListenerIds 定点定位（复合形态附 LoadBalancerId 收窄）→
// ModifyListenerAttributes(ListenerId, CertificateId)；HTTP 监听显式拒绝；
// 纯监听形态（无 lbId 前缀）容忍。
func TestVolcanoDeployerBindALB(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{albListeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{
		{ListenerId: strPtr("lsn-1"), LoadBalancerId: strPtr("alb-1"), Protocol: strPtr("HTTPS")},
		{ListenerId: strPtr("lsn-http"), LoadBalancerId: strPtr("alb-1"), Protocol: strPtr("HTTP")},
	}}
	d, _ := newTestVolcanoDeployer(fake)

	assert.NoError(t, d.BindResource(ctx, testVolcanoCreds(), volcanoProductALB, "alb-1/lsn-1", "alb:cert-alb-1"))
	require.Len(t, fake.albModifyReqs, 1)
	assert.Equal(t, "lsn-1", *fake.albModifyReqs[0].ListenerId)
	assert.Equal(t, "cert-alb-1", *fake.albModifyReqs[0].CertificateId, "裸 ID 下发")

	// HTTP 监听无服务器证书：显式报错（对齐扫描跳过口径）
	err := d.BindResource(ctx, testVolcanoCreds(), volcanoProductALB, "alb-1/lsn-http", "alb:cert-alb-1")
	assert.ErrorContains(t, err, "HTTP (no server certificate)")

	// 纯监听形态（升级窗口互认）容忍
	fake.albModifyReqs = nil
	assert.NoError(t, d.BindResource(ctx, testVolcanoCreds(), volcanoProductALB, "lsn-1", "alb:cert-alb-1"))
	require.Len(t, fake.albModifyReqs, 1)
}

// NLB：监听证书置位走 clb 服务 NLB API（ModifyNLBListenerAttributes）。
func TestVolcanoDeployerBindNLB(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{nlbListeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{
		{ListenerId: strPtr("lsn-nlb-1"), LoadBalancerId: strPtr("nlb-1"), Protocol: strPtr("TLS")},
	}}
	d, _ := newTestVolcanoDeployer(fake)

	assert.NoError(t, d.BindResource(ctx, testVolcanoCreds(), volcanoProductNLB, "nlb-1/lsn-nlb-1", "nlb:cert-nlb-1"))
	require.Len(t, fake.nlbModifyReqs, 1)
	assert.Equal(t, "lsn-nlb-1", *fake.nlbModifyReqs[0].ListenerId)
	assert.Equal(t, "cert-nlb-1", *fake.nlbModifyReqs[0].CertificateId)

	// 监听不存在于任何地域：显式报错
	err := d.BindResource(ctx, testVolcanoCreds(), volcanoProductNLB, "nlb-1/lsn-ghost", "nlb:cert-nlb-1")
	assert.ErrorContains(t, err, "not found in regions")
}

// 绑定入参校验：非归一 ID / csv 统一库实例（ErrVolcanoCSVCertNotBindable）/
// 证书前缀与目标产品不符 / 未支持产品 / 空 resourceID——全部 fail-fast 且
// 不触达云侧。
func TestVolcanoDeployerBindRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{}
	d, _ := newTestVolcanoDeployer(fake)

	err := d.BindResource(ctx, testVolcanoCreds(), volcanoProductCDN, "www.example.com", "cert-1")
	assert.ErrorContains(t, err, "not normalized", "非归一 ID 拒绝")

	err = d.BindResource(ctx, testVolcanoCreds(), volcanoProductCDN, "www.example.com", "csv:inst-9001")
	assert.ErrorIs(t, err, ErrVolcanoCSVCertNotBindable, "csv 统一库实例不可直接绑定产品资源")

	err = d.BindResource(ctx, testVolcanoCreds(), volcanoProductWAF, "app.example.com", "cdn:cert-1")
	assert.ErrorIs(t, err, errVolcanoBindCertProductMismatch, "证书前缀与目标产品不符")

	err = d.BindResource(ctx, testVolcanoCreds(), "clb", "lsn-1", "alb:cert-1")
	assert.ErrorIs(t, err, ErrVolcanoProductNotSupported)

	err = d.BindResource(ctx, testVolcanoCreds(), volcanoProductALB, " ", "alb:cert-1")
	assert.ErrorContains(t, err, "non-empty resource id")

	foreign := testVolcanoCreds()
	foreign.Cloud = "aliyun"
	err = d.BindResource(ctx, foreign, volcanoProductCDN, "www.example.com", "cdn:cert-1")
	assert.ErrorContains(t, err, "not volcano")

	assert.Zero(t, fake.cdnBindCalls+fake.wafListCalls, "校验失败不产生云侧调用")
}

// alb/nlb 证书库互跨容忍：共用 ALB 监听证书库（与 GetCert/CleanupOrphan 路由
// 口径一致）。
func TestVolcanoDeployerBindALBFamilyCrossLib(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{nlbListeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{
		{ListenerId: strPtr("lsn-nlb-1"), LoadBalancerId: strPtr("nlb-1"), Protocol: strPtr("TLS")},
	}}
	d, _ := newTestVolcanoDeployer(fake)
	assert.NoError(t, d.BindResource(ctx, testVolcanoCreds(), volcanoProductNLB, "nlb-1/lsn-nlb-1", "alb:cert-shared-1"),
		"alb 库证书绑定 nlb 监听（共享证书库）")
}

// AC-3：幂等——同一 (resource, cloudCertID) 重绑收敛（绑定 API 置位语义，
// 重放同结果；四产品各一断言）。
func TestVolcanoDeployerBindIdempotentRebind(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{
		wafDomains: map[string][]*volcanosdkwaf.DataForListDomainOutput{
			"cn-beijing": {{Domain: strPtr("app.example.com"), CertificateID: int32Ptr(31), AccessMode: int32Ptr(0)}},
		},
		albListeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{
			{ListenerId: strPtr("lsn-1"), LoadBalancerId: strPtr("alb-1"), Protocol: strPtr("HTTPS")},
		},
		nlbListeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{
			{ListenerId: strPtr("lsn-nlb-1"), LoadBalancerId: strPtr("nlb-1"), Protocol: strPtr("TLS")},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)
	creds := testVolcanoCreds()

	for i := 0; i < 2; i++ {
		assert.NoError(t, d.BindResource(ctx, creds, volcanoProductCDN, "www.example.com", "cdn:cert-1"))
		assert.NoError(t, d.BindResource(ctx, creds, volcanoProductWAF, "app.example.com", "waf:31"))
		assert.NoError(t, d.BindResource(ctx, creds, volcanoProductALB, "alb-1/lsn-1", "alb:cert-alb-1"))
		assert.NoError(t, d.BindResource(ctx, creds, volcanoProductNLB, "nlb-1/lsn-nlb-1", "nlb:cert-nlb-1"))
	}
	// 重放请求逐次一致（置位语义收敛）
	require.Len(t, fake.cdnBindReqs, 2)
	assert.Equal(t, *fake.cdnBindReqs[0].CertId, *fake.cdnBindReqs[1].CertId)
	assert.Equal(t, *fake.cdnBindReqs[0].Domain, *fake.cdnBindReqs[1].Domain)
	require.Len(t, fake.wafUpdateReqs, 2)
	assert.Equal(t, *fake.wafUpdateReqs[0].CertificateID, *fake.wafUpdateReqs[1].CertificateID)
	require.Len(t, fake.albModifyReqs, 2)
	require.Len(t, fake.nlbModifyReqs, 2)
}

// 绑定限流退避（boundedRetry 共享口径）+ 一般失败立即返回。
func TestVolcanoDeployerBindRateLimitedAndFailure(t *testing.T) {
	ctx := context.Background()
	rateFake := &fakeVolcanoLib{cdnBindErrFn: func(call int) error {
		if call <= 2 {
			return fmt.Errorf("volcano cdn_batch_deploy_cert api error: %w", cloudx.ErrCloudRateLimited)
		}
		return nil
	}}
	d, rec := newTestVolcanoDeployer(rateFake)
	assert.NoError(t, d.BindResource(ctx, testVolcanoCreds(), volcanoProductCDN, "www.example.com", "cdn:cert-1"))
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot())

	hardFake := &fakeVolcanoLib{cdnBindErrFn: func(int) error { return errors.New("bind denied by policy") }}
	d2, _ := newTestVolcanoDeployer(hardFake)
	err := d2.BindResource(ctx, testVolcanoCreds(), volcanoProductCDN, "www.example.com", "cdn:cert-1")
	assert.ErrorContains(t, err, "bind denied by policy")
	assert.Len(t, hardFake.cdnBindReqs, 1, "一般失败不重试")
}

// ---------------------------------------------------------------------
// AC-2：ListReferences 四产品枚举（活体重查面，resourceId 形态对齐任务 3）
// ---------------------------------------------------------------------

// CDN：域名粒度（resourceID=加速域名、cloudCertID=cdn:{certId}）；未配置域名
// 的证书不构成引用；翻页分支（refPageSize 缩小）。
func TestVolcanoDeployerListReferencesCDN(t *testing.T) {
	fake := &fakeVolcanoLib{cdnCertInfoList: []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{
		{CertId: strPtr("cert-cdn-1"), ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{
			{Domain: strPtr("a.example.com")},
			{Domain: strPtr("b.example.com")},
		}},
		{CertId: strPtr("cert-cdn-2"), ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{
			{Domain: strPtr("c.example.com")},
		}},
		{CertId: strPtr("cert-cdn-nodomain")}, // 未配置域名不构成引用
	}}
	d, _ := newTestVolcanoDeployer(fake)
	d.refPageSize = 2 // 强制翻页（fake 按入参切片）

	refs, err := d.ListReferences(context.Background(), testVolcanoCreds(), volcanoProductCDN)
	assert.NoError(t, err)
	require.Len(t, refs, 3)
	assert.Equal(t, "a.example.com", refs[0].ResourceID)
	assert.Equal(t, "cdn:cert-cdn-1", refs[0].ReferencedCloudCertID)
	assert.Equal(t, "c.example.com", refs[2].ResourceID)
	assert.Equal(t, "cdn:cert-cdn-2", refs[2].ReferencedCloudCertID)
	assert.Equal(t, volcanoCloud, refs[0].Cloud, "火山云标识经 shared/domain 转译同值")
}

// WAF：防护域名粒度；列表内联 CertificateID（0/缺省跳过）；跨地域遍历。
func TestVolcanoDeployerListReferencesWAF(t *testing.T) {
	fake := &fakeVolcanoLib{wafDomains: map[string][]*volcanosdkwaf.DataForListDomainOutput{
		"cn-beijing": {
			{Domain: strPtr("app.example.com"), CertificateID: int32Ptr(31)},
			{Domain: strPtr("nocert.example.com")}, // 未配置证书不构成引用
			{Domain: strPtr("zero.example.com"), CertificateID: int32Ptr(0)},
		},
	}}
	d, _ := newTestVolcanoDeployer(fake)

	refs, err := d.ListReferences(context.Background(), testVolcanoCreds(), volcanoProductWAF)
	assert.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "app.example.com", refs[0].ResourceID)
	assert.Equal(t, "waf:31", refs[0].ReferencedCloudCertID)
}

// ALB：监听复合 resourceID、主证书 + SNI 扩展证书（同 ID 去重）、HTTP 跳过、
// CertCenterCertificateId 回退、served domains 展开（规则查询失败置空不阻塞）。
func TestVolcanoDeployerListReferencesALB(t *testing.T) {
	fake := &fakeVolcanoLib{
		albListeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{
			{ListenerId: strPtr("lsn-1"), LoadBalancerId: strPtr("alb-1"), Protocol: strPtr("HTTPS"),
				CertificateId: strPtr("cert-alb-1"),
				DomainExtensions: []*volcanosdkalb.DomainExtensionForDescribeListenersOutput{
					{CertificateId: strPtr("cert-alb-ext")},
					{CertificateId: strPtr("cert-alb-1")}, // 与主证书同 ID 去重
					{CertificateId: nil},
				}},
			{ListenerId: strPtr("lsn-http"), LoadBalancerId: strPtr("alb-1"), Protocol: strPtr("http"),
				CertificateId: strPtr("cert-alb-2")}, // HTTP 监听跳过
			{ListenerId: strPtr("lsn-cc"), LoadBalancerId: strPtr("alb-1"), Protocol: strPtr("HTTPS"),
				CertCenterCertificateId: strPtr("cert-cc-1")}, // 证书中心形态回退
			{ListenerId: strPtr("lsn-nocert"), LoadBalancerId: strPtr("alb-1"), Protocol: strPtr("HTTPS")}, // 无证书跳过
		},
		albRules: map[string][]*volcanosdkalb.RuleForDescribeRulesOutput{
			"lsn-1": {{RuleConditions: []*volcanosdkalb.RuleConditionForDescribeRulesOutput{
				{Type: strPtr("host"), HostConfig: &volcanosdkalb.HostConfigForDescribeRulesOutput{
					Values: []*string{strPtr("a.example.com"), strPtr("b.example.com")},
				}},
				{Type: strPtr("path"), HostConfig: &volcanosdkalb.HostConfigForDescribeRulesOutput{
					Values: []*string{strPtr("/ignored")},
				}},
			}}},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)

	refs, err := d.ListReferences(context.Background(), testVolcanoCreds(), volcanoProductALB)
	assert.NoError(t, err)
	require.Len(t, refs, 3, "主证书 + SNI 扩展证书 + 证书中心形态")
	assert.Equal(t, "alb-1/lsn-1", refs[0].ResourceID, "监听复合 resourceID")
	assert.Equal(t, "alb:cert-alb-1", refs[0].ReferencedCloudCertID)
	assert.Equal(t, []string{"a.example.com", "b.example.com"}, refs[0].ServedDomains, "served domains 展开（host 条件）")
	assert.Equal(t, "alb:cert-alb-ext", refs[1].ReferencedCloudCertID)
	assert.Equal(t, "alb-1/lsn-cc", refs[2].ResourceID)
	assert.Equal(t, "alb:cert-cc-1", refs[2].ReferencedCloudCertID)

	// 转发规则查询失败：served 置空不阻塞枚举主干
	fakeErr := &fakeVolcanoLib{
		albListeners: fake.albListeners,
		albRulesErr:  errors.New("describe rules denied"),
	}
	d2, _ := newTestVolcanoDeployer(fakeErr)
	refs2, err := d2.ListReferences(context.Background(), testVolcanoCreds(), volcanoProductALB)
	assert.NoError(t, err)
	require.Len(t, refs2, 3)
	assert.Empty(t, refs2[0].ServedDomains, "规则失败 → served 置空（回退 coverage）")
}

// NLB：L4 TLS 证书内联监听器（无证书监听跳过）、复合 resourceID、NextToken 翻页。
func TestVolcanoDeployerListReferencesNLB(t *testing.T) {
	fake := &fakeVolcanoLib{nlbListeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{
		{ListenerId: strPtr("lsn-nlb-1"), LoadBalancerId: strPtr("nlb-1"), Protocol: strPtr("TLS"),
			CertificateId: strPtr("cert-nlb-1")},
		{ListenerId: strPtr("lsn-nlb-2"), LoadBalancerId: strPtr("nlb-1"), Protocol: strPtr("TCP")}, // 无证书跳过
		{ListenerId: strPtr("lsn-nlb-3"), LoadBalancerId: strPtr("nlb-1"), Protocol: strPtr("TLS"),
			CertificateId: strPtr("cert-nlb-3")},
	}}
	d, _ := newTestVolcanoDeployer(fake)
	d.refPageSize = 1 // 强制 NextToken 翻页（token=偏移量）

	refs, err := d.ListReferences(context.Background(), testVolcanoCreds(), volcanoProductNLB)
	assert.NoError(t, err)
	require.Len(t, refs, 2)
	assert.Equal(t, "nlb-1/lsn-nlb-1", refs[0].ResourceID)
	assert.Equal(t, "nlb:cert-nlb-1", refs[0].ReferencedCloudCertID)
	assert.Equal(t, "nlb:cert-nlb-3", refs[1].ReferencedCloudCertID)
	assert.Empty(t, refs[0].ServedDomains, "NLB（L4）无转发规则")
}

// 未支持产品：哨兵错误。
func TestVolcanoDeployerListReferencesUnsupportedProduct(t *testing.T) {
	d, _ := newTestVolcanoDeployer(&fakeVolcanoLib{})
	_, err := d.ListReferences(context.Background(), testVolcanoCreds(), "clb")
	assert.ErrorIs(t, err, ErrVolcanoProductNotSupported)
}

// ---------------------------------------------------------------------
// AC-2 指纹解析：映射反查 → GetCert 要素 → 确定性占位（与 3.5 扫描路径可对账）
// ---------------------------------------------------------------------

// 映射反查命中 → 精确指纹（映射键 = {product}:{id} 归一口径，与扫描引用同键）；
// 未命中 → cdn 原生 sha256 通道；waf 无指纹通道 → 映射/占位；同证书多引用去重查询。
func TestVolcanoDeployerListReferencesFingerprintResolution(t *testing.T) {
	sha256Fp := strings.Repeat("a", 64)
	fake := &fakeVolcanoLib{
		cdnCertInfoList: []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{
			{CertId: strPtr("cert-cdn-1"), ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{
				{Domain: strPtr("a.example.com")},
			}},
		},
		cdnList: map[string][]*volcanosdkcdn.CertInfoForListCertInfoOutput{
			"cert-cdn-1": {{CertId: strPtr("cert-cdn-1"),
				CertFingerprint: &volcanosdkcdn.CertFingerprintForListCertInfoOutput{Sha256: strPtr(sha256Fp)}}},
		},
		wafDomains: map[string][]*volcanosdkwaf.DataForListDomainOutput{
			"cn-beijing": {
				{Domain: strPtr("app.example.com"), CertificateID: int32Ptr(31)},
				{Domain: strPtr("app2.example.com"), CertificateID: int32Ptr(31)}, // 同证书双引用（去重查询）
			},
		},
	}
	mappings := &fakeVolcanoMappings{byCloudCertID: map[string]string{
		"volcano|acc-main|waf:31": strings.Repeat("b", 64), // 映射反查命中（waf 无指纹通道）
	}}
	d, _ := newTestVolcanoDeployer(fake)
	d.mappings = mappings

	refs, err := d.ListReferences(context.Background(), testVolcanoCreds(), volcanoProductCDN)
	assert.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, sha256Fp, refs[0].CertFingerprint, "cdn 未命中映射 → GetCert 原生 sha256 通道")

	refs, err = d.ListReferences(context.Background(), testVolcanoCreds(), volcanoProductWAF)
	assert.NoError(t, err)
	require.Len(t, refs, 2)
	assert.Equal(t, strings.Repeat("b", 64), refs[0].CertFingerprint, "映射反查命中 → 精确指纹")
	assert.Equal(t, strings.Repeat("b", 64), refs[1].CertFingerprint, "同证书多引用去重（单次反查）")
	assert.Equal(t, 2, mappings.findCalls, "cdn 调用反查 1 次 + waf 双引用共享解析缓存反查 1 次")

	// 无映射仓储 + waf 无指纹通道：确定性占位（公式与 3.5 service.resolveUncached 一致）
	d2, _ := newTestVolcanoDeployer(fake)
	d2.mappings = nil
	refs2, err := d2.ListReferences(context.Background(), testVolcanoCreds(), volcanoProductWAF)
	assert.NoError(t, err)
	require.Len(t, refs2, 2)
	wantPlaceholder := unresolvedPlaceholderFingerprint("volcano|acc-main|waf:31")
	assert.Equal(t, wantPlaceholder, refs2[0].CertFingerprint, "占位公式与扫描路径一致可对账")
	assert.Regexp(t, "^[0-9a-f]{64}$", refs2[0].CertFingerprint)
}

// ---------------------------------------------------------------------
// AC-5：与任务 1 的 {product}:{id} ID 归一互操作（绑定引用归一 ID 可被回滚
// GetCert 解析；ListReferences 产出引用同口径）
// ---------------------------------------------------------------------

func TestVolcanoDeployerBindGetCertInterop(t *testing.T) {
	ctx := context.Background()
	notAfter := time.Unix(1790000000, 0)
	fake := &fakeVolcanoLib{
		cdnList: map[string][]*volcanosdkcdn.CertInfoForListCertInfoOutput{
			"cert-cdn-1": {{CertId: strPtr("cert-cdn-1"), ExpireTime: int64Ptr(1790000000),
				CertFingerprint: &volcanosdkcdn.CertFingerprintForListCertInfoOutput{Sha256: strPtr(strings.Repeat("a", 64))}},
			},
		},
		wafList: []*volcanosdkwaf.DataForListWafServiceCertificateOutput{
			{Id: int32Ptr(31), ExpireTime: strPtr("2026-09-01 00:00:00")},
		},
		albDescribe: map[string][]*volcanosdkalb.CertificateForDescribeCertificatesOutput{
			"cert-alb-1": {{CertificateId: strPtr("cert-alb-1"), ExpiredAt: strPtr("2026-09-01T00:00:00Z")}},
			"cert-nlb-1": {{CertificateId: strPtr("cert-nlb-1"), ExpiredAt: strPtr("2026-09-01T00:00:00Z")}},
		},
		wafDomains: map[string][]*volcanosdkwaf.DataForListDomainOutput{
			"cn-beijing": {{Domain: strPtr("app.example.com"), CertificateID: int32Ptr(31), AccessMode: int32Ptr(0)}},
		},
		cdnCertInfoList: []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput{
			{CertId: strPtr("cert-cdn-1"), ConfiguredDomainDetail: []*volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{
				{Domain: strPtr("www.example.com")},
			}},
		},
		albListeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{
			{ListenerId: strPtr("lsn-1"), LoadBalancerId: strPtr("alb-1"), Protocol: strPtr("HTTPS"),
				CertificateId: strPtr("cert-alb-1")},
		},
		nlbListeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{
			{ListenerId: strPtr("lsn-nlb-1"), LoadBalancerId: strPtr("nlb-1"), Protocol: strPtr("TLS"),
				CertificateId: strPtr("cert-nlb-1")},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)
	creds := testVolcanoCreds()

	cases := []struct {
		product string
		resID   string
		certID  string
	}{
		{volcanoProductCDN, "www.example.com", "cdn:cert-cdn-1"},
		{volcanoProductWAF, "app.example.com", "waf:31"},
		{volcanoProductALB, "alb-1/lsn-1", "alb:cert-alb-1"},
		{volcanoProductNLB, "nlb-1/lsn-nlb-1", "nlb:cert-nlb-1"},
	}
	for _, tc := range cases {
		// 绑定消费归一 ID → 回滚 GetCert 按前缀路由解析（Exists=true）
		assert.NoError(t, d.BindResource(ctx, creds, tc.product, tc.resID, tc.certID), tc.product)
		info, err := d.GetCert(ctx, creds, tc.certID)
		assert.NoError(t, err, tc.product)
		assert.True(t, info.Exists, "绑定引用归一 ID 可被回滚 GetCert 解析（%s）", tc.certID)
	}

	// ListReferences 产出引用的 ReferencedCloudCertID 同口径可被 GetCert 解析
	for _, product := range []string{volcanoProductCDN, volcanoProductWAF, volcanoProductALB, volcanoProductNLB} {
		refs, err := d.ListReferences(ctx, creds, product)
		assert.NoError(t, err, product)
		assert.NotEmpty(t, refs, product)
		for _, r := range refs {
			_, _, ok := splitVolcanoCloudCertID(r.ReferencedCloudCertID)
			assert.True(t, ok, "引用云证书 ID 为归一形态：%q", r.ReferencedCloudCertID)
			info, err := d.GetCert(ctx, creds, r.ReferencedCloudCertID)
			assert.NoError(t, err, r.ReferencedCloudCertID)
			assert.True(t, info.Exists, "引用可被 GetCert 解析（%s）", r.ReferencedCloudCertID)
		}
	}

	// 互操作路径上的 GetCert 要素语义（cdn 原生 sha256 + 有效期 unix 秒）
	info, err := d.GetCert(ctx, creds, "cdn:cert-cdn-1")
	assert.NoError(t, err)
	assert.Equal(t, notAfter, info.NotAfter)
}
