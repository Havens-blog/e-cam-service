package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// SyncResult 同步结果
type SyncResult struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		TotalGroups   int `json:"total_groups"`
		CreatedGroups int `json:"created_groups"`
		UpdatedGroups int `json:"updated_groups"`
		FailedGroups  int `json:"failed_groups"`
		TotalMembers  int `json:"total_members"`
		SyncedMembers int `json:"synced_members"`
		FailedMembers int `json:"failed_members"`
	} `json:"data"`
}

// GroupListResult 用户组列表结果
type GroupListResult struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List  []Group `json:"list"`
		Total int64   `json:"total"`
	} `json:"data"`
}

// Group 用户组
type Group struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	GroupName   string `json:"group_name"`
	Description string `json:"description"`
	Provider    string `json:"provider"`
	MemberCount int    `json:"member_count"`
}

// UserListResult 用户列表结果
type UserListResult struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List  []User `json:"list"`
		Total int64  `json:"total"`
	} `json:"data"`
}

// User 用户
type User struct {
	ID          int64   `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Provider    string  `json:"provider"`
	UserGroups  []int64 `json:"user_groups"`
}

func main() {
	// 从环境变量获取配置
	baseURL := getEnv("API_BASE_URL", "http://localhost:8080")
	tenantID := getEnv("TENANT_ID", "tenant-001")
	cloudAccountID := getEnv("CLOUD_ACCOUNT_ID", "1")

	fmt.Println("=== 用户组成员同步测试 ===")
	fmt.Printf("API地址: %s\n", baseURL)
	fmt.Printf("租户ID: %s\n", tenantID)
	fmt.Printf("云账号ID: %s\n\n", cloudAccountID)

	// 1. 执行同步
	fmt.Println("步骤 1: 执行用户组同步...")
	syncResult, err := syncGroups(baseURL, tenantID, cloudAccountID)
	if err != nil {
		log.Fatalf("同步失败: %v", err)
	}

	fmt.Println("同步完成！")
	fmt.Printf("  用户组统计:\n")
	fmt.Printf("    - 总数: %d\n", syncResult.Data.TotalGroups)
	fmt.Printf("    - 新创建: %d\n", syncResult.Data.CreatedGroups)
	fmt.Printf("    - 已更新: %d\n", syncResult.Data.UpdatedGroups)
	fmt.Printf("    - 失败: %d\n", syncResult.Data.FailedGroups)
	fmt.Printf("  成员统计:\n")
	fmt.Printf("    - 总数: %d\n", syncResult.Data.TotalMembers)
	fmt.Printf("    - 已同步: %d\n", syncResult.Data.SyncedMembers)
	fmt.Printf("    - 失败: %d\n\n", syncResult.Data.FailedMembers)

	// 2. 查询用户组列表
	fmt.Println("步骤 2: 查询用户组列表...")
	groups, err := listGroups(baseURL, tenantID)
	if err != nil {
		log.Fatalf("查询用户组失败: %v", err)
	}

	fmt.Printf("共查询到 %d 个用户组:\n", len(groups))
	for i, group := range groups {
		fmt.Printf("  %d. %s (%s) - 成员数: %d\n",
			i+1, group.Name, group.Provider, group.MemberCount)
	}
	fmt.Println()

	// 3. 查询用户列表
	fmt.Println("步骤 3: 查询用户列表...")
	users, err := listUsers(baseURL, tenantID)
	if err != nil {
		log.Fatalf("查询用户失败: %v", err)
	}

	fmt.Printf("共查询到 %d 个用户:\n", len(users))
	for i, user := range users {
		fmt.Printf("  %d. %s (%s) - 所属用户组: %v\n",
			i+1, user.Username, user.Provider, user.UserGroups)
	}
	fmt.Println()

	// 4. 验证数据一致性
	fmt.Println("步骤 4: 验证数据一致性...")
	validateData(syncResult, groups, users)

	fmt.Println("\n=== 测试完成 ===")
}

// syncGroups 执行用户组同步
func syncGroups(baseURL, tenantID, cloudAccountID string) (*SyncResult, error) {
	url := fmt.Sprintf("%s/api/v1/cam/iam/groups/sync?cloud_account_id=%s", baseURL, cloudAccountID)

	req, err := http.NewRequestWithContext(context.Background(), "POST", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Tenant-ID", tenantID)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result SyncResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API返回错误: %s", result.Message)
	}

	return &result, nil
}

// listGroups 查询用户组列表
func listGroups(baseURL, tenantID string) ([]Group, error) {
	url := fmt.Sprintf("%s/api/v1/cam/iam/groups?page=1&size=100", baseURL)

	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Tenant-ID", tenantID)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result GroupListResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API返回错误: %s", result.Message)
	}

	return result.Data.List, nil
}

// listUsers 查询用户列表
func listUsers(baseURL, tenantID string) ([]User, error) {
	url := fmt.Sprintf("%s/api/v1/cam/iam/users?page=1&size=100", baseURL)

	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Tenant-ID", tenantID)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result UserListResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API返回错误: %s", result.Message)
	}

	return result.Data.List, nil
}

// validateData 验证数据一致性
func validateData(syncResult *SyncResult, groups []Group, users []User) {
	// 验证用户组数量
	if len(groups) != syncResult.Data.TotalGroups {
		fmt.Printf("  ⚠️  警告: 用户组数量不一致 (查询到: %d, 同步结果: %d)\n",
			len(groups), syncResult.Data.TotalGroups)
	} else {
		fmt.Printf("  ✓ 用户组数量一致: %d\n", len(groups))
	}

	// 统计用户的用户组关联数
	totalMemberships := 0
	for _, user := range users {
		totalMemberships += len(user.UserGroups)
	}

	fmt.Printf("  ✓ 用户数量: %d\n", len(users))
	fmt.Printf("  ✓ 用户组关联总数: %d\n", totalMemberships)

	// 检查是否有失败的同步
	if syncResult.Data.FailedGroups > 0 {
		fmt.Printf("  ⚠️  警告: %d 个用户组同步失败\n", syncResult.Data.FailedGroups)
	}
	if syncResult.Data.FailedMembers > 0 {
		fmt.Printf("  ⚠️  警告: %d 个成员同步失败\n", syncResult.Data.FailedMembers)
	}

	if syncResult.Data.FailedGroups == 0 && syncResult.Data.FailedMembers == 0 {
		fmt.Println("  ✓ 所有数据同步成功")
	}
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
