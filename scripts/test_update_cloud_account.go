package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// UpdateCloudAccountReq 更新云账号请求
type UpdateCloudAccountReq struct {
	Name            *string               `json:"name,omitempty"`
	Environment     *string               `json:"environment,omitempty"`
	AccessKeyID     *string               `json:"access_key_id,omitempty"`
	AccessKeySecret *string               `json:"access_key_secret,omitempty"`
	Regions         []string              `json:"regions,omitempty"`
	Description     *string               `json:"description,omitempty"`
	Config          *CloudAccountConfigVO `json:"config,omitempty"`
}

// CloudAccountConfigVO 云账号配置
type CloudAccountConfigVO struct {
	EnableAutoSync       bool     `json:"enable_auto_sync"`
	SyncInterval         int64    `json:"sync_interval"`
	ReadOnly             bool     `json:"read_only"`
	ShowSubAccounts      bool     `json:"show_sub_accounts"`
	EnableCostMonitoring bool     `json:"enable_cost_monitoring"`
	SupportedRegions     []string `json:"supported_regions"`
	SupportedAssetTypes  []string `json:"supported_asset_types"`
}

type Result struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

func main() {
	baseURL := "http://localhost:8080"
	accountID := int64(1) // 替换为实际的账号ID

	// 测试1: 更新账号名称和描述
	fmt.Println("=== 测试1: 更新账号名称和描述 ===")
	name := "更新后的账号名称"
	desc := "更新后的描述信息"
	req1 := UpdateCloudAccountReq{
		Name:        &name,
		Description: &desc,
	}
	if err := updateCloudAccount(baseURL, accountID, req1); err != nil {
		fmt.Printf("❌ 更新失败: %v\n", err)
	} else {
		fmt.Printf("✓ 更新成功\n")
	}
	fmt.Println()

	// 测试2: 更新区域列表
	fmt.Println("=== 测试2: 更新区域列表 ===")
	req2 := UpdateCloudAccountReq{
		Regions: []string{"cn-hangzhou", "cn-beijing", "cn-shanghai", "cn-shenzhen"},
	}
	if err := updateCloudAccount(baseURL, accountID, req2); err != nil {
		fmt.Printf("❌ 更新失败: %v\n", err)
	} else {
		fmt.Printf("✓ 更新成功\n")
	}
	fmt.Println()

	// 测试3: 更新环境
	fmt.Println("=== 测试3: 更新环境 ===")
	env := "production"
	req3 := UpdateCloudAccountReq{
		Environment: &env,
	}
	if err := updateCloudAccount(baseURL, accountID, req3); err != nil {
		fmt.Printf("❌ 更新失败: %v\n", err)
	} else {
		fmt.Printf("✓ 更新成功\n")
	}
	fmt.Println()

	// 测试4: 更新 AccessKey
	fmt.Println("=== 测试4: 更新 AccessKey ===")
	newAccessKeyID := "LTAI5tNewAccessKey123456"
	newAccessKeySecret := "NewSecretKey1234567890abcdef"
	req4 := UpdateCloudAccountReq{
		AccessKeyID:     &newAccessKeyID,
		AccessKeySecret: &newAccessKeySecret,
	}
	if err := updateCloudAccount(baseURL, accountID, req4); err != nil {
		fmt.Printf("❌ 更新失败: %v\n", err)
	} else {
		fmt.Printf("✓ 更新成功\n")
	}
	fmt.Println()

	// 测试5: 更新配置
	fmt.Println("=== 测试5: 更新配置 ===")
	req5 := UpdateCloudAccountReq{
		Config: &CloudAccountConfigVO{
			EnableAutoSync:       true,
			SyncInterval:         600,
			ReadOnly:             false,
			ShowSubAccounts:      true,
			EnableCostMonitoring: true,
			SupportedRegions:     []string{"cn-hangzhou", "cn-beijing", "cn-shanghai"},
			SupportedAssetTypes:  []string{"ecs", "rds", "oss", "vpc"},
		},
	}
	if err := updateCloudAccount(baseURL, accountID, req5); err != nil {
		fmt.Printf("❌ 更新失败: %v\n", err)
	} else {
		fmt.Printf("✓ 更新成功\n")
	}
	fmt.Println()

	// 测试6: 批量更新多个字段
	fmt.Println("=== 测试6: 批量更新多个字段 ===")
	batchName := "集团-阿里云"
	batchEnv := "production"
	batchDesc := "集团-阿里云账号"
	req6 := UpdateCloudAccountReq{
		Name:        &batchName,
		Environment: &batchEnv,
		Regions:     []string{"eu-west-1", "cn-hangzhou", "cn-shenzhen"},
		Description: &batchDesc,
		Config: &CloudAccountConfigVO{
			EnableAutoSync:       true,
			SyncInterval:         300,
			ReadOnly:             false,
			ShowSubAccounts:      true,
			EnableCostMonitoring: true,
		},
	}
	if err := updateCloudAccount(baseURL, accountID, req6); err != nil {
		fmt.Printf("❌ 更新失败: %v\n", err)
	} else {
		fmt.Printf("✓ 更新成功\n")
	}

	fmt.Println("\n=== 所有测试完成 ===")
}

func updateCloudAccount(baseURL string, id int64, req UpdateCloudAccountReq) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	client := &http.Client{}
	httpReq, err := http.NewRequest(
		"PUT",
		fmt.Sprintf("%s/api/v1/cam/cloud-accounts/%d", baseURL, id),
		bytes.NewBuffer(data),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}

	if result.Code != 0 {
		return fmt.Errorf("API 返回错误: %s", result.Msg)
	}

	return nil
}
