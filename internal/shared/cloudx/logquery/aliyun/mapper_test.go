package aliyun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// fixture Phase 0 拉取的真实样本结构(collect_fixtures.py 落盘)。
type fixture struct {
	Meta struct {
		Cloud    string `json:"cloud"`
		LogType  string `json:"log_type"`
		Desc     string `json:"desc"`
		Region   string `json:"region"`
		Project  string `json:"project"`
		Logstore string `json:"logstore"`
	} `json:"meta"`
	Samples []map[string]string `json:"samples"`
}

func loadFixture(t *testing.T, name string) fixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	if len(f.Samples) == 0 {
		t.Fatalf("fixture %s has no samples", name)
	}
	return f
}

func testMeta(f fixture) logquery.LogMeta {
	return logquery.LogMeta{
		Cloud:       "aliyun",
		AccountID:   "1",
		AccountName: "fixture",
		Region:      f.Meta.Region,
		ResourceID:  f.Meta.Logstore,
		Source:      f.Meta.Project + "/" + f.Meta.Logstore,
	}
}

// TestMapALBGolden 阿里 ALB 访问日志 golden(field-mapping.md §一.1;
// fixture: aliyun-alb-access.json,2026-08-27 实时拉取)。
func TestMapALBGolden(t *testing.T) {
	f := loadFixture(t, "aliyun-alb-access")
	for i, raw := range f.Samples {
		e, ok := mapEntry(kindALB, testMeta(f), raw).(*logquery.SLBLogEntry)
		if !ok {
			t.Fatalf("sample %d: not SLBLogEntry", i)
		}
		if e.Timestamp <= 0 {
			t.Errorf("sample %d: timestamp not normalized: %d", i, e.Timestamp)
		}
		if e.ClientIP == "" || e.Status == 0 || e.Method == "" {
			t.Errorf("sample %d: core fields empty: %+v", i, e)
		}
		if e.TargetIP == "" {
			t.Errorf("sample %d: upstream_addr not split: %q", i, raw["upstream_addr"])
		}
		if e.URL == "" || e.Host == "" {
			t.Errorf("sample %d: url/host not built", i)
		}
		if len(e.Raw) != len(raw) {
			t.Errorf("sample %d: raw not fully preserved: %d != %d", i, len(e.Raw), len(raw))
		}
		if e.Meta.ResourceID == "" {
			t.Errorf("sample %d: resource id empty", i)
		}
	}
	// 具体值断言(样本 0,Phase 0 实测字段值)
	e, _ := mapEntry(kindALB, testMeta(f), f.Samples[0]).(*logquery.SLBLogEntry)
	if e.Timestamp != 1787230557000 {
		t.Errorf("timestamp = %d, want 1787230557000(2026-08-20T20:55:57+08:00)", e.Timestamp)
	}
	if e.Status != 403 {
		t.Errorf("status = %d, want 403", e.Status)
	}
	if e.TargetIP != "172.19.152.31" || e.TargetPort != 6090 {
		t.Errorf("target = %s:%d, want 172.19.152.31:6090", e.TargetIP, e.TargetPort)
	}
	if e.LatencyMs != 9 { // request_time 0.009s
		t.Errorf("latency = %d, want 9", e.LatencyMs)
	}
}

// TestMapALBOverseasGolden 海外 ALB(schema 与国内一致,fixture 跨 region 校验)。
func TestMapALBOverseasGolden(t *testing.T) {
	f := loadFixture(t, "aliyun-alb-access-overseas")
	for i, raw := range f.Samples {
		e, ok := mapEntry(kindALB, testMeta(f), raw).(*logquery.SLBLogEntry)
		if !ok {
			t.Fatalf("sample %d: not SLBLogEntry", i)
		}
		if e.Timestamp <= 0 || e.Status == 0 {
			t.Errorf("sample %d: core fields bad: %+v", i, e)
		}
	}
}

// TestMapWAF3Golden 阿里 WAF3.0 访问日志 golden(field-mapping.md §二.3)。
func TestMapWAF3Golden(t *testing.T) {
	f := loadFixture(t, "aliyun-waf3-access")
	for i, raw := range f.Samples {
		e, ok := mapEntry(kindWAF3, testMeta(f), raw).(*logquery.WAFLogEntry)
		if !ok {
			t.Fatalf("sample %d: not WAFLogEntry", i)
		}
		if e.Timestamp <= 0 {
			t.Errorf("sample %d: timestamp not normalized(start_time=%q): %d",
				i, raw["start_time"], e.Timestamp)
		}
		if e.ClientIP == "" {
			t.Errorf("sample %d: client ip empty(real/src/remote 回退全失败)", i)
		}
		if e.Action != "pass" {
			t.Errorf("sample %d: access-log action = %q, want pass", i, e.Action)
		}
		if len(e.Raw) != len(raw) {
			t.Errorf("sample %d: raw not preserved", i)
		}
	}
	e, _ := mapEntry(kindWAF3, testMeta(f), f.Samples[0]).(*logquery.WAFLogEntry)
	if e.Timestamp != 1787824584000 {
		t.Errorf("timestamp = %d, want 1787824584000", e.Timestamp)
	}
	if e.ClientIP != "120.241.127.158" {
		t.Errorf("client ip = %q, want 120.241.127.158(real_client_ip 优先)", e.ClientIP)
	}
}

