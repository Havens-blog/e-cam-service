// Package aliyun 阿里云日志查询 provider(Phase 1)。
//
// Phase 0 实测(source-status.md):阿里侧全部日志已汇聚 SLS,无需碰
// Kafka/日志文件包;每账号 8 类源按固定 catalog 查询:
//   - SLB:cn-shenzhen/jlc-lb-log(ALB 实例流 + 聚合流)、eu-central-1/jlc-prod-overseas-log(海外)
//   - WAF:eu-central-1 wafnew-project(WAF3.0)、cn-shenzhen 云安全中心渠道 wafng-logstore、
//     eu-central-1 Akamai 自采(jlc-prod-akamai-waf-log)
//   - CDN:cn-shenzhen dcdn-edge-rtlog-*(DCDN 边缘实时)、jlc-prod-cdn-log-monitor
//     (CDN 实时投递,与 DCDN 同构)、jlc-prod-cdn-log(离线转存,独立 PascalCase
//     schema,域名在 RequestURL)、eu-central-1 Akamai 自采(jlc-prod-akamai-cdn-log)
package aliyun

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/gotomicro/ego/core/elog"
)

func init() {
	logquery.RegisterProvider(domain.CloudProviderAliyun, logquery.LogTypeSLB, newProvider(logquery.LogTypeSLB))
	logquery.RegisterProvider(domain.CloudProviderAliyun, logquery.LogTypeWAF, newProvider(logquery.LogTypeWAF))
	logquery.RegisterProvider(domain.CloudProviderAliyun, logquery.LogTypeCDN, newProvider(logquery.LogTypeCDN))
}

// slsSource SLS 源 catalog 条目:project(含 logstore 为动态枚举)。
type slsSource struct {
	region   string
	project  string
	logType  logquery.LogType
	kind     mapperKind
	logstore string               // 空=ListLogStores 动态枚举(过滤 -metrics 等内部流)
	name     string               // 展示名(logstore 动态枚举时的前缀描述)
	fixed    []logquery.LogSource // 固定源(不枚举,直接给)
	note     string
	// mixedDomains 单 logstore 混装全部域名(需要按域名扇出查询)。
	// 仅 DCDN 边缘实时与 Akamai 自采两类;域名转存类 logstore 即单域名资源,
	// 整流查询即可。
	mixedDomains bool
}

// catalog 阿里侧 SLS 日志源清单(Phase 0 实测;project 名账号内稳定)。
var catalog = []slsSource{
	// ---- SLB(ALB 访问日志,topic=alb_layer7_access_log) ----
	{region: "cn-shenzhen", project: "jlc-lb-log", logType: logquery.LogTypeSLB, kind: kindALB,
		logstore: "", name: "ALB", note: "国内 ALB/CLB 访问日志(实例流+聚合)"},
	{region: "eu-central-1", project: "jlc-prod-overseas-log", logType: logquery.LogTypeSLB, kind: kindALB,
		logstore: "", name: "海外 ALB", note: "海外 ALB 访问日志"},
	// ---- WAF ----
	{region: "eu-central-1", project: "wafnew-project-1210557380197478-eu-central-1", logType: logquery.LogTypeWAF, kind: kindWAF3,
		logstore: "wafnew-logstore", name: "WAF3.0(海外)", note: "eu-central-1 WAF 日志库"},
	{region: "cn-shenzhen", project: "aliyun-cloudsiem-channel-1210557380197478-cn-shenzhen", logType: logquery.LogTypeWAF, kind: kindWAF3,
		logstore: "wafng-logstore", name: "WAF3.0(国内渠道)", note: "云安全中心渠道,与 wafnew 同 schema"},
	// Akamai 自采(Phase 0 实测两 store 均在 eu-central-1,误写 cn-shenzhen 会静默查空)
	{region: "eu-central-1", project: "jlc-prod-akamai-cdnwaf-log", logType: logquery.LogTypeWAF, kind: kindAkamaiWAF,
		logstore: "jlc-prod-akamai-waf-log", name: "Akamai WAF(自采)", note: "Akamai WAF 日志自采入库(CEF 展开)"},
	// ---- CDN ----
	{region: "eu-central-1", project: "jlc-prod-akamai-cdnwaf-log", logType: logquery.LogTypeCDN, kind: kindAkamaiCDN,
		logstore: "jlc-prod-akamai-cdn-log", name: "Akamai CDN(自采)", note: "Akamai CDN 日志自采入库", mixedDomains: true},
	{region: "cn-shenzhen", project: "dcdn-edge-rtlog-cn-42d9825f", logType: logquery.LogTypeCDN, kind: kindDCDN,
		logstore: "dcdn-edge-rtlog", name: "DCDN 边缘实时", note: "DCDN 实时日志", mixedDomains: true},
	// CDN(非 DCDN)实时投递(监控):schema 与 DCDN 边缘实时完全同构,复用 mapper 与域名扇出
	{region: "cn-shenzhen", project: "jlc-prod-cdn-log-monitor", logType: logquery.LogTypeCDN, kind: kindDCDN,
		logstore: "jlc-prod-cdn-log-monitor", name: "CDN 实时(监控投递)", note: "CDN 实时日志投递(监控),schema 同 DCDN", mixedDomains: true},
	// CDN 离线转存:logstore 即转存任务(单站点,PascalCase schema),动态枚举
	// (当前 jlcfa-his=www.jlcfa.com + api_forface3d_com;metrics/internal 流被过滤)
	{region: "cn-shenzhen", project: "jlc-prod-cdn-log", logType: logquery.LogTypeCDN, kind: kindCDNOffline,
		logstore: "", name: "CDN 离线转存", note: "CDN 离线日志转存(按转存任务)"},
}

