package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"
)

// Config holds all configuration for the APM push script, read from environment variables.
type Config struct {
	ARMSAccessKeyID     string // ARMS API AccessKey ID
	ARMSAccessKeySecret string // ARMS API AccessKey Secret
	ARMSRegionID        string // ARMS region (e.g. cn-hangzhou)
	TopologyAPIURL      string // Topology declaration endpoint URL
	K8sClusterName      string // K8s cluster identifier
	TenantID            string // Tenant ID
}

// sensitiveKeys 需要 base64 解码的敏感字段
// .env 中这些字段的值必须是 base64 编码后的字符串
// K8s 环境中直接传明文环境变量（Secret 已经做了 base64）
var sensitiveKeys = map[string]bool{
	"ARMS_ACCESS_KEY_ID":     true,
	"ARMS_ACCESS_KEY_SECRET": true,
}

// LoadConfig loads .env file (if present), then reads configuration from environment variables.
// .env 中的敏感字段（AccessKey）使用 base64 编码存储，加载时自动解码。
func LoadConfig() (*Config, error) {
	// 尝试加载 .env 文件（本地调试用，K8s 环境中不存在此文件）
	loadEnvFile(".env")

	cfg := &Config{
		ARMSAccessKeyID:     os.Getenv("ARMS_ACCESS_KEY_ID"),
		ARMSAccessKeySecret: os.Getenv("ARMS_ACCESS_KEY_SECRET"),
		ARMSRegionID:        os.Getenv("ARMS_REGION_ID"),
		TopologyAPIURL:      os.Getenv("TOPOLOGY_API_URL"),
		K8sClusterName:      os.Getenv("K8S_CLUSTER_NAME"),
		TenantID:            os.Getenv("TENANT_ID"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks that all required configuration fields are set.
func (c *Config) Validate() error {
	required := map[string]string{
		"ARMS_ACCESS_KEY_ID":     c.ARMSAccessKeyID,
		"ARMS_ACCESS_KEY_SECRET": c.ARMSAccessKeySecret,
		"ARMS_REGION_ID":         c.ARMSRegionID,
		"TOPOLOGY_API_URL":       c.TopologyAPIURL,
		"K8S_CLUSTER_NAME":       c.K8sClusterName,
		"TENANT_ID":              c.TenantID,
	}
	for name, val := range required {
		if val == "" {
			return fmt.Errorf("required environment variable %s is not set", name)
		}
	}
	return nil
}

// loadEnvFile reads a .env file and sets environment variables.
// Sensitive keys (defined in sensitiveKeys) are base64-decoded before setting.
// Only sets variables that are not already set in the environment (env vars take precedence).
// Silently ignores if file doesn't exist.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // File doesn't exist, skip
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// 敏感字段：base64 解码
		if sensitiveKeys[key] {
			decoded, err := base64.StdEncoding.DecodeString(value)
			if err != nil {
				log.Printf("WARN: failed to base64 decode %s, using raw value: %v", key, err)
			} else {
				value = string(decoded)
			}
		}

		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
