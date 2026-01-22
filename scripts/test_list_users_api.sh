#!/bin/bash

# 测试用户列表 API
# 使用方法: ./test_list_users_api.sh

API_BASE_URL="${API_BASE_URL:-http://localhost:8080}"
TENANT_ID="${TENANT_ID:-tenant-001}"

echo "=== 测试用户列表 API ==="
echo "API地址: $API_BASE_URL"
echo "租户ID: $TENANT_ID"
echo ""

# 测试 1: 不带任何参数
echo "测试 1: 不带任何参数"
curl -s -X GET "$API_BASE_URL/api/v1/cam/iam/users" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" | jq '.'
echo ""

# 测试 2: 带分页参数
echo "测试 2: 带分页参数"
curl -s -X GET "$API_BASE_URL/api/v1/cam/iam/users?page=1&size=10" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" | jq '.'
echo ""

# 测试 3: 不带租户ID（应该失败或返回空）
echo "测试 3: 不带租户ID"
curl -s -X GET "$API_BASE_URL/api/v1/cam/iam/users?page=1&size=10" \
  -H "Content-Type: application/json" | jq '.'
echo ""

# 测试 4: 查询用户组列表（对比）
echo "测试 4: 查询用户组列表（对比）"
curl -s -X GET "$API_BASE_URL/api/v1/cam/iam/groups?page=1&size=10" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" | jq '.'
echo ""

echo "=== 测试完成 ==="