// provider 阿里云 SLS 日志 provider(单账号)。
type provider struct {
	logType   logquery.LogType
	account   *domain.CloudAccount
	logger    *elog.Component
	clientsMu sync.Mutex
	clients   map[string]*sls.Client // region -> SLS client
}

// newProvider 按日志类型构造(SLS endpoint 随 region 切换,client 惰性按 region 建)。
func newProvider(logType logquery.LogType) logquery.ProviderCreator {
	return func(account *domain.CloudAccount) (logquery.LogProvider, error) {
		if account.AccessKeyID == "" || account.AccessKeySecret == "" {
			return nil, fmt.Errorf("aliyun logquery: account %d missing credentials", account.ID)
		}
		return &provider{
			logType: logType,
			account: account,
			logger:  elog.DefaultLogger,
		}, nil
	}
}

// clients 按 region 缓存 SLS client(endpoint = <region>.log.aliyuncs.com)。
func (p *provider) clientFor(region string) *sls.Client {
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	if p.clients == nil {
		p.clients = make(map[string]*sls.Client)
	}
	if c, ok := p.clients[region]; ok {
		return c
	}
	c := &sls.Client{
		Endpoint:        fmt.Sprintf("%s.log.aliyuncs.com", region),
		AccessKeyID:     p.account.AccessKeyID,
		AccessKeySecret: p.account.AccessKeySecret,
		RequestTimeOut:  20 * time.Second,
		RetryTimeOut:    40 * time.Second,
	}
	p.clients[region] = c
	return c
}

// Cloud 实现 LogProvider。
func (p *provider) Cloud() domain.CloudProvider { return domain.CloudProviderAliyun }

// LogType 实现 LogProvider。
func (p *provider) LogType() logquery.LogType { return p.logType }

// ListLogSources 枚举该账号该类型日志源:SLS ListLogStores + catalog 元数据。
func (p *provider) ListLogSources(ctx context.Context, account *domain.CloudAccount) ([]logquery.LogSource, error) {
	var out []logquery.LogSource
	for _, src := range catalog {
		if src.logType != p.logType {
			continue
		}
		if src.logstore != "" {
			s := p.toSource(src, src.logstore, true)
			if src.mixedDomains {
				s.Note = src.note + " · 全部域名(整源查询)"
				out = append(out, s)
				// 混装源的选择粒度是域名:探查活跃域名生成域名级子源
				out = append(out, p.domainSources(ctx, src)...)
				continue
			}
			out = append(out, s)
			continue
		}
		// 动态枚举 logstore(过滤 metrics/diagnostic 内部流)
		stores, err := p.clientFor(src.region).ListLogStore(src.project)
		if err != nil {
			// 枚举失败不阻塞其他源(联邦隔离原则)
			p.logger.Warn("[logquery-aliyun] list logstores failed",
				elog.String("project", src.project), elog.FieldErr(err))
			continue
		}
		for _, ls := range stores {
			if strings.HasSuffix(ls, "-metrics") || strings.HasSuffix(ls, "-metrics-result") ||
				ls == "internal-ml-log" || strings.HasPrefix(ls, "internal-") {
				continue
			}
			out = append(out, p.toSource(src, ls, true))
		}
	}
	return out, nil
}

