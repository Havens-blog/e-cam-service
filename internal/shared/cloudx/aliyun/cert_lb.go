package aliyun

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/alb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/nlb"
	"github.com/gotomicro/ego/core/elog"
)

// ALB/NLB 证书产品实现（负载均衡监听维度）。
// 发现：ListListeners（NextToken 分页）遍历账号各地域——
//   ALB 需逐监听 ListListenerCertificates（CertificateType=Server）取证书清单；
//   NLB 监听内联 CertificateIds，无需二次查询。
// 绑定：监听 ID（lsn-*）全局唯一但需定位地域（逐地域 ListListeners 按 ListenerIds 过滤），
//   读当前证书清单后 UpdateListenerAttribute 回写——新证书置默认位，扩展证书保留（替换默认证书语义）。

// listALBReferences 按地域遍历 ALB 监听及其服务器证书
func (a *CertAdapter) listALBReferences(ctx context.Context, creds *domain.CloudAccount) ([]CloudCertRef, error) {
	if creds == nil {
		return nil, fmt.Errorf("aliyun alb cert list: nil creds")
	}
	accountKey := creds.Name
	var refs []CloudCertRef
	for _, region := range credsRegions(creds) {
		client, err := a.newAlbClient(creds, region)
		if err != nil {
			return nil, err
		}
		nextToken := ""
		for {
			if err := a.waitRateLimit(ctx); err != nil {
				return nil, err
			}
			request := alb.CreateListListenersRequest()
			request.Scheme = "https"
			request.MaxResults = requests.NewInteger(a.certPageSize())
			request.NextToken = nextToken
			response, err := client.ListListeners(request)
			if err != nil {
				return nil, wrapCertCloudErr(CertProductALB, err)
			}
			for _, listener := range response.Listeners {
				// HTTP 监听无服务器证书
				if listener.ListenerProtocol == "HTTP" {
					continue
				}
				certs, err := a.listALBListenerCerts(ctx, client, listener.ListenerId)
				if err != nil {
					// 单监听证书列举失败跳过继续，避免整体扫描失败
					a.logger.Warn("获取ALB监听证书失败，跳过",
						elog.String("listener", listener.ListenerId),
						elog.FieldErr(err))
					continue
				}
				for _, cert := range certs {
					refs = append(refs, CloudCertRef{
						Cloud:                 "aliyun",
						Product:               CertProductALB,
						ResourceID:            listener.ListenerId,
						ReferencedCloudCertID: cert.CertificateId,
						AccountKey:            accountKey,
					})
				}
			}
			if response.NextToken == "" {
				break
			}
			nextToken = response.NextToken
		}
	}

	a.logger.Info("获取阿里云ALB证书引用成功",
		elog.String("account", accountKey),
		elog.Int("count", len(refs)))
	return refs, nil
}

// listALBListenerCerts 遍历单个 ALB 监听的服务器证书（NextToken 分页）
func (a *CertAdapter) listALBListenerCerts(ctx context.Context, client albCertAPI, listenerID string) ([]alb.CertificateModel, error) {
	var certs []alb.CertificateModel
	nextToken := ""
	for {
		if err := a.waitRateLimit(ctx); err != nil {
			return nil, err
		}
		request := alb.CreateListListenerCertificatesRequest()
		request.Scheme = "https"
		request.ListenerId = listenerID
		request.CertificateType = "Server"
		request.MaxResults = requests.NewInteger(a.certPageSize())
		request.NextToken = nextToken
		response, err := client.ListListenerCertificates(request)
		if err != nil {
			return nil, wrapCertCloudErr(CertProductALB, err)
		}
		certs = append(certs, response.Certificates...)
		if response.NextToken == "" {
			break
		}
		nextToken = response.NextToken
	}
	return certs, nil
}

// bindALB 将云证书绑定为 ALB 监听默认服务器证书（扩展证书保留）
func (a *CertAdapter) bindALB(ctx context.Context, creds *domain.CloudAccount, listenerID, cloudCertID string) error {
	if creds == nil {
		return fmt.Errorf("aliyun alb cert bind: nil creds")
	}
	region, listener, client, err := a.findALBListener(creds, listenerID)
	if err != nil {
		return err
	}
	if listener.ListenerProtocol == "HTTP" {
		return fmt.Errorf("aliyun alb cert bind: listener %s is HTTP (no server certificate)", listenerID)
	}

	currentCerts, err := a.listALBListenerCerts(ctx, client, listenerID)
	if err != nil {
		return err
	}
	// 新证书置默认位；原默认证书移除，扩展（非默认）证书保留
	newCerts := []alb.UpdateListenerAttributeCertificates{{CertificateId: cloudCertID}}
	for _, cert := range currentCerts {
		if !cert.IsDefault && cert.CertificateId != cloudCertID {
			newCerts = append(newCerts, alb.UpdateListenerAttributeCertificates{CertificateId: cert.CertificateId})
		}
	}

	if err := a.waitRateLimit(ctx); err != nil {
		return err
	}
	request := alb.CreateUpdateListenerAttributeRequest()
	request.Scheme = "https"
	request.ListenerId = listenerID
	request.Certificates = &newCerts
	if _, err := client.UpdateListenerAttribute(request); err != nil {
		return wrapCertCloudErr(CertProductALB, err)
	}
	a.logger.Info("阿里云ALB证书绑定成功",
		elog.String("region", region),
		elog.String("listener", listenerID),
		elog.String("cloud_cert_id", cloudCertID))
	return nil
}

