package dao

import (
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestKeySignature(t *testing.T) {
	cases := []struct {
		name string
		keys bson.D
		want string
	}{
		{
			name: "单字段",
			keys: bson.D{{Key: "request_id", Value: 1}},
			want: "request_id:1",
		},
		{
			name: "复合升序降序",
			keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "ctime", Value: -1}},
			want: "tenant_id:1,ctime:-1",
		},
		{
			// 键序不同 ⇒ 签名不同：两者查询语义不同（ESR 前缀），不算等价
			name: "键序不同签名不同",
			keys: bson.D{{Key: "ctime", Value: -1}, {Key: "tenant_id", Value: 1}},
			want: "ctime:-1,tenant_id:1",
		},
		{
			name: "空键",
			keys: bson.D{},
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := keySignature(c.keys); got != c.want {
				t.Fatalf("keySignature(%v) = %q, want %q", c.keys, got, c.want)
			}
		})
	}
}

// 键序不同的两个签名必须互不相等（防误跳过）。
func TestKeySignature_OrderSensitive(t *testing.T) {
	a := keySignature(bson.D{{Key: "tenant_id", Value: 1}, {Key: "ctime", Value: -1}})
	b := keySignature(bson.D{{Key: "ctime", Value: -1}, {Key: "tenant_id", Value: 1}})
	if a == b {
		t.Fatalf("不同键序的签名不应相等: %q", a)
	}
}

func TestIsEquivalentIndexConflict_Nil(t *testing.T) {
	if isEquivalentIndexConflict(nil) {
		t.Fatal("nil 错误不应被判为等价冲突")
	}
}

func TestIsEquivalentIndexConflict_ByMessage(t *testing.T) {
	// 服务器对 IndexOptionsConflict 的消息原文（code 85）
	err := errors.New("Index already exists with a different name: idx_tenant_id_ctime")
	if !isEquivalentIndexConflict(err) {
		t.Fatal("等价索引冲突应被识别为可跳过")
	}
	other := errors.New("IndexKeySpecsConflict: Index build failed")
	if isEquivalentIndexConflict(other) {
		t.Fatal("同名不同键/选项的冲突不应被跳过")
	}
}
