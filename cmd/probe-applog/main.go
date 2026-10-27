// Command probe-applog 临时诊断（不进 CI）：用平台自身云账号（解密链复用
// internal/account/repository）实测云厂商日志服务的 logstore/topic 清单与
// app 业务日志字段 schema，为 logquery「应用日志」类型的映射设计提供实测依据。
// 已实测：阿里 SLS jlc-prod-log 104 个 <service>-business store；
// 火山 TLS *_business topic。
//
// 凭证只经内存解密后用于云 API 调用，不打印不落盘；日志样本仅输出字段名与
// 截断值。用法：
//
//	go run ./cmd/probe-applog -f config/prod.yaml -tenant 1 \
//	  -region cn-shenzhen -project jlc-lb-log
//	go run ./cmd/probe-applog -f config/prod.yaml -tenant 3 -provider volcano
package main

import (
	"context"
	"encoding/json"
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
	"github.com/volcengine/volc-sdk-golang/service/tls"
)

func fatal(f string, a ...any) { fmt.Fprintf(os.Stderr, "[probe] "+f+"\n", a...); os.Exit(1) }

// resolveAccount 按 provider+tenant 取云账号（凭证不回显），空结果时列出全库分布帮助定位。
func resolveAccount(repo repository.CloudAccountRepository, ctx context.Context, provider domain.CloudProvider, tenant int64) domain.CloudAccount {
	accounts, _, err := repo.List(ctx, domain.CloudAccountFilter{Provider: provider, TenantID: tenant})
	if err != nil {
		fatal("list accounts: %v", err)
	}
	if len(accounts) == 0 {
		all, _, lerr := repo.List(ctx, domain.CloudAccountFilter{})
		if lerr != nil {
			fatal("list all accounts: %v", lerr)
		}
		fmt.Printf("tenant %d 无 %s 账号；全库账号分布：\n", tenant, provider)
		for _, a := range all {
			fmt.Printf("  id=%d provider=%s tenant=%d status=%s name=%s\n", a.ID, a.Provider, a.TenantID, a.Status, a.Name)
		}
		fatal("无法继续")
	}
	acc := accounts[0]
	fmt.Printf("== 使用云账号 id=%d name=%s provider=%s（凭证不回显） ==\n", acc.ID, acc.Name, acc.Provider)
	return acc
}

func main() {
	var (
		confFile     string
		tenant       int64
		provider     string
		region       string
		project      string
		stores       string
		listProjects bool
		kind         string
		listTopics   bool
		query        string
		topicFilter  string
		vregion      string
		rawSQL       string
	)
	flag.StringVar(&confFile, "f", "config/prod.yaml", "service config file")
	flag.Int64Var(&tenant, "tenant", 1, "tenant id owning the cloud accounts")
	flag.StringVar(&provider, "provider", "aliyun", "cloud provider: aliyun | volcano")
	flag.StringVar(&region, "region", "cn-shenzhen", "SLS region (aliyun)")
	flag.StringVar(&project, "project", "jlc-lb-log", "SLS project to enumerate (aliyun)")
	flag.StringVar(&stores, "stores", "", "comma-separated explicit logstores to sample (skips -business detection)")
	flag.BoolVar(&listProjects, "list-projects", false, "only enumerate all SLS projects of the account (aliyun)")
	flag.StringVar(&kind, "kind", "", "aliyun probe kind: k8s = enumerate k8s-log-* projects event stores")
	flag.BoolVar(&fullDump, "full", false, "dump the first non-empty sample row as full JSON")
	flag.StringVar(&rawSQL, "sql", "", "aliyun: run this exact query (search|SQL pipeline) against -project/-stores and dump rows")
	flag.BoolVar(&listTopics, "list-topics", false, "only enumerate all TLS topics (volcano)")
	flag.StringVar(&query, "query", "*", "search query passthrough for topic sampling (volcano)")
	flag.StringVar(&topicFilter, "topic", "", "only probe topics whose name contains this substring (volcano)")
	flag.StringVar(&vregion, "vregion", "", "limit volcano probe to one region (empty = all enum regions)")
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	switch strings.ToLower(provider) {
	case "aliyun":
		acc := resolveAccount(repo, ctx, domain.CloudProviderAliyun, tenant)
		probeAliyun(acc, region, project, stores, listProjects, kind, rawSQL)
	case "volcano":
		// 库里火山账号 provider 键为 volcengine（SDK volcano/volcengine 双键兼容）
		acc := resolveAccount(repo, ctx, domain.CloudProviderVolcengine, tenant)
		probeVolcano(acc, listTopics, query, topicFilter, vregion)
	default:
		fatal("未知 provider: %s", provider)
	}
}

