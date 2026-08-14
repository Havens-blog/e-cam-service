package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/gotomicro/ego/core/elog"
)

// SyncRelations 同步资产关系
func (s *assetSyncService) SyncRelations(ctx context.Context, tenantID int64) (*RelationSyncResult, error) {
	if tenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}

	result := &RelationSyncResult{
		ByRelationType: make(map[string]int),
		StartTime:      time.Now(),
	}

	s.logger.Info("开始同步资产关系", elog.Int64("tenant_id", tenantID))

	// 1. 同步 ECS -> VPC 关系
	if r, err := s.syncECSToVPCRelations(ctx, tenantID); err == nil {
		result.Created += r.Created
		result.Skipped += r.Skipped
		result.Failed += r.Failed
		result.ByRelationType["ecs_belongs_to_vpc"] = r.Created
	}

	// 2. 同步 EIP -> ECS 关系
	if r, err := s.syncEIPToECSRelations(ctx, tenantID); err == nil {
		result.Created += r.Created
		result.Skipped += r.Skipped
		result.Failed += r.Failed
		result.ByRelationType["eip_bindto_ecs"] = r.Created
	}

	// 3. 同步 RDS -> VPC 关系
	if r, err := s.syncRDSToVPCRelations(ctx, tenantID); err == nil {
		result.Created += r.Created
		result.Skipped += r.Skipped
		result.Failed += r.Failed
		result.ByRelationType["rds_belongs_to_vpc"] = r.Created
	}

	// 4. 同步 Redis -> VPC 关系
	if r, err := s.syncRedisToVPCRelations(ctx, tenantID); err == nil {
		result.Created += r.Created
		result.Skipped += r.Skipped
		result.Failed += r.Failed
		result.ByRelationType["redis_belongs_to_vpc"] = r.Created
	}

	result.TotalSynced = result.Created
	result.EndTime = time.Now()
	result.DurationMs = result.EndTime.Sub(result.StartTime).Milliseconds()

	s.logger.Info("资产关系同步完成",
		elog.Int("total_created", result.Created),
		elog.Int("skipped", result.Skipped),
		elog.Int64("duration_ms", result.DurationMs))

	return result, nil
}

type relationSyncPartialResult struct {
	Created int
	Skipped int
	Failed  int
}

