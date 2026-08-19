package aliyun

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/alb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/nlb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== ALB ====================

// fakeAlbClient ALB SDK fake（按地域分发）
type fakeAlbClient struct {
	listeners        []alb.Listener
	listenersByToken map[string][]alb.Listener // NextToken 分页模拟（简化：单页+token 翻转）
	listErr          error
	certPages        map[string][][]alb.CertificateModel // listenerId -> 证书分页
	certErr          map[string]error
	updateErr        error
	updateReqs       []*alb.UpdateListenerAttributeRequest
	listCalls        int
}

func (f *fakeAlbClient) ListListeners(request *alb.ListListenersRequest) (*alb.ListListenersResponse, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	resp := alb.CreateListListenersResponse()
	for _, lsn := range f.listeners {
		if request.ListenerIds == nil || len(*request.ListenerIds) == 0 || certContainsString(*request.ListenerIds, lsn.ListenerId) {
			resp.Listeners = append(resp.Listeners, lsn)
		}
	}
	resp.TotalCount = len(resp.Listeners)
	return resp, nil
}

func (f *fakeAlbClient) ListListenerCertificates(request *alb.ListListenerCertificatesRequest) (*alb.ListListenerCertificatesResponse, error) {
	if err, ok := f.certErr[request.ListenerId]; ok && err != nil {
		return nil, err
	}
	pages := f.certPages[request.ListenerId]
	pageIdx := 0
	if request.NextToken != "" {
		pageIdx = 1
	}
	resp := alb.CreateListListenerCertificatesResponse()
	if pageIdx >= 0 && pageIdx < len(pages) {
		resp.Certificates = pages[pageIdx]
		resp.TotalCount = len(pages[pageIdx])
		if pageIdx+1 < len(pages) {
			resp.NextToken = "next"
		}
	}
	return resp, nil
}

func (f *fakeAlbClient) UpdateListenerAttribute(request *alb.UpdateListenerAttributeRequest) (*alb.UpdateListenerAttributeResponse, error) {
	f.updateReqs = append(f.updateReqs, request)
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	resp := alb.CreateUpdateListenerAttributeResponse()
	resp.JobId = "job-1"
	return resp, nil
}

func certContainsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

func TestCertAdapterALBListReferences(t *testing.T) {
	t.Run("NextToken分页遍历监听与证书", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeAlbClient{
			listeners: []alb.Listener{
				{ListenerId: "lsn-1", ListenerProtocol: "HTTPS", LoadBalancerId: "alb-1"},
				{ListenerId: "lsn-2", ListenerProtocol: "HTTP", LoadBalancerId: "alb-1"},
				{ListenerId: "lsn-3", ListenerProtocol: "HTTPS", LoadBalancerId: "alb-2"},
			},
			certPages: map[string][][]alb.CertificateModel{
				"lsn-1": {{
					{CertificateId: "111-cn-hangzhou", IsDefault: true, CertificateType: "Server"},
					{CertificateId: "222-cn-hangzhou", IsDefault: false, CertificateType: "Server"},
				}},
				"lsn-3": {{{CertificateId: "333-cn-hangzhou", IsDefault: true, CertificateType: "Server"}}},
			},
		}
		adapter.newAlbClient = func(creds *domain.CloudAccount, region string) (albCertAPI, error) {
			if region == "cn-shanghai" {
				return &fakeAlbClient{}, nil
			}
			return fake, nil
		}

		refs, err := adapter.ListReferences(context.Background(), testCertCreds(), CertProductALB)
		require.NoError(t, err)
		require.Len(t, refs, 3)

		assert.Equal(t, "lsn-1", refs[0].ResourceID)
		assert.Equal(t, "111-cn-hangzhou", refs[0].ReferencedCloudCertID)
		assert.Equal(t, "222-cn-hangzhou", refs[1].ReferencedCloudCertID)
		assert.Equal(t, "aliyun", refs[1].Cloud)
		assert.Equal(t, CertProductALB, refs[1].Product)
		assert.Equal(t, "aliyun-main", refs[1].AccountKey)

		assert.Equal(t, "lsn-3", refs[2].ResourceID)
		assert.Equal(t, "333-cn-hangzhou", refs[2].ReferencedCloudCertID)
	})

	t.Run("证书分页NextToken遍历", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeAlbClient{
			listeners: []alb.Listener{
				{ListenerId: "lsn-1", ListenerProtocol: "HTTPS"},
			},
			certPages: map[string][][]alb.CertificateModel{
				"lsn-1": {
					{{CertificateId: "111-cn-hangzhou", IsDefault: true, CertificateType: "Server"}},
					{{CertificateId: "222-cn-hangzhou", IsDefault: false, CertificateType: "Server"}},
				},
			},
		}
		adapter.newAlbClient = func(creds *domain.CloudAccount, region string) (albCertAPI, error) {
			if region == "cn-shanghai" {
				return &fakeAlbClient{}, nil
			}
			return fake, nil
		}

		refs, err := adapter.ListReferences(context.Background(), testCertCreds(), CertProductALB)
		require.NoError(t, err)
		require.Len(t, refs, 2)
		assert.Equal(t, "222-cn-hangzhou", refs[1].ReferencedCloudCertID)
	})

	t.Run("限流错误映射哨兵", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		adapter.newAlbClient = func(creds *domain.CloudAccount, region string) (albCertAPI, error) {
			return &fakeAlbClient{listErr: errFakeThrottling}, nil
		}
		_, err := adapter.ListReferences(context.Background(), testCertCreds(), CertProductALB)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCloudRateLimited)
	})

	t.Run("单监听证书列举失败跳过继续", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeAlbClient{
			listeners: []alb.Listener{
				{ListenerId: "lsn-1", ListenerProtocol: "HTTPS"},
				{ListenerId: "lsn-2", ListenerProtocol: "HTTPS"},
			},
			certPages: map[string][][]alb.CertificateModel{
				"lsn-2": {{{CertificateId: "222-cn-hangzhou", IsDefault: true, CertificateType: "Server"}}},
			},
			certErr: map[string]error{"lsn-1": errors.New("listener cert list boom")},
		}
		adapter.newAlbClient = func(creds *domain.CloudAccount, region string) (albCertAPI, error) {
			if region == "cn-shanghai" {
				return &fakeAlbClient{}, nil
			}
			return fake, nil
		}

		refs, err := adapter.ListReferences(context.Background(), testCertCreds(), CertProductALB)
		require.NoError(t, err)
		require.Len(t, refs, 1)
		assert.Equal(t, "lsn-2", refs[0].ResourceID)
	})
}