// toSource catalog 条目 -> LogSource。
func (p *provider) toSource(src slsSource, logstore string, enabled bool) logquery.LogSource {
	return logquery.LogSource{
		Cloud:       domain.CloudProviderAliyun,
		AccountID:   fmt.Sprintf("%d", p.account.ID),
		AccountName: p.account.Name,
		Region:      src.region,
		LogType:     src.logType,
		ResourceID:  logstore,
		Name:        src.name + " / " + logstore,
		Enabled:     enabled,
		Note:        src.note,
	}
}

// domainSources 混装源活跃域名枚举:近 24h 小样本探查(100 条,热度序),
// 每个域名生成一个域名级子源(ResourceID=域名)。用户在下拉里选域名后
// resources 传域名,Search 的域名扇出(fetchCDNByDomains)按其过滤。
// 探查失败静默降级——只返回 logstore 级条目,不阻塞其余源枚举。
func (p *provider) domainSources(ctx context.Context, src slsSource) []logquery.LogSource {
	now := time.Now().UnixMilli()
	probe, err := p.probeDomains(ctx, src, src.logstore, logquery.SearchParams{
		StartTime: now - 24*3600_000,
		EndTime:   now,
	}, 100)
	if err != nil {
		p.logger.Warn("[logquery-aliyun] domain probe failed",
			elog.String("project", src.project), elog.String("logstore", src.logstore),
			elog.FieldErr(err))
		return nil
	}
	domains := domainNamesFromSample(src.kind, probe)
	out := make([]logquery.LogSource, 0, len(domains))
	for _, d := range domains {
		s := p.toSource(src, d, true)
		s.Note = "活跃域名(近24小时)"
		out = append(out, s)
	}
	return out
}

