//go:build ignore
// +build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

type SwaggerDoc struct {
	Paths map[string]map[string]interface{} `json:"paths"`
	Tags  []map[string]interface{}          `json:"tags"`
}

func main() {
	fmt.Println("📊 统计 Swagger API 数量")
	fmt.Println("=====================================\n")

	// 读取 swagger.json 文件
	data, err := ioutil.ReadFile("docs/swagger.json")
	if err != nil {
		fmt.Printf("❌ 读取 swagger.json 失败: %v\n", err)
		return
	}

	var doc SwaggerDoc
	err = json.Unmarshal(data, &doc)
	if err != nil {
		fmt.Printf("❌ 解析 swagger.json 失败: %v\n", err)
		return
	}

	// 统计 API 数量
	totalAPIs := 0
	methodCount := make(map[string]int)
	tagCount := make(map[string]int)

	for path, methods := range doc.Paths {
		fmt.Printf("📍 路径: %s\n", path)
		for method, details := range methods {
			totalAPIs++
			methodCount[method]++

			if detailMap, ok := details.(map[string]interface{}); ok {
				if tags, exists := detailMap["tags"]; exists {
					if tagList, ok := tags.([]interface{}); ok && len(tagList) > 0 {
						if tag, ok := tagList[0].(string); ok {
							tagCount[tag]++
							fmt.Printf("  └─ %s %s [%s]\n", method, detailMap["summary"], tag)
						}
					}
				}
			}
		}
		fmt.Println()
	}

	fmt.Println("=====================================")
	fmt.Printf("✅ 总计 API 数量: %d\n\n", totalAPIs)

	fmt.Println("📈 按 HTTP 方法统计:")
	for method, count := range methodCount {
		fmt.Printf("  %s: %d 个\n", method, count)
	}

	fmt.Println("\n🏷️  按标签分类统计:")
	for tag, count := range tagCount {
		fmt.Printf("  %s: %d 个 API\n", tag, count)
	}

	fmt.Println("\n=====================================")
	fmt.Println("🌐 访问 Swagger 文档:")
	fmt.Println("   http://localhost:8001/swagger/index.html")
}
