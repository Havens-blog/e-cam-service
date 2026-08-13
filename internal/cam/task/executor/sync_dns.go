package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// syncDNS 同步 DNS 域名和解析记录到专用集合
func (e *SyncAssetsExecutor) syncDNS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
) (int, error) {
	if e.dnsDomainColl == nil || e.dnsRecordColl == nil {
		e.logger.Warn("DNS集合未设置，跳过DNS同步")
		return 0, nil
	}

	dnsAdapter := adapter.DNS()
	if dnsAdapter == nil {
		e.logger.Warn("DNS适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	// 1. 同步域名
	domains, err := dnsAdapter.ListDomains(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取DNS域名列表失败: %w", err)
	}

	e.logger.Info("获取DNS域名列表成功",
		elog.String("provider", string(account.Provider)),
		elog.Int("count", len(domains)))

	now := time.Now().Unix()
	synced := 0
	var domainNames []string

	for _, d := range domains {
		domainNames = append(domainNames, d.DomainName)

		filter := bson.M{
			"tenant_id":   account.TenantID,
			"domain_name": d.DomainName,
			"account_id":  account.ID,
		}
		update := bson.M{
			"$set": bson.M{
				"domain_id":    d.DomainID,
				"domain_name":  d.DomainName,
				"provider":     string(account.Provider),
				"account_id":   account.ID,
				"account_name": account.Name,
				"tenant_id":    account.TenantID,
				"record_count": d.RecordCount,
				"status":       d.Status,
				"utime":        now,
			},
			"$setOnInsert": bson.M{"ctime": now},
		}
		opts := options.Update().SetUpsert(true)
		if _, err := e.dnsDomainColl.UpdateOne(ctx, filter, update, opts); err != nil {
			e.logger.Error("保存DNS域名失败", elog.String("domain", d.DomainName), elog.FieldErr(err))
			continue
		}
		synced++

		// 2. 同步该域名下的解析记录
		records, err := dnsAdapter.ListRecords(ctx, d.DomainName)
		if err != nil {
			e.logger.Error("获取DNS记录失败", elog.String("domain", d.DomainName), elog.FieldErr(err))
			continue
		}

		var recordIDs []string
		for _, r := range records {
			recordIDs = append(recordIDs, r.RecordID)

			rFilter := bson.M{
				"tenant_id":  account.TenantID,
				"record_id":  r.RecordID,
				"account_id": account.ID,
			}
			rUpdate := bson.M{
				"$set": bson.M{
					"record_id":  r.RecordID,
					"domain":     d.DomainName,
					"rr":         r.RR,
					"type":       r.Type,
					"value":      r.Value,
					"ttl":        r.TTL,
					"priority":   r.Priority,
					"line":       r.Line,
					"status":     r.Status,
					"provider":   string(account.Provider),
					"account_id": account.ID,
					"tenant_id":  account.TenantID,
					"utime":      now,
				},
				"$setOnInsert": bson.M{"ctime": now},
			}
			rOpts := options.Update().SetUpsert(true)
			if _, err := e.dnsRecordColl.UpdateOne(ctx, rFilter, rUpdate, rOpts); err != nil {
				e.logger.Error("保存DNS记录失败",
					elog.String("domain", d.DomainName),
					elog.String("record_id", r.RecordID),
					elog.FieldErr(err))
			}
			synced++
		}

		// 清理已删除的记录
		if len(recordIDs) > 0 {
			delFilter := bson.M{
				"tenant_id":  account.TenantID,
				"account_id": account.ID,
				"domain":     d.DomainName,
				"record_id":  bson.M{"$nin": recordIDs},
			}
			if _, err := e.dnsRecordColl.DeleteMany(ctx, delFilter); err != nil {
				e.logger.Error("清理过期DNS记录失败", elog.String("domain", d.DomainName), elog.FieldErr(err))
			}
		}
	}

	// 清理已删除的域名
	if len(domainNames) > 0 {
		delFilter := bson.M{
			"tenant_id":   account.TenantID,
			"account_id":  account.ID,
			"domain_name": bson.M{"$nin": domainNames},
		}
		if _, err := e.dnsDomainColl.DeleteMany(ctx, delFilter); err != nil {
			e.logger.Error("清理过期DNS域名失败", elog.FieldErr(err))
		}
	}

	e.logger.Info("同步DNS完成",
		elog.String("account", account.Name),
		elog.String("provider", string(account.Provider)),
		elog.Int("synced", synced))

	return synced, nil
}