// Search 查询窗口内日志并映射统一模型:逐 catalog 查询,单源失败跳过并记日志,
// 结果按时间倒序(联邦编排层再做跨源归并)。
func (p *provider) Search(ctx context.Context, account *domain.CloudAccount, params logquery.SearchParams) ([]logquery.LogEntry, error) {
	if params.EndTime <= params.StartTime {
		return nil, fmt.Errorf("aliyun logquery: invalid time window")
	}
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100 // SLS GetLogs 单次 Lines<=100,分页到 limit
	}
	resourceFilter := make(map[string]bool, len(params.Resources))
	for _, r := range params.Resources {
		resourceFilter[r] = true
	}
	regionFilter := make(map[string]bool, len(params.Regions))
	for _, r := range params.Regions {
		regionFilter[r] = true
	}

	// ---- 收集 fetch 目标(catalog x logstore) ----
	type fetchTarget struct {
		src      slsSource
		logstore string
	}
	var targets []fetchTarget
	for _, src := range catalog {
		if src.logType != p.logType {
			continue
		}
		if len(regionFilter) > 0 && !regionFilter[src.region] {
			continue
		}
		stores := []string{src.logstore}
		if src.logstore == "" {
			ls, err := p.clientFor(src.region).ListLogStore(src.project)
			if err != nil {
				p.logger.Warn("[logquery-aliyun] list logstores failed",
					elog.String("project", src.project), elog.FieldErr(err))
				continue
			}
			stores = filterInternalStores(ls)
		}
		for _, ls := range stores {
			// mixedDomains 源的资源语义是"域名"(扇出内部过滤),logstore 名
			// 仅作整体开关;此处不按 logstore 过滤(否则选域名时混装源被整流跳过)
			if !src.mixedDomains && len(resourceFilter) > 0 &&
				!resourceFilter[ls] && !resourceFilter[src.project] {
				continue
			}
			targets = append(targets, fetchTarget{src: src, logstore: ls})
		}
	}

	// ---- logstore 级并发拉取(8 并发上限,单源失败隔离) ----
	// CDN 类(DCDN/Akamai)单 logstore 混装全部域名:先小样本探查活跃域名,
	// 再按域名扇出并发查询(每域名各自凑配额,热点域名不挤占长尾);
	// 其余类(ALB/WAF)单流本身就是单源,直接整流查询。
	results := make([][]map[string]string, len(targets))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, tgt := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var logs []map[string]string
			if tgt.src.mixedDomains {
				logs = p.fetchCDNByDomains(ctx, tgt.src, tgt.logstore, params, limit)
			} else {
				var err error
				logs, err = p.fetchLogs(ctx, tgt.src, tgt.logstore, params, limit)
				if err != nil {
					p.logger.Warn("[logquery-aliyun] fetch logs failed",
						elog.String("project", tgt.src.project), elog.String("logstore", tgt.logstore),
						elog.FieldErr(err))
					return
				}
			}
			results[i] = logs
		}()
	}
	wg.Wait()

	// ---- 映射(顺序,无锁) ----
	var entries []logquery.LogEntry
	for i, tgt := range targets {
		if len(results[i]) == 0 {
			continue
		}
		meta := logquery.LogMeta{
			Cloud:       domain.CloudProviderAliyun,
			AccountID:   fmt.Sprintf("%d", p.account.ID),
			AccountName: p.account.Name,
			Region:      tgt.src.region,
			ResourceID:  tgt.logstore,
			Source:      tgt.src.project + "/" + tgt.logstore,
		}
		for _, raw := range results[i] {
			if e := mapEntry(tgt.src.kind, meta, raw); e != nil {
				entries = append(entries, e)
			}
		}
	}
	// 时间倒序;总硬顶与联邦级一致(ADR D4:limit 为**每日志源**上限,
	// 多源归并后按时间排序,全局 1000 防响应过大;不做 provider 级总截断,
	// 否则热点源会吃掉全部配额,其他源(域名)被挤出局)
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GetTimestamp() > entries[j].GetTimestamp()
	})
	const providerCap = 1000
	if len(entries) > providerCap {
		entries = entries[:providerCap]
	}
	return entries, nil
}

// fetchLogs 单 logstore 分页拉取(SLS 单次 Lines<=100)。
func (p *provider) fetchLogs(ctx context.Context, src slsSource, logstore string, params logquery.SearchParams, limit int) ([]map[string]string, error) {
	client := p.clientFor(src.region)
	// SLS 查询窗口为秒
	from := params.StartTime / 1000
	to := params.EndTime / 1000
	query := buildQuery(src.kind, params.Query)
	var all []map[string]string
	offset := int64(0)
	const pageSize = 100
	for int64(len(all)) < int64(limit) {
		resp, err := client.GetLogsV2(src.project, logstore, &sls.GetLogRequest{
			From:    from,
			To:      to,
			Query:   query,
			Lines:   pageSize,
			Offset:  offset,
			Reverse: true, // 时间倒序
		})
		if err != nil {
			return nil, fmt.Errorf("sls get_logs %s/%s: %w", src.project, logstore, err)
		}
		if len(resp.Logs) == 0 {
			break
		}
		all = append(all, resp.Logs...)
		if len(resp.Logs) < pageSize || !resp.IsComplete() {
			break
		}
		offset += int64(len(resp.Logs))
	}
	return all, nil
}

// buildQuery 原始查询式 + kind 专属 __topic__ 过滤(服务端过滤:
// 聚合 project 里混有 nacos/app 等非 LB 日志流,topic 是 ALB/WAF 访问日志的
// 稳定判据;无 topic 的源(DCDN rtlog/Akamai)不加过滤)。
func buildQuery(kind mapperKind, userQuery string) string {
	var topic string
	switch kind {
	case kindALB:
		topic = "__topic__:alb_layer7_access_log"
	case kindWAF3:
		topic = "__topic__:waf_access_log"
	}
	q := strings.TrimSpace(userQuery)
	if q == "" || q == "*" {
		if topic == "" {
			return "*"
		}
		return topic
	}
	if topic == "" {
		return q
	}
	return q + " and " + topic
}