func TestCertAdapterALBBindResource(t *testing.T) {
	t.Run("跨地域定位监听并替换默认证书保留扩展证书", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		hangzhou := &fakeAlbClient{}
		shanghai := &fakeAlbClient{
			listeners: []alb.Listener{
				{ListenerId: "lsn-target", ListenerProtocol: "HTTPS", LoadBalancerId: "alb-9"},
			},
			certPages: map[string][][]alb.CertificateModel{
				"lsn-target": {{
					{CertificateId: "old-cn-hangzhou", IsDefault: true, CertificateType: "Server"},
					{CertificateId: "ext-cn-hangzhou", IsDefault: false, CertificateType: "Server"},
				}},
			},
		}
		adapter.newAlbClient = func(creds *domain.CloudAccount, region string) (albCertAPI, error) {
			if region == "cn-shanghai" {
				return shanghai, nil
			}
			return hangzhou, nil
		}

		err := adapter.BindResource(context.Background(), testCertCreds(), CertProductALB, "lsn-target", "new-cn-hangzhou")
		require.NoError(t, err)

		require.Len(t, shanghai.updateReqs, 1)
		assert.Empty(t, hangzhou.updateReqs)
		req := shanghai.updateReqs[0]
		assert.Equal(t, "lsn-target", req.ListenerId)
		require.NotNil(t, req.Certificates)
		require.Len(t, *req.Certificates, 2)
		assert.Equal(t, "new-cn-hangzhou", (*req.Certificates)[0].CertificateId) // 新证书为默认
		assert.Equal(t, "ext-cn-hangzhou", (*req.Certificates)[1].CertificateId) // 扩展证书保留
	})

	t.Run("HTTP监听显式报错", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeAlbClient{
			listeners: []alb.Listener{
				{ListenerId: "lsn-http", ListenerProtocol: "HTTP"},
			},
		}
		adapter.newAlbClient = func(creds *domain.CloudAccount, region string) (albCertAPI, error) { return fake, nil }

		err := adapter.BindResource(context.Background(), testCertCreds(), CertProductALB, "lsn-http", "new")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP")
	})

	t.Run("监听不存在显式报错", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		adapter.newAlbClient = func(creds *domain.CloudAccount, region string) (albCertAPI, error) {
			return &fakeAlbClient{}, nil
		}
		err := adapter.BindResource(context.Background(), testCertCreds(), CertProductALB, "lsn-missing", "new")
		require.Error(t, err)
	})

	t.Run("更新限流映射哨兵", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeAlbClient{
			listeners: []alb.Listener{
				{ListenerId: "lsn-1", ListenerProtocol: "HTTPS"},
			},
			certPages: map[string][][]alb.CertificateModel{
				"lsn-1": {{{CertificateId: "old-cn-hangzhou", IsDefault: true, CertificateType: "Server"}}},
			},
			updateErr: errFakeThrottling,
		}
		adapter.newAlbClient = func(creds *domain.CloudAccount, region string) (albCertAPI, error) { return fake, nil }

		err := adapter.BindResource(context.Background(), testCertCreds(), CertProductALB, "lsn-1", "new-cn-hangzhou")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCloudRateLimited)
	})
}

// ==================== NLB ====================

// fakeNlbClient NLB SDK fake
type fakeNlbClient struct {
	listeners  []nlb.ListenerInfo
	listErr    error
	updateErr  error
	updateReqs []*nlb.UpdateListenerAttributeRequest
	listCalls  int
}

