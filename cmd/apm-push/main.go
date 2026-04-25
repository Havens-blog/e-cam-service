package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// --debug-tags 模式：打印 ARMS span tags，用于确认 IP 信息
	if len(os.Args) > 1 && os.Args[1] == "--debug-tags" {
		if err := debugTags(); err != nil {
			log.Fatal(err)
		}
		return
	}

	log.Println("APM push script starting...")

	if err := run(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}

	log.Println("APM push script completed successfully")
}

func run() error {
	ctx := context.Background()

	// 1. Load configuration
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	log.Printf("Config loaded: region=%s, cluster=%s, tenant=%s", cfg.ARMSRegionID, cfg.K8sClusterName, cfg.TenantID)

	// 2. Initialize ARMS client and fetch service dependencies
	armsClient, err := NewDefaultARMSClient(cfg.ARMSAccessKeyID, cfg.ARMSAccessKeySecret, cfg.ARMSRegionID)
	if err != nil {
		return err
	}
	deps, err := armsClient.GetServiceDependencies(ctx)
	if err != nil {
		return err
	}
	log.Printf("Fetched %d service dependencies from ARMS", len(deps))

	// 3. Fetch trace details for domain propagation
	traceIDs := collectTraceIDs(deps)
	var traces []Trace
	if len(traceIDs) > 0 {
		traces, err = armsClient.GetTraceDetails(ctx, traceIDs)
		if err != nil {
			log.Printf("WARN: failed to fetch trace details for domain propagation: %v", err)
			// Continue without domain info — not fatal
		} else {
			log.Printf("Fetched %d traces for domain propagation", len(traces))
		}
	}

	// 4. Build service name mapping (基于 ARMS 命名规则，不需要连接 K8s 集群)
	// ARMS 服务名格式：{namespace}_{deployment-name}，如 prod_sdp-model-analyzer
	mapper := NewDefaultServiceNameMapper()
	// 从 deps 和 traces 中收集所有 ARMS 服务名
	serviceNames := collectServiceNames(deps, traces)
	nameMapping := mapper.BuildMappingFromServices(serviceNames, cfg.K8sClusterName)
	log.Printf("Built service name mapping with %d entries", len(nameMapping))

	// 5. Propagate domains from traces
	propagator := NewDefaultDomainPropagator()
	edgeDomains := propagator.Propagate(traces)
	domainMetrics := propagator.AggregateDomainMetrics(traces, deps)
	log.Printf("Propagated domains for %d edges", len(edgeDomains))

	// 5.5 收集每个服务的 ServiceIp 列表（用于 ELB→Gateway 桥接）
	serviceIPs := collectServiceIPs(traces)
	log.Printf("Collected ServiceIPs for %d services", len(serviceIPs))

	// 6. Generate LinkDeclarations
	generator := NewDefaultDeclarationGenerator(cfg.TenantID)
	declarations := generator.GenerateWithIPs(deps, nameMapping, edgeDomains, domainMetrics, serviceIPs)
	log.Printf("Generated %d link declarations", len(declarations))

	// 7. Push declarations to topology API
	pusher := NewHTTPPusher(cfg.TopologyAPIURL, cfg.TenantID)
	pusher.PushAll(ctx, declarations)

	return nil
}

// collectTraceIDs extracts unique trace IDs from service dependencies for domain propagation.
func collectTraceIDs(deps []ServiceDependency) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, dep := range deps {
		for _, tid := range dep.TraceIDs {
			if !seen[tid] {
				seen[tid] = true
				ids = append(ids, tid)
			}
		}
	}
	return ids
}