func (s *assetSyncService) syncECSToVPCRelations(ctx context.Context, tenantID int64) (*relationSyncPartialResult, error) {
	result := &relationSyncPartialResult{}

	ecsInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vm"})
	if err != nil {
		return nil, err
	}

	vpcInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vpc"})
	if err != nil {
		return nil, err
	}

	vpcMap := make(map[string]int64)
	for _, vpc := range vpcInstances {
		if vpcID, ok := vpc.Attributes["vpc_id"].(string); ok && vpcID != "" {
			vpcMap[vpcID] = vpc.ID
		}
	}

	for _, ecs := range ecsInstances {
		vpcID, ok := ecs.Attributes["vpc_id"].(string)
		if !ok || vpcID == "" {
			continue
		}
		targetVPCID, exists := vpcMap[vpcID]
		if !exists {
			result.Skipped++
			continue
		}
		exists, _ = s.relationRepo.Exists(ctx, ecs.ID, targetVPCID, "ecs_belongs_to_vpc")
		if exists {
			result.Skipped++
			continue
		}
		_, err = s.relationRepo.Create(ctx, domain.InstanceRelation{
			SourceInstanceID: ecs.ID, TargetInstanceID: targetVPCID,
			RelationTypeUID: "ecs_belongs_to_vpc", TenantID: tenantID,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Created++
	}
	return result, nil
}

func (s *assetSyncService) syncEIPToECSRelations(ctx context.Context, tenantID int64) (*relationSyncPartialResult, error) {
	result := &relationSyncPartialResult{}

	eipInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_eip"})
	if err != nil {
		return nil, err
	}

	ecsInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vm"})
	if err != nil {
		return nil, err
	}

	ecsMap := make(map[string]int64)
	for _, ecs := range ecsInstances {
		if instanceID, ok := ecs.Attributes["instance_id"].(string); ok && instanceID != "" {
			ecsMap[instanceID] = ecs.ID
		}
	}

	for _, eip := range eipInstances {
		instanceType, _ := eip.Attributes["instance_type"].(string)
		instanceID, _ := eip.Attributes["instance_id"].(string)
		if instanceID == "" || (instanceType != "" && instanceType != "EcsInstance" && instanceType != "Ecs") {
			continue
		}
		targetECSID, exists := ecsMap[instanceID]
		if !exists {
			result.Skipped++
			continue
		}
		exists, _ = s.relationRepo.Exists(ctx, eip.ID, targetECSID, "eip_bindto_ecs")
		if exists {
			result.Skipped++
			continue
		}
		_, err = s.relationRepo.Create(ctx, domain.InstanceRelation{
			SourceInstanceID: eip.ID, TargetInstanceID: targetECSID,
			RelationTypeUID: "eip_bindto_ecs", TenantID: tenantID,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Created++
	}
	return result, nil
}

func (s *assetSyncService) syncRDSToVPCRelations(ctx context.Context, tenantID int64) (*relationSyncPartialResult, error) {
	result := &relationSyncPartialResult{}

	rdsInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_rds"})
	if err != nil {
		return nil, err
	}

	vpcInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vpc"})
	if err != nil {
		return nil, err
	}

	vpcMap := make(map[string]int64)
	for _, vpc := range vpcInstances {
		if vpcID, ok := vpc.Attributes["vpc_id"].(string); ok && vpcID != "" {
			vpcMap[vpcID] = vpc.ID
		}
	}

	for _, rds := range rdsInstances {
		vpcID, ok := rds.Attributes["vpc_id"].(string)
		if !ok || vpcID == "" {
			continue
		}
		targetVPCID, exists := vpcMap[vpcID]
		if !exists {
			result.Skipped++
			continue
		}
		exists, _ = s.relationRepo.Exists(ctx, rds.ID, targetVPCID, "rds_belongs_to_vpc")
		if exists {
			result.Skipped++
			continue
		}
		_, err = s.relationRepo.Create(ctx, domain.InstanceRelation{
			SourceInstanceID: rds.ID, TargetInstanceID: targetVPCID,
			RelationTypeUID: "rds_belongs_to_vpc", TenantID: tenantID,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Created++
	}
	return result, nil
}

func (s *assetSyncService) syncRedisToVPCRelations(ctx context.Context, tenantID int64) (*relationSyncPartialResult, error) {
	result := &relationSyncPartialResult{}

	redisInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_redis"})
	if err != nil {
		return nil, err
	}

	vpcInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vpc"})
	if err != nil {
		return nil, err
	}

	vpcMap := make(map[string]int64)
	for _, vpc := range vpcInstances {
		if vpcID, ok := vpc.Attributes["vpc_id"].(string); ok && vpcID != "" {
			vpcMap[vpcID] = vpc.ID
		}
	}

	for _, redis := range redisInstances {
		vpcID, ok := redis.Attributes["vpc_id"].(string)
		if !ok || vpcID == "" {
			continue
		}
		targetVPCID, exists := vpcMap[vpcID]
		if !exists {
			result.Skipped++
			continue
		}
		exists, _ = s.relationRepo.Exists(ctx, redis.ID, targetVPCID, "redis_belongs_to_vpc")
		if exists {
			result.Skipped++
			continue
		}
		_, err = s.relationRepo.Create(ctx, domain.InstanceRelation{
			SourceInstanceID: redis.ID, TargetInstanceID: targetVPCID,
			RelationTypeUID: "redis_belongs_to_vpc", TenantID: tenantID,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Created++
	}
	return result, nil
}
