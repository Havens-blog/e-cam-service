#!/bin/bash

# 用户组成员查询 API 测试脚本

BASE_URL="http://localhost:8080"
TENANT_ID="tenant-001"

echo "=========================================="
echo "用户组成员查询 API 测试"
echo "=========================================="
echo ""

# 1. 查询用户组列表
echo "1. 查询用户组列表"
echo "------------------------------------------"
curl -s -X GET "${BASE_URL}/api/v1/cam/iam/groups?page=1&size=10" \
  -H "X-Tenant-ID: ${TENANT_ID}" | jq '.'
echo ""

# 2. 获取第一个用户组的ID
GROUP_ID=$(curl -s -X GET "${BASE_URL}/api/v1/cam/iam/groups?page=1&size=1" \
  -H "X-Tenant-ID: ${TENANT_ID}" | jq -r '.data.list[0].id')

if [ "$GROUP_ID" == "null" ] || [ -z "$GROUP_ID" ]; then
    echo "❌ 未找到用户组，请先创建用户组"
    exit 1
fi

echo "2. 查询用户组详情 (ID: ${GROUP_ID})"
echo "------------------------------------------"
curl -s -X GET "${BASE_URL}/api/v1/cam/iam/groups/${GROUP_ID}" \
  -H "X-Tenant-ID: ${TENANT_ID}" | jq '.'
echo ""

# 3. 查询用户组成员
echo "3. 查询用户组成员 (ID: ${GROUP_ID})"
echo "------------------------------------------"
MEMBERS=$(curl -s -X GET "${BASE_URL}/api/v1/cam/iam/groups/${GROUP_ID}/members" \
  -H "X-Tenant-ID: ${TENANT_ID}")

echo "$MEMBERS" | jq '.'
echo ""

# 4. 统计成员数量
MEMBER_COUNT=$(echo "$MEMBERS" | jq '.data | length')
echo "4. 成员统计"
echo "------------------------------------------"
echo "用户组 ID: ${GROUP_ID}"
echo "成员数量: ${MEMBER_COUNT}"
echo ""

# 5. 显示成员列表
if [ "$MEMBER_COUNT" -gt 0 ]; then
    echo "5. 成员列表"
    echo "------------------------------------------"
    echo "$MEMBERS" | jq -r '.data[] | "  - \(.username) (\(.display_name)) - Groups: \(.user_groups)"'
    echo ""
    echo "✅ 查询成功！找到 ${MEMBER_COUNT} 个成员"
else
    echo "⚠️  该用户组暂无成员"
fi

echo ""
echo "=========================================="
echo "测试完成"
echo "=========================================="