// findALBListener 逐地域定位监听所在地域与实例（listenerId 全局唯一）
func (a *CertAdapter) findALBListener(creds *domain.CloudAccount, listenerID string) (string, alb.Listener, albCertAPI, error) {
	for _, region := range credsRegions(creds) {
		client, err := a.newAlbClient(creds, region)
		if err != nil {
			return "", alb.Listener{}, nil, err
		}
		request := alb.CreateListListenersRequest()
		request.Scheme = "https"
		request.ListenerIds = &[]string{listenerID}
		request.MaxResults = requests.NewInteger(1)
		response, err := client.ListListeners(request)
		if err != nil {
			return "", alb.Listener{}, nil, wrapCertCloudErr(CertProductALB, err)
		}
		for _, listener := range response.Listeners {
			if listener.ListenerId == listenerID {
				return region, listener, client, nil
			}
		}
	}
	return "", alb.Listener{}, nil, fmt.Errorf("aliyun alb cert bind: listener %s not found in account regions", listenerID)
}

// listNLBReferences 按地域遍历 NLB 监听及其证书 ID 清单
func (a *CertAdapter) listNLBReferences(ctx context.Context, creds *domain.CloudAccount) ([]CloudCertRef, error) {
	if creds == nil {
		return nil, fmt.Errorf("aliyun nlb cert list: nil creds")
	}
	accountKey := creds.Name
	var refs []CloudCertRef
	for _, region := range credsRegions(creds) {
		client, err := a.newNlbClient(creds, region)
		if err != nil {
			return nil, err
		}
		nextToken := ""
		for {
			if err := a.waitRateLimit(ctx); err != nil {
				return nil, err
			}
			request := nlb.CreateListListenersRequest()
			request.Scheme = "https"
			request.MaxResults = requests.NewInteger(a.certPageSize())
			request.NextToken = nextToken
			response, err := client.ListListeners(request)
			if err != nil {
				return nil, wrapCertCloudErr(CertProductNLB, err)
			}
			// NLB 以响应体 Success 表达业务成败（HTTP 200 也可能携带失败）
			if !response.Success {
				return nil, fmt.Errorf("aliyun nlb list listeners failed: code=%s message=%s", response.Code, response.Message)
			}
			for _, listener := range response.Listeners {
				for _, certID := range listener.CertificateIds {
					refs = append(refs, CloudCertRef{
						Cloud:                 "aliyun",
						Product:               CertProductNLB,
						ResourceID:            listener.ListenerId,
						ReferencedCloudCertID: certID,
						AccountKey:            accountKey,
					})
				}
			}
			if response.NextToken == "" {
				break
			}
			nextToken = response.NextToken
		}
	}

	a.logger.Info("获取阿里云NLB证书引用成功",
		elog.String("account", accountKey),
		elog.Int("count", len(refs)))
	return refs, nil
}

// bindNLB 将云证书绑定为 NLB TLS 监听默认证书（扩展证书保留）
func (a *CertAdapter) bindNLB(ctx context.Context, creds *domain.CloudAccount, listenerID, cloudCertID string) error {
	if creds == nil {
		return fmt.Errorf("aliyun nlb cert bind: nil creds")
	}
	region, listener, client, err := a.findNlbListener(creds, listenerID)
	if err != nil {
		return err
	}
	if listener.ListenerProtocol != "TLS" {
		return fmt.Errorf("aliyun nlb cert bind: listener %s is %s (only TLS listeners hold certificates)", listenerID, listener.ListenerProtocol)
	}

	// 新证书置默认位（首位），原默认移除，扩展证书保留
	newCertIDs := []string{cloudCertID}
	current := listener.CertificateIds
	for i, certID := range current {
		if i == 0 || certID == cloudCertID {
			continue
		}
		newCertIDs = append(newCertIDs, certID)
	}

	if err := a.waitRateLimit(ctx); err != nil {
		return err
	}
	request := nlb.CreateUpdateListenerAttributeRequest()
	request.Scheme = "https"
	request.ListenerId = listenerID
	request.CertificateIds = &newCertIDs
	response, err := client.UpdateListenerAttribute(request)
	if err != nil {
		return wrapCertCloudErr(CertProductNLB, err)
	}
	if !response.Success {
		return fmt.Errorf("aliyun nlb update listener failed: code=%s message=%s", response.Code, response.Message)
	}
	a.logger.Info("阿里云NLB证书绑定成功",
		elog.String("region", region),
		elog.String("listener", listenerID),
		elog.String("cloud_cert_id", cloudCertID))
	return nil
}

// findNlbListener 逐地域定位 NLB 监听所在地域与实例
func (a *CertAdapter) findNlbListener(creds *domain.CloudAccount, listenerID string) (string, nlb.ListenerInfo, nlbCertAPI, error) {
	for _, region := range credsRegions(creds) {
		client, err := a.newNlbClient(creds, region)
		if err != nil {
			return "", nlb.ListenerInfo{}, nil, err
		}
		request := nlb.CreateListListenersRequest()
		request.Scheme = "https"
		request.ListenerIds = &[]string{listenerID}
		request.MaxResults = requests.NewInteger(1)
		response, err := client.ListListeners(request)
		if err != nil {
			return "", nlb.ListenerInfo{}, nil, wrapCertCloudErr(CertProductNLB, err)
		}
		if !response.Success {
			return "", nlb.ListenerInfo{}, nil, fmt.Errorf("aliyun nlb list listeners failed: code=%s message=%s", response.Code, response.Message)
		}
		for _, listener := range response.Listeners {
			if listener.ListenerId == listenerID {
				return region, listener, client, nil
			}
		}
	}
	return "", nlb.ListenerInfo{}, nil, fmt.Errorf("aliyun nlb cert bind: listener %s not found in account regions", listenerID)
}
