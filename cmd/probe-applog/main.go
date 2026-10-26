// Command probe-applog 临时诊断（不进 CI）：用平台自身云账号（解密链复用
// internal/account/repository）实测阿里 SLS 指定 project 的 logstore 清单与
// app 业务日志字段 schema，为 logquery「应用日志」类型的映射设计提供实测依据。
//
// 凭证只经内存解密后用于 SLS 调用，不打印不落盘；日志样本仅输出字段名与
// 截断值。用法：
//
//	go run ./cmd/probe-applog -f config/prod.yaml -tenant 1 \
//	  -region cn-shenzhen -project jlc-lb-log
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/account/repository"
	accountdao "github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	"github.com/Havens-blog/e-cam-service/ioc"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-common-go/crypto"
	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/spf13/viper"
)

func fatal(f string, a ...any) { fmt.Fprintf(os.Stderr, "[probe] "+f+"\n", a...); os.Exit(1) }

func main() {
	var (
		confFile     string
		tenant       int64
		region       string
		project      string
		stores       string
		listProjects bool
	)
	flag.StringVar(&confFile, "f", "config/prod.yaml", "service config file")
	flag.Int64Var(&tenant, "tenant", 1, "tenant id owning the cloud accounts")
	flag.StringVar(&region, "region", "cn-shenzhen", "SLS region")
	flag.StringVar(&project, "project", "jlc-lb-log", "SLS project to enumerate")
	flag.StringVar(&stores, "stores", "", "comma-separated explicit logstores to sample (skips -business detection)")
	flag.BoolVar(&listProjects, "list-projects", false, "only enumerate all SLS projects of the account")
	flag.Parse()

	viper.SetConfigFile(confFile)
	if err := viper.ReadInConfig(); err != nil {
		fatal("read config: %v", err)
	}
	if err := crypto.InitDefaultCrypto(viper.GetString("security.encryption_key")); err != nil {
		fatal("init crypto: %v", err)
	}
	db := ioc.InitMongoDB()
	repo := repository.NewCloudAccountRepository(accountdao.NewCloudAccountDAO(db))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	accounts, _, err := repo.List(ctx, domain.CloudAccountFilter{
		Provider: domain.CloudProviderAliyun,
		TenantID: tenant,
	})
	if err != nil {
		fatal("list accounts: %v", err)
	}
	if len(accounts) == 0 {
		// 兜底：不带过滤列出全部账号，看租户/厂商分布（不打印凭证）
		all, _, lerr := repo.List(ctx, domain.CloudAccountFilter{})
		if lerr != nil {
			fatal("list all accounts: %v", lerr)
		}
		fmt.Printf("tenant %d 无阿里云账号；全库账号分布：\n", tenant)
		for _, a := range all {
			fmt.Printf("  id=%d provider=%s tenant=%d status=%s name=%s\n", a.ID, a.Provider, a.TenantID, a.Status, a.Name)
		}
		fatal("无法继续")
	}
	acc := accounts[0]
	fmt.Printf("== 使用云账号 id=%d name=%s（凭证不回显） ==\n", acc.ID, acc.Name)

	client := &sls.Client{
		Endpoint:        region + ".log.aliyuncs.com",
		AccessKeyID:     acc.AccessKeyID,
		AccessKeySecret: acc.AccessKeySecret,
		RequestTimeOut:  20 * time.Second,
	}

	// -list-projects：只枚举全账号 project 清单（找 app 日志落点用）
	if listProjects {
		projects, err := client.ListProject()
		if err != nil {
			fatal("ListProject: %v", err)
		}
		fmt.Printf("== 账号全部 SLS project（共 %d 个） ==\n", len(projects))
		for _, p := range projects {
			fmt.Println("  -", p)
		}
		return
	}

	storeList, err := client.ListLogStore(project)
	if err != nil {
		fatal("ListLogStore %s/%s: %v", region, project, err)
	}
	fmt.Printf("== %s/%s 共 %d 个 logstore ==\n", region, project, len(storeList))
	for _, s := range storeList {
		fmt.Println("  -", s)
	}

	var targets []string
	if stores != "" {
		for _, s := range strings.Split(stores, ",") {
			if s = strings.TrimSpace(s); s != "" {
				targets = append(targets, s)
			}
		}
	} else {
		for _, s := range storeList {
			if strings.HasSuffix(s, "-business") || strings.Contains(s, "business") {
				targets = append(targets, s)
			}
		}
	}
	fmt.Printf("\n== 探测 %d 个 app 业务候选 store（近 24h，各 3 条） ==\n", len(targets))
	now := time.Now().Unix()
	for _, store := range targets {
		resp, err := client.GetLogsV2(project, store, &sls.GetLogRequest{
			From: now - 86400, To: now, Query: "*", Lines: 3, Reverse: true,
		})
		if err != nil {
			fmt.Printf("\n-- %s: 查询失败 %v\n", store, err)
			continue
		}
		fmt.Printf("\n-- %s: %d 条样本\n", store, len(resp.Logs))
		if len(resp.Logs) == 0 {
			continue
		}
		keys := make([]string, 0, len(resp.Logs[0]))
		for k := range resp.Logs[0] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Println("   字段:", strings.Join(keys, ", "))
		row := resp.Logs[0]
		fmt.Println("   样本:")
		for _, k := range keys {
			v := row[k]
			if len(v) > 80 {
				v = v[:80] + "…"
			}
			fmt.Printf("     %s = %s\n", k, v)
		}
	}
}
