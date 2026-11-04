package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalOverridePath 覆盖主配置到本地覆盖文件的路径推导。
//
// 重点是无扩展名与多级目录这两种容易出错的形态：viper 依赖文件名后缀判定解析器，
// 推成 "prod.local" 会读不出来（静默失效），因此必须补成 "prod.local.yaml"。
func TestLocalOverridePath(t *testing.T) {
	testCases := []struct {
		name string
		conf string
		want string
	}{
		{name: "标准 yaml", conf: "config/prod.yaml", want: "config/prod.local.yaml"},
		{name: "yml 后缀需保留", conf: "config/test.yml", want: "config/test.local.yml"},
		{name: "无扩展名补 yaml", conf: "config/prod", want: "config/prod.local.yaml"},
		{name: "裸文件名", conf: "prod.yaml", want: "prod.local.yaml"},
		{name: "绝对路径", conf: "/etc/ecam/prod.yaml", want: "/etc/ecam/prod.local.yaml"},
		{name: "文件名含点", conf: "config/prod.v2.yaml", want: "config/prod.v2.local.yaml"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, localOverridePath(tc.conf))
		})
	}
}

// TestMergeLocalOverride_NoLocalFile 没有本地覆盖文件时必须静默跳过：不 panic、不改动既有配置。
func TestMergeLocalOverride_NoLocalFile(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "prod.yaml")
	writeFile(t, base, "server:\n  port: 8001\n")

	viper.Reset()
	confFile = base
	viper.SetConfigFile(base)
	require.NoError(t, viper.ReadInConfig())

	require.NotPanics(t, mergeLocalOverride)
	assert.Equal(t, 8001, viper.GetInt("server.port"))
}

// TestMergeLocalOverride_OnlyOverridesLocalKeys 是本改动的核心语义：
// 本地文件只覆盖它出现的键，其余键必须保持主配置的值。
//
// 这是在锁定 MergeInConfig 与 ReadInConfig 的行为差异 —— 若有人改回 ReadInConfig，
// "server.port 仍为 8001"与"name 未被清空"这两条断言会立即失败。
func TestMergeLocalOverride_OnlyOverridesLocalKeys(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "prod.yaml")
	writeFile(t, base, "server:\n  port: 8001\nname: base-name\n")

	local := filepath.Join(dir, "prod.local.yaml")
	writeFile(t, local, "eiam:\n  token_url: http://127.0.0.1:9000/oauth/v2/token\nname: local-name\n")

	viper.Reset()
	confFile = base
	viper.SetConfigFile(base)
	require.NoError(t, viper.ReadInConfig())
	require.Equal(t, "base-name", viper.GetString("name"))

	mergeLocalOverride()

	// 本地文件里出现的键被覆盖
	assert.Equal(t, "local-name", viper.GetString("name"))
	assert.Equal(t, "http://127.0.0.1:9000/oauth/v2/token", viper.GetString("eiam.token_url"))
	// 主配置里存在、本地文件未提及的键必须保留
	assert.Equal(t, 8001, viper.GetInt("server.port"))
}

// TestMergeLocalOverride_LocalFileBroken 本地覆盖文件语法错误时必须显式失败（panic），
// 而不是静默忽略 —— 否则凭据配错了却以为生效，排查成本极高。
func TestMergeLocalOverride_LocalFileBroken(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "prod.yaml")
	writeFile(t, base, "server:\n  port: 8001\n")
	writeFile(t, filepath.Join(dir, "prod.local.yaml"), "eiam: [unclosed\n")

	viper.Reset()
	confFile = base
	viper.SetConfigFile(base)
	require.NoError(t, viper.ReadInConfig())

	assert.Panics(t, mergeLocalOverride)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
