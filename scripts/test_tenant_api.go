package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const baseURL = "http://localhost:9001/api/v1/cam/iam"

type CreateTenantRequest struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name"`
	Description string                 `json:"description"`
	Settings    map[string]interface{} `json:"settings"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type Response struct {
	Code int                    `json:"code"`
	Msg  string                 `json:"msg"`
	Data map[string]interface{} `json:"data"`
}

func main() {
	fmt.Println("=== 租户管理API测试 ===\n")

	// 1. 创建租户
	fmt.Println("1. 测试创建租户...")
	tenant := CreateTenantRequest{
		ID:          fmt.Sprintf("test_tenant_%d", time.Now().Unix()),
		Name:        fmt.Sprintf("测试租户_%d", time.Now().Unix()),
		DisplayName: "测试租户显示名称",
		Description: "这是一个测试租户",
		Settings: map[string]interface{}{
			"max_cloud_accounts": 10,
			"max_users":          100,
			"max_user_groups":    50,
			"features": map[string]bool{
				"auto_sync": true,
			},
		},
		Metadata: map[string]interface{}{
			"owner":         "admin@test.com",
			"contact_email": "contact@test.com",
			"company_name":  "测试公司",
			"industry":      "Technology",
			"region":        "China",
		},
	}

	tenantID, err := createTenant(tenant)
	if err != nil {
		fmt.Printf("❌ 创建租户失败: %v\n", err)
		return
	}
	fmt.Printf("✅ 创建租户成功，ID: %s\n\n", tenantID)

	// 2. 获取租户详情
	fmt.Println("2. 测试获取租户详情...")
	err = getTenant(tenantID)
	if err != nil {
		fmt.Printf("❌ 获取租户详情失败: %v\n", err)
		return
	}
	fmt.Println("✅ 获取租户详情成功\n")

	// 3. 查询租户列表
	fmt.Println("3. 测试查询租户列表...")
	err = listTenants()
	if err != nil {
		fmt.Printf("❌ 查询租户列表失败: %v\n", err)
		return
	}
	fmt.Println("✅ 查询租户列表成功\n")

	// 4. 更新租户
	fmt.Println("4. 测试更新租户...")
	err = updateTenant(tenantID)
	if err != nil {
		fmt.Printf("❌ 更新租户失败: %v\n", err)
		return
	}
	fmt.Println("✅ 更新租户成功\n")

	// 5. 获取租户统计
	fmt.Println("5. 测试获取租户统计...")
	err = getTenantStats(tenantID)
	if err != nil {
		fmt.Printf("❌ 获取租户统计失败: %v\n", err)
		return
	}
	fmt.Println("✅ 获取租户统计成功\n")

	// 6. 删除租户
	fmt.Println("6. 测试删除租户...")
	err = deleteTenant(tenantID)
	if err != nil {
		fmt.Printf("❌ 删除租户失败: %v\n", err)
		return
	}
	fmt.Println("✅ 删除租户成功\n")

	fmt.Println("=== 所有测试通过 ✅ ===")
}

func createTenant(req CreateTenantRequest) (string, error) {
	data, _ := json.Marshal(req)
	resp, err := http.Post(baseURL+"/tenants", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result Response
	json.Unmarshal(body, &result)

	if result.Code != 0 {
		return "", fmt.Errorf("API返回错误: %s", result.Msg)
	}

	if tenant, ok := result.Data["id"]; ok {
		return tenant.(string), nil
	}
	return req.ID, nil
}

func getTenant(tenantID string) error {
	resp, err := http.Get(baseURL + "/tenants/" + tenantID)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result Response
	json.Unmarshal(body, &result)

	if result.Code != 0 {
		return fmt.Errorf("API返回错误: %s", result.Msg)
	}

	fmt.Printf("   租户信息: %+v\n", result.Data)
	return nil
}

func listTenants() error {
	resp, err := http.Get(baseURL + "/tenants?page=1&size=10")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result Response
	json.Unmarshal(body, &result)

	if result.Code != 0 {
		return fmt.Errorf("API返回错误: %s", result.Msg)
	}

	fmt.Printf("   查询到 %v 个租户\n", result.Data["total"])
	return nil
}

func updateTenant(tenantID string) error {
	updateData := map[string]interface{}{
		"description": "更新后的描述",
	}
	data, _ := json.Marshal(updateData)

	req, _ := http.NewRequest("PUT", baseURL+"/tenants/"+tenantID, bytes.NewBuffer(data))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result Response
	json.Unmarshal(body, &result)

	if result.Code != 0 {
		return fmt.Errorf("API返回错误: %s", result.Msg)
	}

	return nil
}

func getTenantStats(tenantID string) error {
	resp, err := http.Get(baseURL + "/tenants/" + tenantID + "/stats")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result Response
	json.Unmarshal(body, &result)

	if result.Code != 0 {
		return fmt.Errorf("API返回错误: %s", result.Msg)
	}

	fmt.Printf("   统计信息: %+v\n", result.Data)
	return nil
}

func deleteTenant(tenantID string) error {
	req, _ := http.NewRequest("DELETE", baseURL+"/tenants/"+tenantID, nil)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result Response
	json.Unmarshal(body, &result)

	if result.Code != 0 {
		return fmt.Errorf("API返回错误: %s", result.Msg)
	}

	return nil
}
