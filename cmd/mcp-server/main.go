package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Havens-blog/e-cam-service/internal/mcp"
	"github.com/Havens-blog/e-cam-service/pkg/crypto"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
)

func main() {
	// 加载配置
	configFile := "config/prod.yaml"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	viper.SetConfigFile(configFile)
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "读取配置文件失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化加密器
	encryptionKey := viper.GetString("security.encryption_key")
	if err := crypto.InitDefaultCrypto(encryptionKey); err != nil {
		fmt.Fprintf(os.Stderr, "加密组件初始化失败: %v\n", err)
	}

	// 初始化 MCP Server 依赖
	deps, err := mcp.InitDependencies()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化依赖失败: %v\n", err)
		os.Exit(1)
	}
	defer deps.Close()

	// 创建 MCP Server
	srv := mcp.NewServer(deps)

	// 优雅退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		elog.Info("收到退出信号，正在关闭 MCP Server...")
		cancel()
	}()

	// 启动 stdio 传输
	if err := srv.ServeStdio(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "MCP Server 运行失败: %v\n", err)
		os.Exit(1)
	}
}