// ---------------------------------------------------------------------
// CDN 按域名扇出(单 logstore 混装全部域名,热点域名会吃掉整窗配额——
// 100 条样本实测单一域名占 51%。按域名拆分并发查询,每域名各自凑配额,
// 长尾域名不被挤出结果)
// ---------------------------------------------------------------------

// domainField 按 kind 返回域名原始字段名(单测覆盖)。
func domainField(kind mapperKind) string {
	switch kind {
	case kindDCDN:
		return "domain"
	case kindAkamaiCDN:
		return "reqHost"
	default:
		return ""
	}
}

// splitByDomain 把探查样本按域名分组(保序:域名首现顺序即热度序)。
func splitByDomain(kind mapperKind, logs []map[string]string) map[string][]map[string]string {
	field := domainField(kind)
	if field == "" {
		return nil
	}
	grouped := make(map[string][]map[string]string)
	for _, l := range logs {
		d := l[field]
		if d == "" {
			continue
		}
		grouped[d] = append(grouped[d], l)
	}
	return grouped
}

// domainNamesFromSample 从探查样本提取域名清单(热度序:样本数降序)。
// ListLogSources 域名枚举与 Search 域名扇出共用此排序。
func domainNamesFromSample(kind mapperKind, probe []map[string]string) []string {
	grouped := splitByDomain(kind, probe)
	domains := make([]string, 0, len(grouped))
	for d := range grouped {
		domains = append(domains, d)
	}
	sort.Slice(domains, func(i, j int) bool {
		return len(grouped[domains[i]]) > len(grouped[domains[j]])
	})
	return domains
}

// probeDomains logstore 探查:小样本拉取活跃域名集合(probeLimit 条,
// 兼作首批数据;探查失败返回 nil——扇出退化为整 store 查询)。
func (p *provider) probeDomains(ctx context.Context, src slsSource, logstore string, params logquery.SearchParams, probeLimit int) ([]map[string]string, error) {
	return p.fetchLogs(ctx, src, logstore, params, probeLimit)
}

// fetchPerDomain 单域名查询:domain 过滤 + 用户检索式,凑满 limit 即停。
func (p *provider) fetchPerDomain(ctx context.Context, src slsSource, logstore, domain string, params logquery.SearchParams, limit int) ([]map[string]string, error) {
	client := p.clientFor(src.region)
	from := params.StartTime / 1000
	to := params.EndTime / 1000
	// 域名过滤拼接用户检索式(SLS 查询语法 and 连接)
	q := strings.TrimSpace(params.Query)
	domainTerm := domainField(src.kind) + ": " + domain
	if q == "" || q == "*" {
		q = domainTerm
	} else {
		q = "(" + q + ") and " + domainTerm
	}
	var all []map[string]string
	offset := int64(0)
	const pageSize = 100
	for int64(len(all)) < int64(limit) {
		resp, err := client.GetLogsV2(src.project, logstore, &sls.GetLogRequest{
			From:    from,
			To:      to,
			Query:   q,
			Lines:   pageSize,
			Offset:  offset,
			Reverse: true,
		})
		if err != nil {
			return nil, fmt.Errorf("sls get_logs %s/%s domain=%s: %w", src.project, logstore, domain, err)
		}
		if len(resp.Logs) == 0 {
			break
		}
		all = append(all, resp.Logs...)
		if len(resp.Logs) < pageSize || !resp.IsComplete() {
			break
		}
		offset += int64(len(resp.Logs))
	}
	return all, nil
}

