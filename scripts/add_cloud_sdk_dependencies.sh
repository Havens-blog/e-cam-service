#!/bin/bash

# 添加华为云和腾讯云 SDK 依赖

echo "Adding Huawei Cloud SDK..."
go get github.com/huaweicloud/huaweicloud-sdk-go-v3/services/iam/v3

echo "Adding Tencent Cloud SDK..."
go get github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cam/v20190116
go get github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common

echo "Tidying go.mod..."
go mod tidy

echo "Done! SDK dependencies added successfully."