// probeAliyun 阿里 SLS：project/logstore 枚举 + -business 候选采样。
func probeAliyun(acc domain.CloudAccount, region, project, stores string, listProjects bool, kind, rawSQL string) {
	client := &sls.Client{
		Endpoint:        region + ".log.aliyuncs.com",
		AccessKeyID:     acc.AccessKeyID,
		AccessKeySecret: acc.AccessKeySecret,
		RequestTimeOut:  20 * time.Second,
	}

	if rawSQL != "" {
		now := time.Now().Unix()
		resp, err := client.GetLogsV2(project, stores, &sls.GetLogRequest{
			From: now - 7*86400, To: now, Query: rawSQL, Lines: 20,
		})
		if err != nil {
			fatal("raw sql: %v", err)
		}
		fmt.Printf("== 命中 %d 行 ==\n", len(resp.Logs))
		for _, row := range resp.Logs {
			parts := make([]string, 0, len(row))
			for k, v := range row {
				parts = append(parts, k+"="+v)
			}
			sort.Strings(parts)
			fmt.Println("  " + strings.Join(parts, " | "))
		}
		return
	}

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

	if kind == "k8s" {
		probeAliyunK8s(client)
		return
	}
	if kind == "k8sindex" {
		// 索引配置转储:field index(doc_value/倒排)决定过滤与分组能否下推 SLS。
		target := project
		if stores != "" {
			target = stores // 复用 -stores 传 logstore 名
		}
		idx, err := client.GetIndex(project, target)
		if err != nil {
			fatal("GetIndex %s/%s: %v", project, target, err)
		}
		fmt.Printf("== %s/%s 索引配置 ==\nline_full_text: %v\n", project, target, idx.Line != nil)
		if idx.Line != nil {
			fmt.Printf("  fulltext: case_sensitive=%v chn=%v tokens=%v\n",
				idx.Line.CaseSensitive, idx.Line.Chn, idx.Line.Token)
		}
		keys := make([]string, 0, len(idx.Keys))
		for k := range idx.Keys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := idx.Keys[k]
			fmt.Printf("  key %s: type=%s doc_value=%v json_keys=%d\n",
				k, v.Type, v.DocValue, len(v.JsonKeys))
			for name, jk := range v.JsonKeys {
				fmt.Printf("    json %s: type=%s doc_value=%v\n", name, jk.Type, jk.DocValue)
			}
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
		rows := make([]map[string]any, 0, len(resp.Logs))
		for _, l := range resp.Logs {
			r := make(map[string]any, len(l))
			for k, v := range l {
				r[k] = v
			}
			rows = append(rows, r)
		}
		printSamples(store, len(rows), func(i int) map[string]any { return rows[i] })
	}
}

// probeAliyunK8s 枚举账号内 k8s-log-* project(ACK 集群日志接入),逐 project
// 列 logstore 并采样含 "event" 的 store schema,为 k8s 事件日志类型设计
// 提供实测依据。SLS ListProject 分页由 SDK 内部处理。
var fullDump bool

func probeAliyunK8s(client *sls.Client) {
	projects, err := client.ListProject()
	if err != nil {
		fatal("ListProject: %v", err)
	}
	var k8s []string
	for _, p := range projects {
		if strings.HasPrefix(p, "k8s-log-") {
			k8s = append(k8s, p)
		}
	}
	fmt.Printf("== k8s-log-* project 共 %d 个 ==\n", len(k8s))
	now := time.Now().Unix()
	for _, project := range k8s {
		stores, err := client.ListLogStore(project)
		if err != nil {
			fmt.Printf("\n-- %s: ListLogStore 失败 %v\n", project, err)
			continue
		}
		var eventStores []string
		for _, s := range stores {
			if strings.Contains(strings.ToLower(s), "event") {
				eventStores = append(eventStores, s)
			}
		}
		fmt.Printf("\n== %s(%d store,事件候选 %d)==\n", project, len(stores), len(eventStores))
		for _, s := range stores {
			fmt.Printf("   store: %s\n", s)
		}
		for _, s := range eventStores {
			resp, err := client.GetLogsV2(project, s, &sls.GetLogRequest{
				From: now - 86400, To: now, Query: "*", Lines: 2, Reverse: true,
			})
			if err != nil {
				fmt.Printf("   [%s] 采样失败 %v\n", s, err)
				continue
			}
			rows := make([]map[string]any, 0, len(resp.Logs))
			for _, l := range resp.Logs {
				r := make(map[string]any, len(l))
				for k, v := range l {
					r[k] = v
				}
				rows = append(rows, r)
			}
			if fullDump && len(rows) > 0 {
				b, _ := json.MarshalIndent(rows[0], "", "  ")
				fmt.Printf("   [%s] 全量首条:\n%s\n", s, string(b))
				fullDump = false
			}
			printSamples(s, len(rows), func(i int) map[string]any { return rows[i] })
		}
	}
}

