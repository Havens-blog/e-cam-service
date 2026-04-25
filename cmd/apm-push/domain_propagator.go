package main

import (
	"sort"
)

// DomainMetric holds per-domain aggregated metrics for a call edge.
type DomainMetric struct {
	QPS        float64
	LatencyP99 float64
	ErrorRate  float64
}

// DomainPropagator extracts domains from trace root spans and propagates them
// to all cross-service call edges along the trace.
type DomainPropagator interface {
	// Propagate extracts http.host from trace root spans and propagates domains
	// to each cross-service call edge. Returns: edgeKey("caller→callee") → []domain
	Propagate(traces []Trace) map[string][]string

	// AggregateDomainMetrics computes per-domain metrics for each edge.
	// Returns: edgeKey → domainName → DomainMetric
	AggregateDomainMetrics(traces []Trace, deps []ServiceDependency) map[string]map[string]DomainMetric
}

// DefaultDomainPropagator implements DomainPropagator.
type DefaultDomainPropagator struct{}

// NewDefaultDomainPropagator creates a new domain propagator.
func NewDefaultDomainPropagator() *DefaultDomainPropagator {
	return &DefaultDomainPropagator{}
}

// EdgeKey constructs a canonical edge key from caller and callee service names.
func EdgeKey(caller, callee string) string {
	return caller + "→" + callee
}

// Propagate extracts http.host from trace root spans and propagates the domain
// to all cross-service call edges via DFS traversal of the span tree.
func (p *DefaultDomainPropagator) Propagate(traces []Trace) map[string][]string {
	edgeDomains := make(map[string][]string)

	for _, trace := range traces {
		root := findRoot(trace.Spans)
		if root == nil {
			continue
		}

		domain := root.Tags["http.host"]
		if domain == "" {
			continue // Non-HTTP entry trace, skip domain propagation
		}

		// Build span tree: parentSpanID → children
		children := buildSpanTree(trace.Spans)

		// DFS: propagate domain to every cross-service call edge
		var dfs func(span *Span)
		dfs = func(span *Span) {
			for _, child := range children[span.SpanID] {
				if child.ServiceName != span.ServiceName {
					key := EdgeKey(span.ServiceName, child.ServiceName)
					edgeDomains[key] = appendUnique(edgeDomains[key], domain)
				}
				dfs(child)
			}
		}
		dfs(root)
	}

	return edgeDomains
}

// AggregateDomainMetrics computes per-domain metrics for each edge by analyzing
// which traces (and their domains) pass through each edge.
// For each edge, metrics from ServiceDependency are distributed proportionally
// across the domains that use that edge.
func (p *DefaultDomainPropagator) AggregateDomainMetrics(traces []Trace, deps []ServiceDependency) map[string]map[string]DomainMetric {
	// Step 1: Count how many traces per domain pass through each edge
	// edgeKey → domain → count
	edgeDomainCounts := make(map[string]map[string]int)

	for _, trace := range traces {
		root := findRoot(trace.Spans)
		if root == nil {
			continue
		}
		domain := root.Tags["http.host"]
		if domain == "" {
			continue
		}

		children := buildSpanTree(trace.Spans)
		// Track which edges this trace touches (deduplicate within a single trace)
		touchedEdges := make(map[string]bool)

		var dfs func(span *Span)
		dfs = func(span *Span) {
			for _, child := range children[span.SpanID] {
				if child.ServiceName != span.ServiceName {
					key := EdgeKey(span.ServiceName, child.ServiceName)
					touchedEdges[key] = true
				}
				dfs(child)
			}
		}
		dfs(root)

		for key := range touchedEdges {
			if edgeDomainCounts[key] == nil {
				edgeDomainCounts[key] = make(map[string]int)
			}
			edgeDomainCounts[key][domain]++
		}
	}

	// Step 2: Build a lookup for dependency metrics
	depMetrics := make(map[string]*ServiceDependency)
	for i := range deps {
		key := EdgeKey(deps[i].CallerServiceName, deps[i].CalleeServiceName)
		depMetrics[key] = &deps[i]
	}

	// Step 3: Distribute metrics proportionally by domain trace count
	result := make(map[string]map[string]DomainMetric)
	for edgeKey, domainCounts := range edgeDomainCounts {
		dep, ok := depMetrics[edgeKey]
		if !ok {
			continue
		}

		totalCount := 0
		for _, c := range domainCounts {
			totalCount += c
		}
		if totalCount == 0 {
			continue
		}

		result[edgeKey] = make(map[string]DomainMetric)
		for domain, count := range domainCounts {
			ratio := float64(count) / float64(totalCount)
			result[edgeKey][domain] = DomainMetric{
				QPS:        dep.QPS * ratio,
				LatencyP99: dep.LatencyP99, // P99 is not additive, keep as-is per domain
				ErrorRate:  dep.ErrorRate * ratio,
			}
		}
	}

	return result
}

// findRoot finds the root span (parentSpanID is empty) in a list of spans.
func findRoot(spans []Span) *Span {
	for i := range spans {
		if spans[i].ParentSpanID == "" {
			return &spans[i]
		}
	}
	return nil
}

// buildSpanTree builds a map from parentSpanID to child spans.
func buildSpanTree(spans []Span) map[string][]*Span {
	children := make(map[string][]*Span)
	for i := range spans {
		if spans[i].ParentSpanID != "" {
			children[spans[i].ParentSpanID] = append(children[spans[i].ParentSpanID], &spans[i])
		}
	}
	return children
}

// appendUnique appends a value to a slice only if it's not already present.
func appendUnique(slice []string, val string) []string {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	result := append(slice, val)
	sort.Strings(result)
	return result
}
