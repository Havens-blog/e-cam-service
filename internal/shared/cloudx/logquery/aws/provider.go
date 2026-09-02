package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gotomicro/ego/core/elog"
)

func init() {
	logquery.RegisterProvider(domain.CloudProviderAWS, logquery.LogTypeWAF, newProvider(logquery.LogTypeWAF))
	logquery.RegisterProvider(domain.CloudProviderAWS, logquery.LogTypeCDN, newProvider(logquery.LogTypeCDN))
}

// 投递特征:Firehose 分钟级、CloudFront ~1 小时;对象 LastModified 早于窗口
// 起点也可能含窗口内日志,向前多看一个投递延迟。
const (
	wafLookback        = 2 * time.Hour
	cloudFrontLookback = 3 * time.Hour
	maxObjectsPerScan  = 60 // 单源单轮最多下载对象数(防大窗口拉爆)
	maxObjectBytes     = 32 << 20
)

// aSource S3 源 catalog 条目。
type aSource struct {
	bucket   string
	prefix   string // 空=桶根(域名目录布局)
	region   string
	kind     string // "waf-json" | "cloudfront-tsv"
	logType  logquery.LogType
	name     string
	note     string
	lookback time.Duration
}

// catalog AWS 侧日志源清单(Phase 0 实测;ALB 访问日志大多未开启,不纳入)。
var catalog = []aSource{
	{
		// WAFv2 CLOUDFRONT scope 主桶(Firehose aws-waf-logs-CL-s3-* 落地)
		bucket: "waf-loghub-clloggingbucket5f34e4eb-hjalewqix1db",
		prefix: "AWSLogs/", region: "us-east-1", kind: "waf-json",
		logType: logquery.LogTypeWAF, name: "AWS WAF",
		note:    "Firehose 投递 JSON(分钟级)", lookback: wafLookback,
	},
	{
		// 第二 Firehose 桶(部分 WebACL 落此)
		bucket: "loghub-clloggingbucket5f34e4eb-asng9spoysri",
		prefix: "AWSLogs/", region: "us-east-1", kind: "waf-json",
		logType: logquery.LogTypeWAF, name: "AWS WAF",
		note:    "Firehose 投递 JSON(分钟级)", lookback: wafLookback,
	},
	{
		// CloudFront 标准日志(按域名前缀分目录)
		bucket: "jlc-prod-cdn-log", prefix: "", region: "eu-west-1", kind: "cloudfront-tsv",
		logType: logquery.LogTypeCDN, name: "CloudFront",
		note:    "标准日志 TSV(~1h 延迟)", lookback: cloudFrontLookback,
	},
}

// provider AWS S3 日志 provider(单账号)。
type provider struct {
	logType logquery.LogType
	account *domain.CloudAccount
	logger  *elog.Component

	mu      sync.Mutex
	clients map[string]*s3.Client // bucket -> client(region 按桶自动探测)
}

func newProvider(logType logquery.LogType) logquery.ProviderCreator {
	return func(account *domain.CloudAccount) (logquery.LogProvider, error) {
		if account.AccessKeyID == "" || account.AccessKeySecret == "" {
			return nil, fmt.Errorf("aws logquery: account %d missing credentials", account.ID)
		}
		return &provider{
			logType: logType,
			account: account,
			logger:  elog.DefaultLogger,
		}, nil
	}
}

// clientFor 按 bucket 缓存 S3 client(region 以 catalog 为 hint,首次使用时
// HeadBucket 探测真实区域并纠正——Go SDK v2 不会像 boto3 自动跟随 301)。
func (p *provider) clientFor(bucket, hintRegion string) (*s3.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.clients == nil {
		p.clients = make(map[string]*s3.Client)
	}
	if c, ok := p.clients[bucket]; ok {
		return c, nil
	}
	c, err := s3Client(p.account, hintRegion)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if real := resolveBucketRegion(ctx, c, bucket); real != "" && real != hintRegion {
		if fixed, ferr := s3Client(p.account, real); ferr == nil {
			p.logger.Info("[logquery-aws] bucket region resolved",
				elog.String("bucket", bucket), elog.String("region", real))
			c = fixed
		}
	}
	p.clients[bucket] = c
	return c, nil
}

// Cloud 实现 LogProvider。
func (p *provider) Cloud() domain.CloudProvider { return domain.CloudProviderAWS }

// LogType 实现 LogProvider。
func (p *provider) LogType() logquery.LogType { return p.logType }

// sourcesOf 该类型全部 catalog 源。
func (p *provider) sourcesOf() []aSource {
	var out []aSource
	for _, s := range catalog {
		if s.logType == p.logType {
			out = append(out, s)
		}
	}
	return out
}