// fetchCDNByDomains CDN 类按域名扇出:
//  1. 探查(probeLimit 条,兼作首批数据)-> 域名集合(热度序);
//  2. 用户指定 Resources 时按其过滤域名(同时作为唯一域名清单);
//  3. 逐域名并发(8 上限)查询,每域名各自凑满 limit;
//  4. 探查失败降级为整 store 查询(总截断到 limit,退回旧行为)。
func (p *provider) fetchCDNByDomains(ctx context.Context, src slsSource, logstore string, params logquery.SearchParams, limit int) []map[string]string {
	const probeLimit = 100
	probe, err := p.probeDomains(ctx, src, logstore, params, probeLimit)
	if err != nil {
		p.logger.Warn("[logquery-aliyun] cdn probe failed, fallback to whole-store query",
			elog.String("project", src.project), elog.String("logstore", logstore),
			elog.FieldErr(err))
		logs, ferr := p.fetchLogs(ctx, src, logstore, params, limit)
		if ferr != nil {
			p.logger.Warn("[logquery-aliyun] fetch logs failed",
				elog.String("project", src.project), elog.String("logstore", logstore),
				elog.FieldErr(ferr))
			return nil
		}
		return logs
	}

	// 域名清单 = 探查结果分组(保热度序);探查样本直接计入对应域名配额
	grouped := splitByDomain(src.kind, probe)
	if len(grouped) == 0 {
		return probe
	}
	domains := domainNamesFromSample(src.kind, probe)

	// 用户指定资源过滤:域名清单收敛为其交集。
	// Resources 可能是域名(混装源的展示/选择粒度)或 logstore 名(选中整个
	// 混装源)——logstore 名等于本源自身时视为"不过滤域名,查全源"。
	if len(params.Resources) > 0 {
		wholeSource := false
		allowed := make(map[string]bool, len(params.Resources))
		for _, r := range params.Resources {
			if r == logstore || r == src.project {
				wholeSource = true
			}
			allowed[r] = true
		}
		if wholeSource {
			allowed = nil // 整源:不限域名
		}
		if allowed != nil {
			filtered := domains[:0]
			for _, d := range domains {
				if allowed[d] {
					filtered = append(filtered, d)
				}
			}
			if len(filtered) == 0 {
				// 指定资源不在探查样本中(低频域名):按指定清单全量查
				for _, r := range params.Resources {
					filtered = append(filtered, r)
				}
			}
			domains = filtered
		}
	}

	// 逐域名并发,每域名配额 = limit - 探查已得(探查样本均分抵扣)
	type domainResult struct {
		logs []map[string]string
	}
	perDomainQuota := limit / len(domains)
	if perDomainQuota < 1 {
		perDomainQuota = 1
	}
	results := make([]domainResult, len(domains))
	sem2 := make(chan struct{}, 8)
	var wg2 sync.WaitGroup
	for i, d := range domains {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			sem2 <- struct{}{}
			defer func() { <-sem2 }()
			quota := perDomainQuota
			if got := len(grouped[d]); got < quota {
				quota -= got // 探查已贡献部分配额
			} else {
				results[i] = domainResult{logs: grouped[d]}
				return // 探查样本已凑满该域名配额
			}
			logs, err := p.fetchPerDomain(ctx, src, logstore, d, params, quota)
			if err != nil {
				p.logger.Warn("[logquery-aliyun] per-domain fetch failed",
					elog.String("domain", d), elog.FieldErr(err))
				// 单域名失败隔离:退回探查样本保底
			}
			// 合并探查样本 + 域名查询(查询从最新开始,探查样本可能重叠,
			// 按时间戳去重由映射层后排序兜底;此处直接拼接)
			merged := append(append([]map[string]string{}, grouped[d]...), logs...)
			results[i] = domainResult{logs: merged}
		}()
	}
	wg2.Wait()

	var out []map[string]string
	for i := range domains {
		out = append(out, results[i].logs...)
	}
	return out
}

// filterInternalStores 过滤 SLS 内部流(metrics/diagnostic/ml)。
func filterInternalStores(stores []string) []string {
	out := make([]string, 0, len(stores))
	for _, ls := range stores {
		if strings.HasSuffix(ls, "-metrics") || strings.HasSuffix(ls, "-metrics-result") ||
			strings.HasPrefix(ls, "internal-") {
			continue
		}
		out = append(out, ls)
	}
	return out
}
