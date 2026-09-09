// Package main tmp-cas-diag 一次性只读诊断（用后即删）：
// 最近扫描快照状态/部分失败 + cas 产品线引用计数与样例，验证证书库扫描是否生效。
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	v := viper.New()
	v.SetConfigFile("config/prod.yaml")
	if err := v.ReadInConfig(); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := v.GetString("mongodb.dsn")
	parts := strings.Split(dsn, "//")
	uri := fmt.Sprintf("%s//%s:%s@%s", parts[0], v.GetString("mongodb.username"), v.GetString("mongodb.password"), parts[1])
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		panic(err)
	}
	m := mongox.NewMongo(client, v.GetString("mongodb.db"))

	// 最近 3 个快照
	snapColl := m.Collection("cert_scan_snapshots")
	cur, _ := snapColl.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "startedAt", Value: -1}}).SetLimit(3))
	var snaps []bson.M
	_ = cur.All(ctx, &snaps)
	fmt.Println("== 最近快照 ==")
	var latest string
	for i, s := range snaps {
		oid, _ := s["_id"].(primitive.ObjectID)
		id := oid.Hex()
		if i == 0 {
			latest = id
		}
		fmt.Printf("  %s status=%v started=%v finished=%v failReason=%v\n", id, s["status"], s["startedAt"], s["finishedAt"], s["failReason"])
		if pf, ok := s["partialFailures"].(bson.A); ok && len(pf) > 0 {
			for j, p := range pf {
				if j >= 5 {
					fmt.Printf("    ... 共 %d 条部分失败\n", len(pf))
					break
				}
				fmt.Printf("    partial: %v\n", p)
			}
		}
	}

	if latest == "" {
		return
	}

	refColl := m.Collection("cert_references")
	byProduct, _ := refColl.Aggregate(ctx, bson.A{
		bson.M{"$match": bson.M{"snapshotId": latest}},
		bson.M{"$group": bson.M{"_id": "$product", "n": bson.M{"$sum": 1}}},
		bson.M{"$sort": bson.M{"n": -1}},
	})
	fmt.Printf("\n== 快照 %s 引用按产品 ==\n", latest)
	var rows []bson.M
	_ = byProduct.All(ctx, &rows)
	for _, r := range rows {
		fmt.Printf("  %-8s %v\n", r["_id"], r["n"])
	}

	// cas 引用样例
	cur2, _ := refColl.Find(ctx, bson.M{"snapshotId": latest, "product": "cas"}, options.Find().SetLimit(8))
	var casRefs []bson.M
	_ = cur2.All(ctx, &casRefs)
	fmt.Printf("\n== cas 引用样例 (%d) ==\n", len(casRefs))
	for _, r := range casRefs {
		fp := fmt.Sprintf("%v", r["certFingerprint"])
		tag := "真实"
		if strings.HasPrefix(fp, "certscan-unresolved") {
			tag = "占位!"
		}
		if len(fp) > 16 {
			fp = fp[:16] + "…"
		}
		fmt.Printf("  resourceId=%v cloudCertId=%v fp=%s(%s) account=%v\n",
			r["resourceId"], r["referencedCloudCertId"], fp, tag, r["accountKey"])
	}

	// 目标证书：jlccam.com-2026-09（certId=27029968）
	var target bson.M
	err = refColl.FindOne(ctx, bson.M{"snapshotId": latest, "referencedCloudCertID": "27029968"}).Decode(&target)
	if err != nil {
		err = refColl.FindOne(ctx, bson.M{"snapshotId": latest, "referencedCloudCertId": "27029968"}).Decode(&target)
	}
	fmt.Printf("\n== 目标 27029968 (jlccam.com-2026-09) ==\n")
	if err != nil {
		fmt.Printf("  快照内未找到: %v\n", err)
	} else {
		fp := fmt.Sprintf("%v", target["certFingerprint"])
		if len(fp) > 20 {
			fp = fp[:20] + "…"
		}
		fmt.Printf("  找到: resourceId=%v fp=%s\n", target["resourceId"], fp)
	}

	// 台账与映射现状
	certColl := m.Collection("cert_certificates")
	nFP, _ := certColl.CountDocuments(ctx, bson.M{"fingerprint": bson.M{"$regex": "^e0f76419fd16"}})
	fmt.Printf("台账含 e0f76419fd16 指纹: %d 条\n", nFP)
	mapColl := m.Collection("cert_cloud_cert_mappings")
	nMap, _ := mapColl.CountDocuments(ctx, bson.M{"cloudCertId": "27029968"})
	fmt.Printf("映射表 certId=27029968: %d 条\n", nMap)
}
