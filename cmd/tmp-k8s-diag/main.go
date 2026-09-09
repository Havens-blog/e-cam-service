// 诊断：集群凭证状态 + 最新扫描快照的 K8s 通道结果 + crd 引用落库情况
// + APIServer 可达性（TCP 拨号）。
package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type cred struct {
	ClusterName string `bson:"clusterName"`
	DisplayName string `bson:"displayName"`
	APIEndpoint string `bson:"apiEndpoint"`
	CreatedAt   time.Time `bson:"createdAt"`
}

type snapshot struct {
	Status      string `bson:"status"`
	StartedAt   time.Time `bson:"startedAt"`
	FinishedAt  time.Time `bson:"finishedAt"`
	FailReason  string `bson:"failReason"`
	PartialFailures []struct {
		Cloud   string `bson:"cloud"`
		Product string `bson:"product"`
		Account string `bson:"account"`
		Reason  string `bson:"reason"`
	} `bson:"partialFailures"`
}

type refAgg struct {
	ID    map[string]string `bson:"_id"`
	Count int64             `bson:"count"`
}

func main() {
	v := viper.New()
	v.SetConfigFile("config/prod.yaml")
	if err := v.ReadInConfig(); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	dsn := v.GetString("mongodb.dsn")
	parts := strings.Split(dsn, "//")
	uri := fmt.Sprintf("%s//%s:%s@%s", parts[0], v.GetString("mongodb.username"), v.GetString("mongodb.password"), parts[1])
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		panic(err)
	}
	m := mongox.NewMongo(client, v.GetString("mongodb.db"))

	fmt.Println("=== 1. 集群凭证 ===")
	cur, err := m.Collection("cert_k8s_credentials").Find(ctx, bson.M{})
	if err != nil {
		fmt.Println("credentials query err:", err)
		return
	}
	var creds []cred
	if err := cur.All(ctx, &creds); err != nil {
		fmt.Println("credentials decode err:", err)
		return
	}
	for _, c := range creds {
		fmt.Printf("  键=%s 可读名=%q endpoint=%q 登记=%s\n",
			c.ClusterName, c.DisplayName, c.APIEndpoint, c.CreatedAt.Format("01-02 15:04"))
		// APIServer 可达性
		if c.APIEndpoint != "" {
			go dialCheck(c.ClusterName, c.APIEndpoint)
		} else {
			fmt.Printf("    [无 endpoint] 无法探测\n")
		}
	}
	time.Sleep(3 * time.Second)

	fmt.Println("\n=== 2. 最新扫描快照 ===")
	snapCol := m.Collection("cert_scan_snapshots")
	var latest snapshot
	err = snapCol.FindOne(ctx, bson.M{}, options.FindOne().SetSort(bson.M{"startedAt": -1})).Decode(&latest)
	if err != nil {
		fmt.Println("snapshot err:", err)
	} else {
		fmt.Printf("  status=%s started=%s\n", latest.Status, latest.StartedAt.Format("01-02 15:04:05"))
		if latest.FailReason != "" {
			fmt.Printf("  failReason=%s\n", latest.FailReason)
		}
		for _, pf := range latest.PartialFailures {
			fmt.Printf("  通道失败: cloud=%s product=%s account=%s\n    reason=%s\n", pf.Cloud, pf.Product, pf.Account, pf.Reason)
		}
		if len(latest.PartialFailures) == 0 {
			fmt.Println("  无通道失败记录")
		}
	}

	fmt.Println("\n=== 3. cert_references 按产品统计 ===")
	col := m.Collection("cert_references")
	cursor, err := col.Aggregate(ctx, []bson.M{{"$group": bson.M{"_id": bson.M{"product": "$product", "clusterId": "$clusterId"}, "count": bson.M{"$sum": 1}}}})
	if err != nil {
		fmt.Println("aggregate err:", err)
		return
	}
	var aggs []refAgg
	if err := cursor.All(ctx, &aggs); err != nil {
		fmt.Println("agg decode err:", err)
		return
	}
	for _, a := range aggs {
		fmt.Printf("  product=%s clusterId=%s count=%d\n", a.ID["product"], a.ID["clusterId"], a.Count)
	}
}

func dialCheck(name, endpoint string) {
	u, err := url.Parse(endpoint)
	if err != nil {
		fmt.Printf("    [%s] endpoint 解析失败: %v\n", name, err)
		return
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if err != nil {
		fmt.Printf("    [%s] %s:%s 不可达: %v\n", name, host, port, err)
		return
	}
	_ = conn.Close()
	fmt.Printf("    [%s] %s:%s 可达 (%dms)\n", name, host, port, time.Since(start).Milliseconds())
}
