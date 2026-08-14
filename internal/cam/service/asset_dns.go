package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// syncDNSDomains 同步 DNS 域名到 c_dns_domain
func (s *assetSyncService) syncDNSDomains(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	dnsAdapter := adapter.DNS()
	if dnsAdapter == nil {
		s.logger.Warn("DNS适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	domains, err := dnsAdapter.ListDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取DNS域名失败: %w", err)
	}

	if s.dnsDomainColl != nil {
		// 写入 c_dns_domain 集合
		now := time.Now().Unix()
		var currentNames []string
		for _, d := range domains {
			currentNames = append(currentNames, d.DomainName)
			filter := bson.M{
				"tenant_id":   tenantID,
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
					"tenant_id":    tenantID,
					"record_count": d.RecordCount,
					"status":       d.Status,
					"utime":        now,
				},
				"$setOnInsert": bson.M{
					"ctime": now,
				},
			}
			opts := options.Update().SetUpsert(true)
			if _, err := s.dnsDomainColl.UpdateOne(ctx, filter, update, opts); err != nil {
				s.logger.Error("保存DNS域名失败", elog.String("domain", d.DomainName), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.TotalSynced++
		}
		// 清理不再存在的域名
		deleteFilter := bson.M{
			"tenant_id":  tenantID,
			"account_id": account.ID,
		}
		if len(currentNames) > 0 {
			deleteFilter["domain_name"] = bson.M{"$nin": currentNames}
		}
		_, _ = s.dnsDomainColl.DeleteMany(ctx, deleteFilter)
	} else {
		// 回退：写入 c_instance（兼容旧逻辑）
		for _, d := range domains {
			attrs := map[string]interface{}{
				"provider":         string(account.Provider),
				"cloud_account_id": account.ID,
				"domain_id":        d.DomainID,
				"domain_name":      d.DomainName,
				"record_count":     d.RecordCount,
				"status":           d.Status,
			}
			assetID := d.DomainName
			if d.DomainID != "" {
				assetID = d.DomainID
			}
			cmdbInstance := domain.Instance{
				ModelUID:   "cloud_dns_domain",
				AssetID:    assetID,
				AssetName:  d.DomainName,
				TenantID:   tenantID,
				AccountID:  account.ID,
				Attributes: attrs,
			}
			if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
				result.Failed++
				continue
			}
			result.TotalSynced++
		}
	}
	return result, nil
}

// syncDNSRecords 同步 DNS 解析记录到 c_dns_record
func (s *assetSyncService) syncDNSRecords(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	dnsAdapter := adapter.DNS()
	if dnsAdapter == nil {
		return result, nil
	}

	// 先获取所有域名，再逐个同步记录
	domains, err := dnsAdapter.ListDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取DNS域名列表失败: %w", err)
	}

	for _, d := range domains {
		records, err := dnsAdapter.ListRecords(ctx, d.DomainName)
		if err != nil {
			s.logger.Warn("获取DNS记录失败",
				elog.String("domain", d.DomainName),
				elog.FieldErr(err))
			continue
		}

		if s.dnsRecordColl != nil {
			// 写入 c_dns_record 集合
			now := time.Now().Unix()
			var currentIDs []string
			for _, r := range records {
				recordID := r.RecordID
				if recordID == "" {
					recordID = fmt.Sprintf("%s_%s_%s", r.Domain, r.RR, r.Type)
				}
				currentIDs = append(currentIDs, recordID)
				filter := bson.M{
					"tenant_id":  tenantID,
					"record_id":  recordID,
					"account_id": account.ID,
				}
				update := bson.M{
					"$set": bson.M{
						"record_id":  recordID,
						"domain":     r.Domain,
						"rr":         r.RR,
						"type":       r.Type,
						"value":      r.Value,
						"ttl":        r.TTL,
						"priority":   r.Priority,
						"line":       r.Line,
						"status":     r.Status,
						"provider":   string(account.Provider),
						"account_id": account.ID,
						"tenant_id":  tenantID,
						"utime":      now,
					},
					"$setOnInsert": bson.M{
						"ctime": now,
					},
				}
				opts := options.Update().SetUpsert(true)
				if _, err := s.dnsRecordColl.UpdateOne(ctx, filter, update, opts); err != nil {
					s.logger.Error("保存DNS记录失败", elog.String("record_id", recordID), elog.FieldErr(err))
					result.Failed++
					continue
				}
				result.TotalSynced++
			}
			// 清理不再存在的记录
			deleteFilter := bson.M{
				"tenant_id":  tenantID,
				"account_id": account.ID,
				"domain":     d.DomainName,
			}
			if len(currentIDs) > 0 {
				deleteFilter["record_id"] = bson.M{"$nin": currentIDs}
			}
			_, _ = s.dnsRecordColl.DeleteMany(ctx, deleteFilter)
		} else {
			// 回退：写入 c_instance（兼容旧逻辑）
			for _, r := range records {
				attrs := map[string]interface{}{
					"provider":         string(account.Provider),
					"cloud_account_id": account.ID,
					"domain":           r.Domain,
					"rr":               r.RR,
					"type":             r.Type,
					"value":            r.Value,
					"ttl":              r.TTL,
					"priority":         r.Priority,
					"line":             r.Line,
					"status":           r.Status,
				}
				assetID := r.RecordID
				if assetID == "" {
					assetID = fmt.Sprintf("%s_%s_%s", r.Domain, r.RR, r.Type)
				}
				assetName := fmt.Sprintf("%s.%s", r.RR, r.Domain)
				cmdbInstance := domain.Instance{
					ModelUID:   "cloud_dns_record",
					AssetID:    assetID,
					AssetName:  assetName,
					TenantID:   tenantID,
					AccountID:  account.ID,
					Attributes: attrs,
				}
				if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
					result.Failed++
					continue
				}
				result.TotalSynced++
			}
		}
	}
	return result, nil
}
