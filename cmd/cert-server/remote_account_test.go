package main

// cert CloudAccountLister 远程适配器的往返测试 + https 安全闸测试。
//
// 往返测试：立一个实现内部契约的 httptest 假生产方，验证 filter→query 形状与
// 共享 SDK 类型的凭据字段（access_key_id/access_key_secret）零翻译往返。这是三个
// 远程端口里契约最稳的一个——线上类型直接复用已发布的 sharedomain.CloudAccount。
//
// 安全闸测试锁住 secure-by-default：云账号端口 remote 模式默认强制 https，明文
// http 即 fail-closed 拒绝启动，除非显式 allow_insecure=true。这是凭据不裸奔的
// 最后防线，绝不允许静默降级。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

func TestRemoteCloudAccountLister_RoundTrip(t *testing.T) {
	var gotPath, gotStatus, gotProvider string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotStatus = r.URL.Query().Get("status")
		gotProvider = r.URL.Query().Get("provider")

		acct := sharedomain.CloudAccount{
			ID: 1, Name: "prod-aliyun", Provider: sharedomain.CloudProvider("aliyun"),
			AccessKeyID: "AKID-example", AccessKeySecret: "SECRET-example",
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": []sharedomain.CloudAccount{acct},
			"total":    int64(1),
		})
	}))
	defer srv.Close()

	lister := newRemoteCloudAccountLister(srv.URL)
	got, total, err := lister.List(context.Background(), sharedomain.CloudAccountFilter{
		Provider: sharedomain.CloudProvider("aliyun"),
		Status:   sharedomain.CloudAccountStatus("active"),
	})
	if err != nil {
		t.Fatalf("远程往返失败: %v", err)
	}

	if gotPath != "/internal/v1/cloud-accounts" {
		t.Errorf("path = %s，期望 /internal/v1/cloud-accounts", gotPath)
	}
	if gotProvider != "aliyun" {
		t.Errorf("provider query = %q，期望 aliyun", gotProvider)
	}
	if gotStatus != "active" {
		t.Errorf("status query = %q，期望 active", gotStatus)
	}
	if total != 1 {
		t.Errorf("total = %d，期望 1", total)
	}
	// 零翻译往返：凭据字段必须原样可读（这是远程模式工作的前提）。
	if len(got) != 1 {
		t.Fatalf("accounts 长度 = %d，期望 1", len(got))
	}
	if got[0].AccessKeyID != "AKID-example" || got[0].AccessKeySecret != "SECRET-example" {
		t.Errorf("凭据字段往返失真: AKID=%q Secret=%q", got[0].AccessKeyID, got[0].AccessKeySecret)
	}
}

func TestRequireSecureForCredentials(t *testing.T) {
	cases := []struct {
		name    string
		cfg     upstreamConfig
		wantErr bool
	}{
		{
			name:    "https 缺省通过",
			cfg:     upstreamConfig{BaseURL: "https://account-service:8443"},
			wantErr: false,
		},
		{
			name:    "http 缺省拒绝（fail-closed）",
			cfg:     upstreamConfig{BaseURL: "http://account-service:8080"},
			wantErr: true,
		},
		{
			name:    "http 但显式 allow_insecure 放行",
			cfg:     upstreamConfig{BaseURL: "http://account-service:8080", AllowInsecure: true},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireSecureForCredentials("cloud_account_lister", tc.cfg)
			if tc.wantErr && err == nil {
				t.Fatal("期望安全闸拒绝（凭据走明文），却放行")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("意外拒绝: %v", err)
			}
		})
	}
}
