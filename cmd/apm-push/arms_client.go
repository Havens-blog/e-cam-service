package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

// ServiceDependency represents a service-to-service call relationship from ARMS.
type ServiceDependency struct {
	CallerServiceName string
	CalleeServiceName string
	QPS               float64
	LatencyP99        float64
	ErrorRate         float64
	TraceIDs          []string // Sample trace IDs for domain propagation
}

// Trace represents a distributed trace with its spans.
type Trace struct {
	TraceID string
	Spans   []Span
}

// Span represents a single span in a trace.
type Span struct {
	SpanID       string
	ParentSpanID string
	ServiceName  string
	ServiceIp    string            // 服务实例 IP（Pod IP 或 Node IP）
	Tags         map[string]string // Contains http.host, etc.
}

// ARMSClient defines the interface for fetching data from ARMS OpenAPI.
type ARMSClient interface {
	// GetServiceDependencies fetches service call relationships.
	GetServiceDependencies(ctx context.Context) ([]ServiceDependency, error)
	// GetTraceDetails fetches trace details for domain extraction.
	GetTraceDetails(ctx context.Context, traceIDs []string) ([]Trace, error)
}

// DefaultARMSClient implements ARMSClient using Alibaba Cloud SDK CommonRequest.
// Calls xtrace (2019-08-08) OpenAPI:
//   - SearchTraces: search traces by time range, returns trace list with traceID/serviceName/duration
//   - GetTrace: get full span tree for a traceID, each span has SpanId/ParentSpanId/ServiceName/TagEntryList
type DefaultARMSClient struct {
	client   *sdk.Client
	regionID string
}

// NewDefaultARMSClient creates a new ARMS client with the given credentials.
func NewDefaultARMSClient(accessKeyID, accessKeySecret, regionID string) (*DefaultARMSClient, error) {
	client, err := sdk.NewClientWithAccessKey(regionID, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create SDK client: %w", err)
	}
	return &DefaultARMSClient{client: client, regionID: regionID}, nil
}

// ---- SearchTraces response structures ----

type searchTracesResponse struct {
	RequestID string   `json:"RequestId"`
	PageBean  pageBean `json:"PageBean"`
}

type pageBean struct {
	PageSize   int        `json:"PageSize"`
	PageNumber int        `json:"PageNumber"`
	TotalCount int        `json:"TotalCount"`
	TraceInfos traceInfos `json:"TraceInfos"`
}

type traceInfos struct {
	TraceInfo []traceInfo `json:"TraceInfo"`
}

type traceInfo struct {
	TraceID       string         `json:"TraceID"`
	Duration      int64          `json:"Duration"`
	ServiceName   string         `json:"ServiceName"`
	ServiceIp     string         `json:"ServiceIp"`
	OperationName string         `json:"OperationName"`
	Timestamp     int64          `json:"Timestamp"`
	TagMap        map[string]any `json:"TagMap"`
}

// ---- GetTrace response structures ----

type getTraceResponse struct {
	RequestID string    `json:"RequestId"`
	Spans     spansList `json:"Spans"`
}

type spansList struct {
	Span []spanItem `json:"Span"`
}

type spanItem struct {
	SpanId        string       `json:"SpanId"`
	ParentSpanId  string       `json:"ParentSpanId"`
	ServiceName   string       `json:"ServiceName"`
	ServiceIp     string       `json:"ServiceIp"`
	OperationName string       `json:"OperationName"`
	Duration      int64        `json:"Duration"`
	Timestamp     int64        `json:"Timestamp"`
	ResultCode    string       `json:"ResultCode"`
	TraceID       string       `json:"TraceID"`
	HaveStack     bool         `json:"HaveStack"`
	RpcId         string       `json:"RpcId"`
	TagEntryList  tagEntryList `json:"TagEntryList"`
}

type tagEntryList struct {
	TagEntry []tagEntry `json:"TagEntry"`
}

type tagEntry struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

