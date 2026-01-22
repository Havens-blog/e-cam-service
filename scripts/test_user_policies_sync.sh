#!/bin/bash

# 用户个人权限同步测试脚本

BASE_URL="http://localhost:8080"
TENANT_ID="tenant-001"

echo "=========================================="
echo "用户个人权限同步测试"
echo "=========================================="
echo ""

# 1. 获取云账号列表
echo "1. 获取云账号列表"
echo "------------------------------------------"
ACCOUNT_ID=$(curl -s -X GET "${BASE_URL}/api/v1/cam/accounts?page=1&size=1" \
  -H "X-Tenant-ID: ${TENANT_ID}" | jq -r '.data.list[0].id')

if [ "$ACCOUNT_ID" == "null" ] || [ -z "$ACCOUNT_ID" ]; then
    echo "❌ 未找到云账号，请先添加云账号"
    exit 1
fi

echo "云账号 ID: ${ACCOUNT_ID}"
echo ""

# 2. 同步用户
echo "2. 同步云平台用户"
echo "------------------------------------------"
SYNC_RESULT=$(curl -s -X POST "${BASE_URL}/api/v1/cam/iam/users/sync?cloud_account_id=${ACCOUNT_ID}" \
  -H "X-Tenant-ID: ${TENANT_ID}")

echo "$SYNC_RESULT" | jq '.'
echo ""

# 3. 获取同步结果统计
TOTAL=$(echo "$SYNC_RESULT" | jq -r '.data.total_count')
ADDED=$(echo "$SYNC_RESULT" | jq -r '.data.added_count')
UPDATED=$(echo "$SYNC_RESULT" | jq -r '.data.updated_count')

echo "同步统计:"
echo "  总数: ${TOTAL}"
echo "  新增: ${ADDED}"
echo "  更新: ${UPDATED}"
echo ""

# 4. 查询用户列表
echo "3. 查询用户列表"
echo "------------------------------------------"
USERS=$(curl -s -X GET "${BASE_URL}/api/v1/cam/iam/users?page=1&size=5" \
  -H "X-Tenant-ID: ${TENANT_ID}")

echo "$USERS" | jq '.'
echo ""

# 5. 获取第一个用户的详情
USER_ID=$(echo "$USERS" | jq -r '.data.list[0].id')

if [ "$USER_ID" == "null" ] || [ -z "$USER_ID" ]; then
    echo "❌ 未找到用户"
    exit 1
fi

echo "4. 查询用户详情 (ID: ${USER_ID})"
echo "------------------------------------------"
USER_DETAIL=$(curl -s -X GET "${BASE_URL}/api/v1/cam/iam/users/${USER_ID}" \
  -H "X-Tenant-ID: ${TENANT_ID}")

echo "$USER_DETAIL" | jq '.'
echo ""

# 6. 检查个人权限
POLICY_COUNT=$(echo "$USER_DETAIL" | jq '.data.policies | length')
echo "5. 个人权限统计"
echo "------------------------------------------"
echo "用户 ID: ${USER_ID}"
echo "用户名: $(echo "$USER_DETAIL" | jq -r '.data.username')"
echo "个人权限数量: ${POLICY_COUNT}"
echo ""

if [ "$POLICY_COUNT" -gt 0 ]; then
    echo "个人权限列表:"
    echo "$USER_DETAIL" | jq -r '.data.policies[] | "  - \(.policy_name) (\(.policy_type))"'
    echo ""
    echo "✅ 用户个人权限同步成功！"
else
    echo "⚠️  该用户暂无个人权限（可能是正常情况）"
fi

echo ""
echo "=========================================="
echo "测试完成"
echo "=========================================="
