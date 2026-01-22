#!/bin/bash

echo "测试 IAM 路由..."
echo ""

echo "1. 测试用户列表 GET /api/v1/cam/iam/users"
curl -X GET "http://localhost:8081/api/v1/cam/iam/users" -w "\nHTTP Status: %{http_code}\n\n"

echo "2. 测试用户同步 POST /api/v1/cam/iam/users/sync"
curl -X POST "http://localhost:8081/api/v1/cam/iam/users/sync?cloud_account_id=2" -w "\nHTTP Status: %{http_code}\n\n"

echo "3. 测试权限组列�?GET /api/v1/cam/iam/groups"
curl -X GET "http://localhost:8081/api/v1/cam/iam/groups" -w "\nHTTP Status: %{http_code}\n\n"

echo "4. 测试模板列表 GET /api/v1/cam/iam/templates"
curl -X GET "http://localhost:8081/api/v1/cam/iam/templates" -w "\nHTTP Status: %{http_code}\n\n"

echo "5. 测试审计日志 GET /api/v1/cam/iam/audit/logs"
curl -X GET "http://localhost:8081/api/v1/cam/iam/audit/logs" -w "\nHTTP Status: %{http_code}\n\n"

echo "6. 测试同步任务 GET /api/v1/cam/iam/sync/tasks"
curl -X GET "http://localhost:8081/api/v1/cam/iam/sync/tasks" -w "\nHTTP Status: %{http_code}\n\n"