// GetServiceDependencies fetches service call relationships by analyzing recent traces.
//
// Strategy:
//  1. Call SearchTraces to get recent traces (last 5 minutes)
//  2. For each trace, call GetTrace to get the full span tree
//  3. Extract caller→callee relationships from parent-child spans across different services
//  4. Aggregate into ServiceDependency list with QPS/latency/error metrics
func (c *DefaultARMSClient) GetServiceDependencies(ctx context.Context) ([]ServiceDependency, error) {
	now := time.Now()
	startTime := now.Add(-5 * time.Minute)

	log.Printf("Fetching traces from ARMS (region=%s, window=%s to %s)",
		c.regionID, startTime.Format(time.RFC3339), now.Format(time.RFC3339))

	// Step 1: Search recent traces via xtrace SearchTraces API
	req := requests.NewCommonRequest()
	req.Method = "POST"
	req.Scheme = "https"
	req.Domain = fmt.Sprintf("xtrace.%s.aliyuncs.com", c.regionID)
	req.Version = "2019-08-08"
	req.ApiName = "SearchTraces"
	req.QueryParams["RegionId"] = c.regionID
	req.QueryParams["StartTime"] = strconv.FormatInt(startTime.UnixMilli(), 10)
	req.QueryParams["EndTime"] = strconv.FormatInt(now.UnixMilli(), 10)
	req.QueryParams["PageNumber"] = "1"
	req.QueryParams["PageSize"] = "100"

	resp, err := c.client.ProcessCommonRequest(req)
	if err != nil {
		return nil, fmt.Errorf("SearchTraces failed: %w", err)
	}

	var searchResp searchTracesResponse
	if err := json.Unmarshal(resp.GetHttpContentBytes(), &searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse SearchTraces response: %w", err)
	}

	traceInfoList := searchResp.PageBean.TraceInfos.TraceInfo
	log.Printf("SearchTraces returned %d traces (total=%d)", len(traceInfoList), searchResp.PageBean.TotalCount)

	if len(traceInfoList) == 0 {
		return nil, nil
	}

	// Step 2: Get full span details for each trace
	traceIDs := make([]string, 0, len(traceInfoList))
	for _, ti := range traceInfoList {
		traceIDs = append(traceIDs, ti.TraceID)
	}

	allTraces, err := c.GetTraceDetails(ctx, traceIDs)
	if err != nil {
		log.Printf("WARN: GetTraceDetails partially failed: %v", err)
	}

	// Step 3: Extract service dependencies from span trees
	type edgeKey struct{ caller, callee string }
	type edgeStats struct {
		totalDuration float64 // milliseconds
		count         int
		errorCount    int
		traceIDs      []string
	}
	edgeMap := make(map[edgeKey]*edgeStats)

	for _, trace := range allTraces {
		spanMap := make(map[string]*Span)
		for i := range trace.Spans {
			spanMap[trace.Spans[i].SpanID] = &trace.Spans[i]
		}

		for _, span := range trace.Spans {
			if span.ParentSpanID == "" {
				continue
			}
			parent, ok := spanMap[span.ParentSpanID]
			if !ok || parent.ServiceName == span.ServiceName {
				continue // Same service or orphan span
			}

			key := edgeKey{caller: parent.ServiceName, callee: span.ServiceName}
			stats, exists := edgeMap[key]
			if !exists {
				stats = &edgeStats{}
				edgeMap[key] = stats
			}
			stats.count++

			// Duration from tags if available
			if durStr, ok := span.Tags["duration"]; ok {
				var dur float64
				fmt.Sscanf(durStr, "%f", &dur)
				stats.totalDuration += dur
			}

			// Error detection via tags
			if span.Tags["error"] == "true" || span.Tags["otel.status_code"] == "ERROR" {
				stats.errorCount++
			}

			// Sample trace IDs (keep up to 10 per edge for domain propagation)
			if len(stats.traceIDs) < 10 {
				stats.traceIDs = append(stats.traceIDs, trace.TraceID)
			}
		}
	}

	// Step 4: Convert to ServiceDependency list
	windowSeconds := 5.0 * 60.0 // 5 minute window
	deps := make([]ServiceDependency, 0, len(edgeMap))
	for key, stats := range edgeMap {
		avgLatency := 0.0
		if stats.count > 0 {
			avgLatency = stats.totalDuration / float64(stats.count)
		}
		errorRate := 0.0
		if stats.count > 0 {
			errorRate = float64(stats.errorCount) / float64(stats.count) * 100
		}

		deps = append(deps, ServiceDependency{
			CallerServiceName: key.caller,
			CalleeServiceName: key.callee,
			QPS:               float64(stats.count) / windowSeconds,
			LatencyP99:        avgLatency, // Approximation: using avg as P99 proxy from sampled traces
			ErrorRate:         errorRate,
			TraceIDs:          stats.traceIDs,
		})
	}

	log.Printf("Extracted %d service dependencies from %d traces", len(deps), len(allTraces))
	return deps, nil
}

// GetTraceDetails fetches detailed trace data for the given trace IDs.
// Calls xtrace GetTrace API for each traceID, parses spans with tags.
func (c *DefaultARMSClient) GetTraceDetails(ctx context.Context, traceIDs []string) ([]Trace, error) {
	log.Printf("Fetching %d trace details from ARMS", len(traceIDs))

	traces := make([]Trace, 0, len(traceIDs))
	var lastErr error

	for _, tid := range traceIDs {
		req := requests.NewCommonRequest()
		req.Method = "POST"
		req.Scheme = "https"
		req.Domain = fmt.Sprintf("xtrace.%s.aliyuncs.com", c.regionID)
		req.Version = "2019-08-08"
		req.ApiName = "GetTrace"
		req.QueryParams["RegionId"] = c.regionID
		req.QueryParams["TraceID"] = tid

		resp, err := c.client.ProcessCommonRequest(req)
		if err != nil {
			log.Printf("WARN: GetTrace failed for traceID=%s: %v", tid, err)
			lastErr = err
			continue
		}

		var traceResp getTraceResponse
		if err := json.Unmarshal(resp.GetHttpContentBytes(), &traceResp); err != nil {
			log.Printf("WARN: failed to parse GetTrace response for traceID=%s: %v", tid, err)
			lastErr = err
			continue
		}

		trace := Trace{TraceID: tid}
		for _, s := range traceResp.Spans.Span {
			tags := make(map[string]string)
			for _, tag := range s.TagEntryList.TagEntry {
				tags[tag.Key] = tag.Value
			}
			// Also store duration and result code as tags for downstream processing
			tags["duration"] = strconv.FormatInt(s.Duration, 10)
			if s.ResultCode != "" && s.ResultCode != "200" && s.ResultCode != "0" {
				tags["error"] = "true"
			}

			trace.Spans = append(trace.Spans, Span{
				SpanID:       s.SpanId,
				ParentSpanID: s.ParentSpanId,
				ServiceName:  s.ServiceName,
				ServiceIp:    s.ServiceIp,
				Tags:         tags,
			})
		}
		traces = append(traces, trace)
	}

	if lastErr != nil && len(traces) == 0 {
		return nil, fmt.Errorf("all GetTrace calls failed, last error: %w", lastErr)
	}

	log.Printf("Successfully fetched %d/%d trace details", len(traces), len(traceIDs))
	return traces, nil
}
