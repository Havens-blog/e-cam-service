//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	fmt.Println("🔍 测试 Swagger 文档访问")
	fmt.Println("=====================================\n")

	// 等待服务启动
	fmt.Println("⏳ 等待服务启动...")
	time.Sleep(2 * time.Second)

	// 测试 Swagger 相关路由
	routes := []struct {
		url         string
		description string
	}{
		{"http://localhost:8001/swagger/index.html", "Swagger UI 界面"},
		{"http://localhost:8001/swagger/doc.json", "Swagger JSON 文档"},
		{"http://localhost:8001/docs", "文档重定向"},
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	for _, route := range routes {
		fmt.Printf("📡 测试: %s\n", route.description)
		fmt.Printf("🔗 URL: %s\n", route.url)

		resp, err := client.Get(route.url)
		if err != nil {
			fmt.Printf("❌ 请求失败: %v\n\n", err)
			continue
		}
		defer resp.Body.Close()

		fmt.Printf("✅ 状态码: %d\n", resp.StatusCode)
		fmt.Printf("📄 Content-Type: %s\n", resp.Header.Get("Content-Type"))

		// 如果是 JSON 文档，显示部分内容
		if route.url == "http://localhost:8001/swagger/doc.json" && resp.StatusCode == 200 {
			body, err := io.ReadAll(resp.Body)
			if err == nil && len(body) > 0 {
				content := string(body)
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				fmt.Printf("📋 内容预览: %s\n", content)
			}
		}

		fmt.Println()
	}

	fmt.Println("=====================================")
	fmt.Println("✅ Swagger 访问测试完成！")
	fmt.Println("\n💡 如果测试失败，请确保:")
	fmt.Println("   1. 服务正在运行: ./e-cam-service.exe start -f config/prod.yaml")
	fmt.Println("   2. 端口 8001 未被占用")
	fmt.Println("   3. 配置文件正确")
	fmt.Println("\n🌐 浏览器访问:")
	fmt.Println("   http://localhost:8001/swagger/index.html")
}
