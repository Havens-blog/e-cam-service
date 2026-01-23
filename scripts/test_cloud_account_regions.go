//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CreateCloudAccountReq 创建云账号请�?
type CreateCloudAccountReq struct {
	Name            string               `json:"name"`
	Provider        string               `json:"provider"`
	Environment     string               `json:"environment"`
	AccessKeyID     string               `json:"access_key_id"`
	AccessKeySecret string               `json:"access_key_secret"`
	Regions         []string             `json:"regions"` // 支持多个区域
	Description     string               `json:"description"`
	Config          CloudAccountConfigVO `json:"config"`
	TenantID        string               `json:"tenant_id"`
}

// CloudAccountConfigVO 云账号配�?
type CloudAccountConfigVO struct {
	EnableAutoSync       bool     `json:"enable_auto_sync"`
	SyncInterval         int64    `json:"sync_interval"`
	ReadOnly             bool     `json:"read_only"`
	ShowSubAccounts      bool     `json:"show_sub_accounts"`
	EnableCostMonitoring bool     `json:"enable_cost_monitoring"`
	SupportedRegions     []string `json:"supported_regions"`
	SupportedAssetTypes  []string `json:"supported_asset_types"`
}

// CloudAccount 云账号响�?
type CloudAccount struct {
	ID              int64                `json:"id"`
	Name            string               `json:"name"`
	Provider        string               `json:"provider"`
	Environment     string               `json:"environment"`
	AccessKeyID     string               `json:"access_key_id"`
	Regions         []string             `json:"regions"` // 支持多个区域
	Description     string               `json:"description"`
	Status          string               `json:"status"`
	Config          CloudAccountConfigVO `json:"config"`
	TenantID        string               `json:"tenant_id"`
	LastSyncTime    *time.Time           `json:"last_sync_time"`
	LastTestTime    *time.Time           `json:"last_test_time"`
	AssetCount      int64                `json:"asset_count"`
	ErrorMessage    string               `json:"error_message"`
	CreateTime      time.Time            `json:"create_time"`
	UpdateTime      time.Time            `json:"update_time"`
}

type Result struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

func main() {
	baseURL := "http://localhost:8080"

	// 测试1: 创建支持多个区域的云账号
	fmt.Println("=== 测试1: 创建支持多个区域的云账号 ===")
	createReq := CreateCloudAccountReq{
		Name:            "测试多区域账�?,
		Provider:        "aliyun",
		Environment:     "development",
		AccessKeyID:     "LTAI5tTestAccessKey123456",
		AccessKeySecret: "TestSecretKey1234567890abcdef",
		Regions:         []string{"cn-hangzhou", "cn-beijing", "cn-shanghai"}, // 多个区域
		Description:     "测试支持多个区域的云账号",
		Config: CloudAccountConfigVO{
			EnableAutoSync:       true,
			SyncInterval:         300,
			ReadOnly:             false,
			ShowSubAccounts:      true,
			EnableCostMonitoring: true,
			SupportedRegions:     []string{"cn-hangzhou", "cn-beijing", "cn-shanghai", "cn-shenzhen"},
			SupportedAssetTypes:  []string{"ecs", "rds", "oss"},
		},
		TenantID: "tenant_test_001",
	}

	accountID, err := createCloudAccount(baseURL, createReq)
	if err != nil {
		fmt.Printf("创建云账号失�? %v\n", err)
		return
	}
	fmt.Printf("�?创建成功，账号ID: %d\n\n", accountID)

	// 测试2: 获取云账号详情，验证 regions 字段
	fmt.Println("=== 测试2: 获取云账号详�?===")
	account, err := getCloudAccount(baseURL, accountID)
	if err != nil {
		fmt.Printf("获取云账号失�? %v\n", err)
		return
	}
	fmt.Printf("�?账号名称: %s\n", account.Name)
	fmt.Printf("�?云厂�? %s\n", account.Provider)
	fmt.Printf("�?支持的区�? %v\n", account.Regions)
	fmt.Printf("�?区域数量: %d\n\n", len(account.Regions))

	// 测试3: 创建单个区域的云账号
	fmt.Println("=== 测试3: 创建单个区域的云账号 ===")
	singleRegionReq := CreateCloudAccountReq{
		Name:            "测试单区域账�?,
		Provider:        "aws",
		Environment:     "production",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		AccessKeySecret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Regions:         []string{"us-west-2"}, // 单个区域
		Description:     "测试单个区域的云账号",
		Config: CloudAccountConfigVO{
			EnableAutoSync:       false,
			SyncInterval:         600,
			ReadOnly:             true,
			ShowSubAccounts:      false,
			EnableCostMonitoring: false,
			SupportedRegions:     []string{"us-west-2", "us-east-1"},
			SupportedAssetTypes:  []string{"ec2", "s3"},
		},
		TenantID: "tenant_test_002",
	}

	accountID2, err := createCloudAccount(baseURL, singleRegionReq)
	if err != nil {
		fmt.Printf("创建云账号失�? %v\n", err)
		return
	}
	fmt.Printf("�?创建成功，账号ID: %d\n\n", accountID2)

	// 测试4: 列出所有云账号
	fmt.Println("=== 测试4: 列出所有云账号 ===")
	accounts, err := listCloudAccounts(baseURL)
	if err != nil {
		fmt.Printf("列出云账号失�? %v\n", err)
		return
	}
	fmt.Printf("�?共找�?%d 个云账号\n", len(accounts))
	for i, acc := range accounts {
		fmt.Printf("  %d. %s (%s) - 区域: %v\n", i+1, acc.Name, acc.Provider, acc.Regions)
	}

	fmt.Println("\n=== 所有测试完�?===")
}

func createCloudAccount(baseURL string, req CreateCloudAccountReq) (int64, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}

	resp, err := http.Post(
		baseURL+"/api/v1/cam/cloud-accounts",
		"application/json",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Code int `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if result.Code != 0 {
		return 0, fmt.Errorf("API 返回错误: %s", result.Msg)
	}

	return result.Data.ID, nil
}

func getCloudAccount(baseURL string, id int64) (*CloudAccount, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/cam/cloud-accounts/%d", baseURL, id))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code int           `json:"code"`
		Msg  string        `json:"msg"`
		Data *CloudAccount `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API 返回错误: %s", result.Msg)
	}

	return result.Data, nil
}

func listCloudAccounts(baseURL string) ([]*CloudAccount, error) {
	resp, err := http.Get(baseURL + "/api/v1/cam/cloud-accounts?limit=100")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Accounts []*CloudAccount `json:"accounts"`
			Total    int64           `json:"total"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API 返回错误: %s", result.Msg)
	}

	return result.Data.Accounts, nil
}