// TestMapDCDNGolden DCDN 边缘实时日志 golden(field-mapping.md §三.7)。
func TestMapDCDNGolden(t *testing.T) {
	f := loadFixture(t, "aliyun-dcdn-edge-rtlog")
	for i, raw := range f.Samples {
		e, ok := mapEntry(kindDCDN, testMeta(f), raw).(*logquery.CDNLogEntry)
		if !ok {
			t.Fatalf("sample %d: not CDNLogEntry", i)
		}
		if e.Timestamp <= 0 {
			t.Errorf("sample %d: timestamp bad(unixtime=%q)", i, raw["unixtime"])
		}
		if e.Host == "" || e.Status == 0 {
			t.Errorf("sample %d: domain/status empty", i)
		}
		if e.EdgeNode == "" && raw["via_info"] != "" {
			t.Errorf("sample %d: edge node not extracted: %q", i, raw["via_info"])
		}
	}
	e, _ := mapEntry(kindDCDN, testMeta(f), f.Samples[0]).(*logquery.CDNLogEntry)
	if e.Timestamp != 1787824545000 {
		t.Errorf("timestamp = %d, want 1787824545000", e.Timestamp)
	}
	if e.EdgeNode != "cache15.l2eu95-4[161,0]" {
		t.Errorf("edge node = %q", e.EdgeNode)
	}
}

// TestMapAkamaiCDNGolden Akamai CDN golden(field-mapping.md §三.8;坑位:reqTimeSec
// 为时刻戳、UA 为 URL 编码、cacheStatus 码表)。
func TestMapAkamaiCDNGolden(t *testing.T) {
	f := loadFixture(t, "aliyun-akamai-cdn")
	for i, raw := range f.Samples {
		e, ok := mapEntry(kindAkamaiCDN, testMeta(f), raw).(*logquery.CDNLogEntry)
		if !ok {
			t.Fatalf("sample %d: not CDNLogEntry", i)
		}
		if e.Timestamp <= 0 {
			t.Errorf("sample %d: timestamp bad(reqTimeSec=%q)", i, raw["reqTimeSec"])
		}
		if e.UserAgent != "" && e.UserAgent == raw["UA"] && strings.Contains(raw["UA"], "%") {
			t.Errorf("sample %d: UA not url-decoded: %q", i, e.UserAgent)
		}
		if e.CacheHit != "hit" && raw["cacheStatus"] == "0" {
			t.Errorf("sample %d: akamai cacheStatus 0 -> %q, want hit", i, e.CacheHit)
		}
	}
	e, _ := mapEntry(kindAkamaiCDN, testMeta(f), f.Samples[0]).(*logquery.CDNLogEntry)
	if e.Timestamp != 1787225893800 {
		t.Errorf("timestamp = %d, want 1787225893800(reqTimeSec 1787225893.800)", e.Timestamp)
	}
}

// TestMapAkamaiWAFGolden Akamai WAF golden(field-mapping.md §二.5;坑位:start 为秒,
// _unixtimestamp_ 为纳秒,severity 为 1~10)。
func TestMapAkamaiWAFGolden(t *testing.T) {
	f := loadFixture(t, "aliyun-akamai-waf")
	for i, raw := range f.Samples {
		e, ok := mapEntry(kindAkamaiWAF, testMeta(f), raw).(*logquery.WAFLogEntry)
		if !ok {
			t.Fatalf("sample %d: not WAFLogEntry", i)
		}
		if e.Timestamp <= 0 {
			t.Errorf("sample %d: timestamp bad(start=%q)", i, raw["start"])
		}
		if e.Action == "" {
			t.Errorf("sample %d: action empty(act=%q)", i, raw["act"])
		}
		if e.RuleName == "" && raw["name"] != "" {
			t.Errorf("sample %d: rule name lost", i)
		}
	}
	e, _ := mapEntry(kindAkamaiWAF, testMeta(f), f.Samples[0]).(*logquery.WAFLogEntry)
	if e.Timestamp != 1787824470000 {
		t.Errorf("timestamp = %d, want 1787824470000(start=1787824470 秒)", e.Timestamp)
	}
	if e.Action != "alert" {
		t.Errorf("action = %q, want alert(act=alert)", e.Action)
	}
	if e.Host != "wmsc.lcsc.com" {
		t.Errorf("host = %q, want wmsc.lcsc.com", e.Host)
	}
}

// TestMapEntryUnknownKind 未知 kind 返回 nil(新增源未配 mapper 时不静默产出错误数据)。
func TestMapEntryUnknownKind(t *testing.T) {
	if got := mapEntry(mapperKind("nope"), testMeta(fixture{}), map[string]string{"a": "b"}); got != nil {
		t.Errorf("unknown kind should return nil, got %v", got)
	}
}

// TestDomainField CDN 类域名原始字段名(按域名扇出的分组键)。
func TestDomainField(t *testing.T) {
	cases := map[mapperKind]string{
		kindDCDN:      "domain",
		kindAkamaiCDN: "reqHost",
		kindALB:       "",
		kindWAF3:      "",
	}
	for kind, want := range cases {
		if got := domainField(kind); got != want {
			t.Errorf("domainField(%q) = %q, want %q", kind, got, want)
		}
	}
}

// TestSplitByDomain 探查样本按域名分组(空域名行丢弃,分组键正确)。
func TestSplitByDomain(t *testing.T) {
	logs := []map[string]string{
		{"domain": "a.com"},
		{"domain": "b.com"},
		{"domain": "a.com"},
		{"domain": ""},     // 空域名丢弃
		{"reqHost": "c.com"}, // 非 domain 键,DCDN 分组下丢弃
	}
	got := splitByDomain(kindDCDN, logs)
	if len(got) != 2 {
		t.Fatalf("groups = %d, want 2: %v", len(got), got)
	}
	if len(got["a.com"]) != 2 || len(got["b.com"]) != 1 {
		t.Errorf("group counts wrong: %v", got)
	}
	if splitByDomain(kindALB, logs) != nil {
		t.Error("non-CDN kind should return nil")
	}
}
