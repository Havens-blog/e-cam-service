//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const baseURL = "http://localhost:8001/api/v1/cam"

func main() {
	fmt.Println("=== 测试异步任务框架 ===\n")

	// 1. 提交同步资产任务
	fmt.Println("1. 提交同步资产任务...")
	taskID, err := submitSyncAssetsTask()
	if err != nil {
		log.Fatal("提交任务失败:", err)
	}
	fmt.Printf("�?任务已提交，任务ID: %s\n\n", taskID)

	// 2. 轮询任务状�?
	fmt.Println("2. 轮询任务状�?..")
	for i := 0; i < 30; i++ {
		task, err := getTaskStatus(taskID)
		if err != nil {
			log.Printf("获取任务状态失�? %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		fmt.Printf("   状�? %s, 进度: %d%%, 消息: %s\n",
			task["status"], int(task["progress"].(float64)), task["message"])

		status := task["status"].(string)
		if status == "completed" {
			fmt.Println("\n�?任务执行完成�?)
			fmt.Println("\n任务结果:")
			if result, ok := task["result"].(map[string]interface{}); ok {
				resultJSON, _ := json.MarshalIndent(result, "  ", "  ")
				fmt.Printf("  %s\n", string(resultJSON))
			}
			break
		} else if status == "failed" {
			fmt.Printf("\n�?任务执行失败: %s\n", task["error"])
			break
		}

		time.Sleep(2 * time.Second)
	}

	// 3. 获取任务列表
	fmt.Println("\n3. 获取任务列表...")
	tasks, err := listTasks()
	if err != nil {
		log.Printf("获取任务列表失败: %v\n", err)
	} else {
		fmt.Printf("�?找到 %d 个任务\n", len(tasks))
		for i, t := range tasks {
			if i >= 5 {
				break
			}
			fmt.Printf("  %d. ID: %s, 类型: %s, 状�? %s\n",
				i+1, t["id"], t["type"], t["status"])
		}
	}

	fmt.Println("\n=== 测试完成 ===")
}

func submitSyncAssetsTask() (string, error) {
	reqBody := map[string]interface{}{
		"provider":    "aliyun",
		"asset_types": []string{"ecs"},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(
		baseURL+"/tasks/sync-assets",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result["code"].(float64) != 0 {
		return "", fmt.Errorf("API错误: %s", result["msg"])
	}

	data := result["data"].(map[string]interface{})
	return data["task_id"].(string), nil
}

func getTaskStatus(taskID string) (map[string]interface{}, error) {
	resp, err := http.Get(baseURL + "/tasks/" + taskID)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result["code"].(float64) != 0 {
		return nil, fmt.Errorf("API错误: %s", result["msg"])
	}

	return result["data"].(map[string]interface{}), nil
}

func listTasks() ([]map[string]interface{}, error) {
	resp, err := http.Get(baseURL + "/tasks?limit=10")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result["code"].(float64) != 0 {
		return nil, fmt.Errorf("API错误: %s", result["msg"])
	}

	data := result["data"].(map[string]interface{})
	tasksData := data["tasks"].([]interface{})

	tasks := make([]map[string]interface{}, len(tasksData))
	for i, t := range tasksData {
		tasks[i] = t.(map[string]interface{})
	}

	return tasks, nil
}