// ListLogSources 枚举日志源:WAF=逐 ACL 前缀下钻;CDN=桶根一级目录(域名)。
func (p *provider) ListLogSources(ctx context.Context, account *domain.CloudAccount) ([]logquery.LogSource, error) {
	var out []logquery.LogSource
	for _, src := range p.sourcesOf() {
		client, err := p.clientFor(src.bucket, src.region)
		if err != nil {
			p.logger.Warn("[logquery-aws] s3 client failed", elog.FieldErr(err))
			continue
		}
		switch src.kind {
		case "waf-json":
			// AWSLogs/<acct>/WAFLogs/cloudfront/<acl>/ -> 深钻 4 级取 ACL
			aclPrefixes, err := walkPrefixes(ctx, client, src.bucket, src.prefix, 4)
			if err != nil {
				p.logger.Warn("[logquery-aws] walk waf prefixes failed",
					elog.String("bucket", src.bucket), elog.FieldErr(err))
				continue
			}
			for _, pref := range aclPrefixes {
				acl := pathBase(strings.TrimSuffix(pref, "/"))
				out = append(out, p.toSource(src, acl, acl, true))
			}
		case "cloudfront-tsv":
			domainPrefixes, err := listCommonPrefixes(ctx, client, src.bucket, "")
			if err != nil {
				p.logger.Warn("[logquery-aws] list domains failed",
					elog.String("bucket", src.bucket), elog.FieldErr(err))
				continue
			}
			for _, pref := range domainPrefixes {
				d := strings.TrimSuffix(pref, "/")
				out = append(out, p.toSource(src, d, d, true))
			}
		}
	}
	return out, nil
}

// toSource catalog+资源 -> LogSource。
func (p *provider) toSource(src aSource, resourceID, name string, enabled bool) logquery.LogSource {
	return logquery.LogSource{
		Cloud:       domain.CloudProviderAWS,
		AccountID:   fmt.Sprintf("%d", p.account.ID),
		AccountName: p.account.Name,
		Region:      src.region,
		LogType:     src.logType,
		ResourceID:  resourceID,
		Name:        src.name + " / " + name,
		Enabled:     enabled,
		Note:        src.note,
	}
}

// Search 窗口内日志:逐源列对象(最新优先)-> 下载解压 -> 按格式解析 ->
// 时间过滤 -> 归并倒序。单源失败隔离。
func (p *provider) Search(ctx context.Context, account *domain.CloudAccount, params logquery.SearchParams) ([]logquery.LogEntry, error) {
	if params.EndTime <= params.StartTime {
		return nil, fmt.Errorf("aws logquery: invalid time window")
	}
	limit := params.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	resourceFilter := map[string]bool{}
	for _, r := range params.Resources {
		resourceFilter[r] = true
	}
	regionFilter := map[string]bool{}
	for _, r := range params.Regions {
		regionFilter[r] = true
	}

	type scanTarget struct {
		src       aSource
		prefix    string // 对象列举前缀(源级或资源级)
		resource  string // 资源标识(ACL/域名;空=整源)
	}
	var targets []scanTarget
	for _, src := range p.sourcesOf() {
		if len(regionFilter) > 0 && !regionFilter[src.region] {
			continue
		}
		switch src.kind {
		case "waf-json":
			client, err := p.clientFor(src.bucket, src.region)
			if err != nil {
				continue
			}
			prefixes, err := walkPrefixes(ctx, client, src.bucket, src.prefix, 4)
			if err != nil {
				p.logger.Warn("[logquery-aws] walk waf prefixes failed",
					elog.String("bucket", src.bucket), elog.FieldErr(err))
				continue
			}
			for _, pref := range prefixes {
				acl := pathBase(strings.TrimSuffix(pref, "/"))
				if len(resourceFilter) > 0 && !resourceFilter[acl] {
					continue
				}
				targets = append(targets, scanTarget{src: src, prefix: pref, resource: acl})
			}
		case "cloudfront-tsv":
			client, err := p.clientFor(src.bucket, src.region)
			if err != nil {
				continue
			}
			prefixes, err := listCommonPrefixes(ctx, client, src.bucket, "")
			if err != nil {
				p.logger.Warn("[logquery-aws] list domains failed",
					elog.String("bucket", src.bucket), elog.FieldErr(err))
				continue
			}
			for _, pref := range prefixes {
				d := strings.TrimSuffix(pref, "/")
				if len(resourceFilter) > 0 && !resourceFilter[d] {
					continue
				}
				targets = append(targets, scanTarget{src: src, prefix: pref, resource: d})
			}
		}
	}

	// ---- 源级并发(8 上限,单源失败隔离) ----
	results := make([][]logquery.LogEntry, len(targets))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, tgt := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			logs, err := p.scanTarget(ctx, tgt.src, tgt.prefix, tgt.resource, params, limit)
			if err != nil {
				p.logger.Warn("[logquery-aws] scan failed",
					elog.String("bucket", tgt.src.bucket), elog.String("prefix", tgt.prefix),
					elog.FieldErr(err))
				return
			}
			results[i] = logs
		}()
	}
	wg.Wait()

	var entries []logquery.LogEntry
	for i := range targets {
		entries = append(entries, results[i]...)
	}
	// limit 为每日志源(ACL/域名)上限(ADR D4);归并后全局 1000 硬顶,
	// 不做 provider 级总截断(防热点域名吃掉全部配额,长尾域名被挤出局)
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GetTimestamp() > entries[j].GetTimestamp()
	})
	const providerCap = 1000
	if len(entries) > providerCap {
		entries = entries[:providerCap]
	}
	return entries, nil
}

