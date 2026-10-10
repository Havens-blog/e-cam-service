package main

// cert 远程出站适配器的往返测试 + feature-flag selector 测试。
//
// 往返测试立一个实现内部契约的 httptest 假生产方，验证 remoteInstanceCounter
// 的请求形状（path/query/method）与响应解码正确。这把"远程 adapter"从死代码
// 变成可执行验证的真实代码：真实生产方（asset 域）上线后只要匹配本测试锁住的
// 同一契约，cert 远程模式即可直接工作——消费侧契约守护，与 CMDB 两侧快照同形。
//
// selector 测试锁住 feature-flag 语义：默认 local；mode=remote 必须有 base_url
// （fail-closed，不静默回退）；非法 mode 拒启。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
)

func TestRemoteInstanceCounter_RoundTrip(t *testing.T) {
	const wantModelUID = "host"
	const wantCount int64 = 42

	var gotPath, gotMethod, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotQuery = r.URL.Query().Get("modelUid")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"count": wantCount})
	}))
	defer srv.Close()

	rc := newRemoteInstanceCounter(srv.URL)
	got, err := rc.CountByModelUID(context.Background(), wantModelUID)
	if err != nil {
		t.Fatalf("远程往返失败: %v", err)
	}

	// 响应解码正确。
	if got != wantCount {
		t.Errorf("count = %d，期望 %d", got, wantCount)
	}
	// 请求形状符合内部契约。
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s，期望 GET", gotMethod)
	}
	if gotPath != "/internal/v1/instances/count" {
		t.Errorf("path = %s，期望 /internal/v1/instances/count", gotPath)
	}
	if gotQuery != wantModelUID {
		t.Errorf("modelUid = %q，期望 %q", gotQuery, wantModelUID)
	}
}

func TestRemoteInstanceCounter_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	rc := newRemoteInstanceCounter(srv.URL)
	if _, err := rc.CountByModelUID(context.Background(), "host"); err == nil {
		t.Fatal("上游 500 期望返回错误，却成功")
	}
}

func TestResolveUpstream_FeatureFlag(t *testing.T) {
	cases := []struct {
		name     string
		set      map[string]any
		wantMode string
		wantErr  bool
	}{
		{
			name:     "缺省即 local",
			set:      map[string]any{},
			wantMode: "local",
		},
		{
			name:     "显式 local",
			set:      map[string]any{"cert-server.upstream.instance_counter.mode": "local"},
			wantMode: "local",
		},
		{
			name: "remote 带 base_url 合法",
			set: map[string]any{
				"cert-server.upstream.instance_counter.mode":     "remote",
				"cert-server.upstream.instance_counter.base_url": "http://asset-svc:8080",
			},
			wantMode: "remote",
		},
		{
			name:    "remote 缺 base_url fail-closed",
			set:     map[string]any{"cert-server.upstream.instance_counter.mode": "remote"},
			wantErr: true,
		},
		{
			name:    "非法 mode 拒启",
			set:     map[string]any{"cert-server.upstream.instance_counter.mode": "hybrid"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()
			for k, v := range tc.set {
				viper.Set(k, v)
			}

			cfg, err := resolveUpstream("instance_counter")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望错误（fail-closed），却成功: %+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if cfg.Mode != tc.wantMode {
				t.Errorf("mode = %q，期望 %q", cfg.Mode, tc.wantMode)
			}
		})
	}
}
