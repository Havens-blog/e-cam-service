// Package executor NAS 任务执行器共用构件(每日采集与历史回填共享)。
//
// 文件：internal/cam/task/executor/nas_account_gate.go
//
// 作用：收敛 SyncNASMetricsExecutor 与 SyncNASBackfillExecutor 之间的同构
// 代码——账号级互斥闸与账号清单解析。原两执行器各自持有一份逐字重复的
// syncMu/syncingNow 互斥实现与 resolveAccounts,DRY 收敛为单一定义;
// CDN/资产执行器(sync_cdn_metrics.go/sync_assets.go)的同模式实现不在本
// feature 范围,保持原样。
package executor

import (
	"context"
	"fmt"
	"sync"

	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
)

// nasAccountGate NAS 执行器共用的账号级互斥闸(嵌入两个执行器):
// 同一账号同时只放行一个任务,避免重复消耗厂商 API 配额(沿
// SyncAssetsExecutor/SyncCDNMetricsExecutor 同模式)。嵌入后执行器经字段
// 提升直接暴露 syncingNow/syncMu 与 tryAcquireAccount/releaseAccount
// (单测依赖同名访问),行为与原各执行器私有实现逐字一致。
type nasAccountGate struct {
	syncMu     sync.Mutex
	syncingNow map[int64]string // account_id -> task_id(持有者幂等)
}

// newNASAccountGate 创建账号级互斥闸
func newNASAccountGate() nasAccountGate {
	return nasAccountGate{syncingNow: make(map[int64]string)}
}

// tryAcquireAccount 占用账号执行权;已被其他任务持有返回 false(持有者自身幂等)。
func (g *nasAccountGate) tryAcquireAccount(accountID int64, taskID string) bool {
	g.syncMu.Lock()
	defer g.syncMu.Unlock()
	if owner, busy := g.syncingNow[accountID]; busy && owner != taskID {
		return false
	}
	g.syncingNow[accountID] = taskID
	return true
}

// releaseAccount 释放账号执行权(仅持有者可释放)。
func (g *nasAccountGate) releaseAccount(accountID int64, taskID string) {
	g.syncMu.Lock()
	defer g.syncMu.Unlock()
	if owner, busy := g.syncingNow[accountID]; busy && owner == taskID {
		delete(g.syncingNow, accountID)
	}
}

// resolveNASAccounts 解析待执行账号(每日采集与历史回填共用,口径一致):
// 指定 account_id 取单个,否则取全部活跃账号(provider 可选过滤)。指标采集
// 与回填均不依赖账号的 EnableAutoSync 开关——对已纳管账号统一执行,
// 避免未开自动同步的账号静默漏采(与 CDN 一致)。
func resolveNASAccounts(
	ctx context.Context,
	accountRepo camrepository.CloudAccountRepository,
	accountID int64,
	provider string,
) ([]domain.CloudAccount, error) {
	if accountID > 0 {
		account, err := accountRepo.GetByID(ctx, accountID)
		if err != nil {
			return nil, fmt.Errorf("获取云账号失败: %w", err)
		}
		return []domain.CloudAccount{account}, nil
	}
	filter := domain.CloudAccountFilter{
		Provider: domain.CloudProvider(provider),
		Status:   domain.CloudAccountStatusActive,
		Limit:    100,
	}
	accts, _, err := accountRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("获取云账号列表失败: %w", err)
	}
	// provider 过滤在执行器侧再收口一次(仓储实现差异不影响限定语义)
	if provider != "" {
		filtered := make([]domain.CloudAccount, 0, len(accts))
		for _, a := range accts {
			if string(a.Provider) == provider {
				filtered = append(filtered, a)
			}
		}
		accts = filtered
	}
	return accts, nil
}