// scanTarget 单源扫描:列对象(最新优先)-> 逐对象下载解析,攒够 limit 即停。
func (p *provider) scanTarget(ctx context.Context, src aSource, prefix, resource string, params logquery.SearchParams, limit int) ([]logquery.LogEntry, error) {
	client, err := p.clientFor(src.bucket, src.region)
	if err != nil {
		return nil, err
	}
	// 键内嵌日期的布局(WAF 嵌套目录)按窗口起点跳过历史数据;
	// CloudFront 扁平布局(dist 前缀)无法按日期定位,靠 maxKeys+最新优先容忍
	startAfter := ""
	if src.kind == "waf-json" {
		// <acl>/YYYY/MM/DD/HH:窗口起点(含回看)所在小时,前一小时兜底跨文件边界
		floor := time.UnixMilli(params.StartTime).Add(-src.lookback).Add(-time.Hour)
		startAfter = prefix + s3KeyDateFloor(floor)
	}
	objects, err := listObjects(ctx, client, src.bucket, prefix, startAfter, 500)
	if err != nil {
		return nil, err
	}
	notBefore := time.UnixMilli(params.StartTime).Add(-src.lookback)
	notAfter := time.UnixMilli(params.EndTime).Add(time.Hour) // 投递延迟容忍
	var entries []logquery.LogEntry
	scanned := 0
	for _, o := range objects {
		if int64(len(entries)) >= int64(limit) || scanned >= maxObjectsPerScan {
			break
		}
		// 对象级时间剪枝:LastModified 过旧的不再下载(对象按最新优先排列)
		if o.LastModified.Before(notBefore) {
			break
		}
		if o.LastModified.After(notAfter) {
			continue // 未来对象(时钟偏差),跳过不计数
		}
		body, err := getObjectBytes(ctx, client, src.bucket, o.Key, maxObjectBytes)
		if err != nil {
			p.logger.Warn("[logquery-aws] get object failed",
				elog.String("key", o.Key), elog.FieldErr(err))
			continue
		}
		scanned++
		meta := logquery.LogMeta{
			Cloud:       domain.CloudProviderAWS,
			AccountID:   fmt.Sprintf("%d", p.account.ID),
			AccountName: p.account.Name,
			Region:      src.region,
			ResourceID:  resource,
			Source:      src.bucket + "/" + o.Key,
		}
		switch src.kind {
		case "waf-json":
			entries = append(entries, parseWAFJSONLines(meta, body, params, int64(limit)-int64(len(entries)))...)
		case "cloudfront-tsv":
			entries = append(entries, parseCloudFrontBody(meta, body, params, int64(limit)-int64(len(entries)))...)
		}
	}
	return entries, nil
}

// parseWAFJSONLines JSON Lines -> 过滤窗口 -> 统一模型。
func parseWAFJSONLines(meta logquery.LogMeta, body []byte, params logquery.SearchParams, remain int64) []logquery.LogEntry {
	var out []logquery.LogEntry
	for _, line := range strings.Split(string(body), "\n") {
		if int64(len(out)) >= remain {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue // 非法行跳过
		}
		ts := logquery.ParseTimeMs(raw["timestamp"])
		if ts < params.StartTime || ts > params.EndTime {
			continue
		}
		out = append(out, mapWAFJSON(meta, raw))
	}
	return out
}

// parseCloudFrontBody TSV 正文(#Fields 头驱动)-> 过滤窗口 -> 统一模型。
func parseCloudFrontBody(meta logquery.LogMeta, body []byte, params logquery.SearchParams, remain int64) []logquery.LogEntry {
	var fields []string
	var out []logquery.LogEntry
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#Version") {
			continue
		}
		if strings.HasPrefix(line, "#Fields:") {
			fields = strings.Fields(strings.TrimPrefix(line, "#Fields:"))
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if int64(len(out)) >= remain {
			break
		}
		if len(fields) == 0 {
			continue // 无字段头的正文无法解析
		}
		e := mapCloudFrontLine(meta, fields, line)
		if e.Timestamp < params.StartTime || e.Timestamp > params.EndTime {
			continue
		}
		out = append(out, e)
	}
	return out
}

// pathBase 取路径末段("AWSLogs/123/WAFLogs/cloudfront/<acl>/" -> <acl>)。
func pathBase(s string) string {
	s = strings.TrimSuffix(s, "/")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}