// probeVolcano 火山 TLS：全量 DescribeTopics 枚举 + *_business 候选采样。
// TLS SearchLogs 时间为 Unix 秒（同 SDK provider.go 注释）。
func probeVolcano(acc domain.CloudAccount, listTopics bool, query, topicFilter, vregion string) {
	regions := acc.Regions
	if len(regions) == 0 {
		regions = []string{"cn-guangzhou", "cn-beijing", "cn-shanghai"}
	}
	if vregion != "" {
		regions = []string{vregion}
	}
	now := time.Now().Unix()
	var allBusiness []*tls.Topic
	for _, region := range regions {
		client := tls.NewClient("https://tls-"+region+".volces.com", acc.AccessKeyID, acc.AccessKeySecret, "", region)
		var topics []*tls.Topic
		for page := 1; ; page++ {
			resp, err := client.DescribeTopics(&tls.DescribeTopicsRequest{ProjectID: "", PageNumber: page, PageSize: 100})
			if err != nil {
				fatal("DescribeTopics %s page %d: %v", region, page, err)
			}
			if resp == nil || len(resp.Topics) == 0 {
				break
			}
			topics = append(topics, resp.Topics...)
			if len(resp.Topics) < 100 {
				break
			}
		}
		fmt.Printf("== %s 共 %d 个 topic ==\n", region, len(topics))
		business := 0
		for _, t := range topics {
			if listTopics {
				fmt.Printf("  - %s (id=%s ttl=%d)\n", t.TopicName, t.TopicID, t.Ttl)
			}
			if strings.HasSuffix(strings.ToLower(t.TopicName), "business") {
				if topicFilter != "" && !strings.Contains(strings.ToLower(t.TopicName), strings.ToLower(topicFilter)) {
					continue
				}
				business++
				allBusiness = append(allBusiness, t)
				if listTopics {
					continue // 列表模式不再重复采样
				}
				q := query
				if q == "" {
					q = "*"
				}
				resp, err := client.SearchLogs(&tls.SearchLogsRequest{
					TopicID: t.TopicID, Query: q,
					StartTime: now - 86400, EndTime: now, Limit: 3, Sort: "desc",
				})
				if err != nil {
					fmt.Printf("\n-- %s: 查询失败 %v\n", t.TopicName, err)
					continue
				}
				rows := make([]map[string]any, 0, len(resp.Logs))
				for _, l := range resp.Logs {
					r := make(map[string]any, len(l))
					for k, v := range l {
						r[k] = v
					}
					rows = append(rows, r)
				}
				printSamples(t.TopicName, len(rows), func(i int) map[string]any { return rows[i] })
			}
		}
		fmt.Printf("   其中 business 候选 %d 个\n", business)
	}
	fmt.Printf("\n== 全 region business 候选合计 %d 个 ==\n", len(allBusiness))
}

// printSamples 打印样本字段名与截断值（凭证无关字段，值截断 80 字符）。
func printSamples(name string, n int, row func(int) map[string]any) {
	fmt.Printf("\n-- %s: %d 条样本\n", name, n)
	if n == 0 {
		return
	}
	keys := make([]string, 0, len(row(0)))
	for k := range row(0) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("   字段:", strings.Join(keys, ", "))
	fmt.Println("   样本:")
	for _, k := range keys {
		v := fmt.Sprintf("%v", row(0)[k])
		if len(v) > 80 {
			v = v[:80] + "…"
		}
		fmt.Printf("     %s = %s\n", k, v)
	}
}