func (f *fakeNlbClient) ListListeners(request *nlb.ListListenersRequest) (*nlb.ListListenersResponse, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	resp := nlb.CreateListListenersResponse()
	resp.Success = true
	for _, lsn := range f.listeners {
		if request.ListenerIds == nil || len(*request.ListenerIds) == 0 || certContainsString(*request.ListenerIds, lsn.ListenerId) {
			resp.Listeners = append(resp.Listeners, lsn)
		}
	}
	resp.TotalCount = len(resp.Listeners)
	return resp, nil
}

func (f *fakeNlbClient) UpdateListenerAttribute(request *nlb.UpdateListenerAttributeRequest) (*nlb.UpdateListenerAttributeResponse, error) {
	f.updateReqs = append(f.updateReqs, request)
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	resp := nlb.CreateUpdateListenerAttributeResponse()
	resp.Success = true
	return resp, nil
}

func TestCertAdapterNLBListReferences(t *testing.T) {
	t.Run("遍历监听内联证书ID", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeNlbClient{
			listeners: []nlb.ListenerInfo{
				{ListenerId: "lsn-tls-1", ListenerProtocol: "TLS", CertificateIds: []string{"111-cn-hangzhou", "222-cn-hangzhou"}},
				{ListenerId: "lsn-tcp-1", ListenerProtocol: "TCP"},
			},
		}
		adapter.newNlbClient = func(creds *domain.CloudAccount, region string) (nlbCertAPI, error) {
			if region == "cn-shanghai" {
				return &fakeNlbClient{listCalls: 0}, nil
			}
			return fake, nil
		}

		refs, err := adapter.ListReferences(context.Background(), testCertCreds(), CertProductNLB)
		require.NoError(t, err)
		require.Len(t, refs, 2)

		assert.Equal(t, "lsn-tls-1", refs[0].ResourceID)
		assert.Equal(t, "111-cn-hangzhou", refs[0].ReferencedCloudCertID)
		assert.Equal(t, "aliyun", refs[0].Cloud)
		assert.Equal(t, CertProductNLB, refs[0].Product)
		assert.Equal(t, "aliyun-main", refs[0].AccountKey)
		assert.Equal(t, "222-cn-hangzhou", refs[1].ReferencedCloudCertID)
	})

	t.Run("限流错误映射哨兵", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		adapter.newNlbClient = func(creds *domain.CloudAccount, region string) (nlbCertAPI, error) {
			return &fakeNlbClient{listErr: errFakeThrottling}, nil
		}
		_, err := adapter.ListReferences(context.Background(), testCertCreds(), CertProductNLB)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCloudRateLimited)
	})
}

func TestCertAdapterNLBBindResource(t *testing.T) {
	t.Run("替换默认证书保留扩展证书", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeNlbClient{
			listeners: []nlb.ListenerInfo{
				{ListenerId: "lsn-tls-1", ListenerProtocol: "TLS", CertificateIds: []string{"old-cn-hangzhou", "ext-cn-hangzhou"}},
			},
		}
		adapter.newNlbClient = func(creds *domain.CloudAccount, region string) (nlbCertAPI, error) {
			if region == "cn-shanghai" {
				return &fakeNlbClient{}, nil
			}
			return fake, nil
		}

		err := adapter.BindResource(context.Background(), testCertCreds(), CertProductNLB, "lsn-tls-1", "new-cn-hangzhou")
		require.NoError(t, err)

		require.Len(t, fake.updateReqs, 1)
		req := fake.updateReqs[0]
		assert.Equal(t, "lsn-tls-1", req.ListenerId)
		require.NotNil(t, req.CertificateIds)
		require.Len(t, *req.CertificateIds, 2)
		assert.Equal(t, "new-cn-hangzhou", (*req.CertificateIds)[0]) // 默认位换新
		assert.Equal(t, "ext-cn-hangzhou", (*req.CertificateIds)[1]) // 扩展保留
	})

	t.Run("非TLS监听显式报错", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeNlbClient{
			listeners: []nlb.ListenerInfo{
				{ListenerId: "lsn-tcp-1", ListenerProtocol: "TCP"},
			},
		}
		adapter.newNlbClient = func(creds *domain.CloudAccount, region string) (nlbCertAPI, error) { return fake, nil }

		err := adapter.BindResource(context.Background(), testCertCreds(), CertProductNLB, "lsn-tcp-1", "new")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TLS")
	})

	t.Run("更新限流映射哨兵", func(t *testing.T) {
		adapter := newTestCertAdapter(t)
		fake := &fakeNlbClient{
			listeners: []nlb.ListenerInfo{
				{ListenerId: "lsn-tls-1", ListenerProtocol: "TLS", CertificateIds: []string{"old-cn-hangzhou"}},
			},
			updateErr: errFakeThrottling,
		}
		adapter.newNlbClient = func(creds *domain.CloudAccount, region string) (nlbCertAPI, error) { return fake, nil }

		err := adapter.BindResource(context.Background(), testCertCreds(), CertProductNLB, "lsn-tls-1", "new-cn-hangzhou")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCloudRateLimited)
	})
}