// collectServiceNames extracts all unique ARMS service names from dependencies and traces.
func collectServiceNames(deps []ServiceDependency, traces []Trace) []string {
	seen := make(map[string]bool)
	for _, dep := range deps {
		seen[dep.CallerServiceName] = true
		seen[dep.CalleeServiceName] = true
	}
	for _, trace := range traces {
		for _, span := range trace.Spans {
			if span.ServiceName != "" {
				seen[span.ServiceName] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}

// collectServiceIPs 从 trace 数据中收集每个 ARMS 服务的 ServiceIp 列表。
// 返回: ARMS 服务名 → []IP（去重）
// 这些 IP 是服务 Pod 的 IP 地址，用于在后端做 ELB→Gateway 的桥接匹配。
func collectServiceIPs(traces []Trace) map[string][]string {
	svcIPs := make(map[string]map[string]bool) // serviceName → set of IPs
	for _, trace := range traces {
		for _, span := range trace.Spans {
			if span.ServiceName == "" || span.ServiceIp == "" {
				continue
			}
			if svcIPs[span.ServiceName] == nil {
				svcIPs[span.ServiceName] = make(map[string]bool)
			}
			svcIPs[span.ServiceName][span.ServiceIp] = true
		}
	}
	result := make(map[string][]string, len(svcIPs))
	for svc, ips := range svcIPs {
		list := make([]string, 0, len(ips))
		for ip := range ips {
			list = append(list, ip)
		}
		result[svc] = list
	}
	return result
}

// debugTags 打印 ARMS trace span 的所有 tags，用于确认是否有 IP 信息
// 用法：go run . --debug-tags [serviceName]
// 例如：go run . --debug-tags prod-eda-access-gateway
func debugTags() error {
	loadEnvFile(".env")

	// 可选：指定服务名过滤
	serviceName := ""
	if len(os.Args) > 2 {
		serviceName = os.Args[2]
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	client, err := NewDefaultARMSClient(cfg.ARMSAccessKeyID, cfg.ARMSAccessKeySecret, cfg.ARMSRegionID)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// 直接用 SearchTraces API 搜索指定服务的 trace
	now := time.Now()
	startTime := now.Add(-5 * time.Minute)

	req := requests.NewCommonRequest()
	req.Method = "POST"
	req.Scheme = "https"
	req.Domain = fmt.Sprintf("xtrace.%s.aliyuncs.com", cfg.ARMSRegionID)
	req.Version = "2019-08-08"
	req.ApiName = "SearchTraces"
	req.QueryParams["RegionId"] = cfg.ARMSRegionID
	req.QueryParams["StartTime"] = strconv.FormatInt(startTime.UnixMilli(), 10)
	req.QueryParams["EndTime"] = strconv.FormatInt(now.UnixMilli(), 10)
	req.QueryParams["PageNumber"] = "1"
	req.QueryParams["PageSize"] = "10"
	if serviceName != "" {
		req.QueryParams["ServiceName"] = serviceName
		fmt.Printf("Searching traces for service: %s\n", serviceName)
	}

	resp, err := client.client.ProcessCommonRequest(req)
	if err != nil {
		return fmt.Errorf("SearchTraces failed: %w", err)
	}

	var searchResp searchTracesResponse
	if err := json.Unmarshal(resp.GetHttpContentBytes(), &searchResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	traceInfoList := searchResp.PageBean.TraceInfos.TraceInfo
	fmt.Printf("Found %d traces (total=%d)\n", len(traceInfoList), searchResp.PageBean.TotalCount)

	if len(traceInfoList) == 0 {
		fmt.Println("No traces found. Try a different service name or time window.")
		return nil
	}

	// 取前 3 条 trace 的详情
	traceIDs := make([]string, 0)
	for i, ti := range traceInfoList {
		if i >= 3 {
			break
		}
		traceIDs = append(traceIDs, ti.TraceID)
	}

	traces, err := client.GetTraceDetails(ctx, traceIDs)
	if err != nil {
		return err
	}

	// 打印每个 span 的 tags
	for _, trace := range traces {
		fmt.Printf("\n=== Trace: %s (%d spans) ===\n", trace.TraceID, len(trace.Spans))
		for _, span := range trace.Spans {
			rootMark := ""
			if span.ParentSpanID == "" {
				rootMark = " [ROOT]"
			}
			fmt.Printf("\n  Span: %s%s\n", span.ServiceName, rootMark)
			fmt.Printf("    SpanID: %s, ParentSpanID: %s, ServiceIp: %s\n", span.SpanID, span.ParentSpanID, span.ServiceIp)

			keys := make([]string, 0, len(span.Tags))
			for k := range span.Tags {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, k := range keys {
				v := span.Tags[k]
				highlight := ""
				kLower := strings.ToLower(k)
				if strings.Contains(kLower, "ip") || strings.Contains(kLower, "host") ||
					strings.Contains(kLower, "addr") || strings.Contains(kLower, "peer") ||
					strings.Contains(kLower, "port") || strings.Contains(kLower, "net.") ||
					strings.Contains(kLower, "http.url") {
					highlight = " <<<< IP/HOST"
				}
				if len(v) > 100 {
					v = v[:100] + "..."
				}
				fmt.Printf("    %s = %s%s\n", k, v, highlight)
			}
		}
	}

	// 汇总所有 tag keys
	allKeys := make(map[string]int)
	for _, trace := range traces {
		for _, span := range trace.Spans {
			for k := range span.Tags {
				allKeys[k]++
			}
		}
	}
	fmt.Printf("\n=== All Tag Keys (across %d traces) ===\n", len(traces))
	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	for _, k := range sortedKeys {
		fmt.Printf("  %s (count: %d)\n", k, allKeys[k])
	}

	// 打印第一个 trace 的原始 JSON
	if len(traces) > 0 {
		fmt.Printf("\n=== First Trace Raw JSON ===\n")
		data, _ := json.MarshalIndent(traces[0], "", "  ")
		fmt.Println(string(data))
	}

	return nil
}
