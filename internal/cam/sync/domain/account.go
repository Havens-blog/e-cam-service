package domain

import "time"

// CloudAccount 云账号配置
type CloudAccount struct {
	ID              int64         `json:"id" bson:"id"`
	Name            string        `json:"name" bson:"name"`                         // 账号名称
	Provider        CloudProvider `json:"provider" bson:"provider"`                 // 云厂商
	AccessKeyID     string        `json:"access_key_id" bson:"access_key_id"`       // 访问密钥ID
	AccessKeySecret string        `json:"access_key_secret" bson:"access_key_secret"` // 访问密钥Secret（加密存储）
	DefaultRegion   string        `json:"default_region" bson:"default_region"`     // 默认地域
	Enabled         bool          `json:"enabled" bson:"enabled"`                   // 是否启用
	Description     string        `json:"description" bson:"description"`           // 描述
	Ctime           int64         `json:"ctime" bson:"ctime"`                       // 创建时间
	Utime           int64         `json:"utime" bson:"utime"`                       // 更新时间
}

// SyncConfig 同步配置
type SyncConfig struct {
	AccountID      int64    `json:"account_id" bson:"account_id"`           // 账号ID
	ResourceTypes  []string `json:"resource_types" bson:"resource_types"`   // 要同步的资源类型
	Regions        []string `json:"regions" bson:"regions"`                 // 要同步的地域列表，为空表示所有地域
	SyncInterval   int      `json:"sync_interval" bson:"sync_interval"`     // 同步间隔（秒）
	Enabled        bool     `json:"enabled" bson:"enabled"`                 // 是否启用自动同步
	ConcurrentNum  int      `json:"concurrent_num" bson:"concurrent_num"`   // 并发数
	LastSyncTime   int64    `json:"last_sync_time" bson:"last_sync_time"`   // 上次同步时间
	NextSyncTime   int64    `json:"next_sync_time" bson:"next_sync_time"`   // 下次同步时间
}

// GetDecryptedSecret 获取解密后的密钥（实际使用时需要实现解密逻辑）
func (a *CloudAccount) GetDecryptedSecret() string {
	// TODO: 实现解密逻辑
	return a.AccessKeySecret
}

// Validate 验证账号配置
func (a *CloudAccount) Validate() error {
	if a.Name == "" {
		return ErrInvalidAccountName
	}
	if a.Provider == "" {
		return ErrInvalidProvider
	}
	if a.AccessKeyID == "" {
		return ErrInvalidAccessKey
	}
	if a.AccessKeySecret == "" {
		return ErrInvalidAccessKey
	}
	return nil
}

// IsExpired 检查账号是否过期（可以根据业务需求扩展）
func (a *CloudAccount) IsExpired() bool {
	// TODO: 实现过期检查逻辑
	return false
}

// GetSyncRegions 获取要同步的地域列表
func (c *SyncConfig) GetSyncRegions() []string {
	if len(c.Regions) == 0 {
		return nil // 返回 nil 表示同步所有地域
	}
	return c.Regions
}

// ShouldSync 判断是否应该执行同步
func (c *SyncConfig) ShouldSync() bool {
	if !c.Enabled {
		return false
	}
	now := time.Now().Unix()
	return now >= c.NextSyncTime
}

// UpdateNextSyncTime 更新下次同步时间
func (c *SyncConfig) UpdateNextSyncTime() {
	now := time.Now().Unix()
	c.LastSyncTime = now
	c.NextSyncTime = now + int64(c.SyncInterval)
}
