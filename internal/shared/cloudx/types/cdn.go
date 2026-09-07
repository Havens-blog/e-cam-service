package types

// CDNInstance CDN加速域名实例（通用格式）
type CDNInstance struct {
	// 基本信息
	DomainID   string `json:"domain_id"`   // 域名ID
	DomainName string `json:"domain_name"` // 加速域名
	Cname      string `json:"cname"`       // CNAME地址
	Status     string `json:"status"`      // 状态: online/offline/configuring/checking/check_failed/error(归一化枚举)
	Region     string `json:"region"`      // 加速区域: domestic/overseas/global

	// 业务类型(同步时经 NormalizeCDNInstance 归一化为统一枚举)
	BusinessType    string `json:"business_type"`     // 业务类型: web/download/media/whole_site/dynamic/other
	BusinessTypeRaw string `json:"business_type_raw"` // 厂商原始业务类型(归一化前)
	ServiceArea     string `json:"service_area"`      // 服务区域: domestic/overseas/global
	ServiceAreaRaw  string `json:"service_area_raw"`  // 厂商原始服务区域(归一化前)
	StatusRaw       string `json:"status_raw"`        // 厂商原始状态(归一化前)

	// 源站信息
	Origins    []CDNOrigin `json:"origins"`     // 源站列表
	OriginType string      `json:"origin_type"` // 源站类型: ip/domain/oss
	OriginHost string      `json:"origin_host"` // 回源Host

	// HTTPS配置
	HTTPSEnabled bool   `json:"https_enabled"` // 是否开启HTTPS
	CertName     string `json:"cert_name"`     // 证书名称
	HTTP2Enabled bool   `json:"http2_enabled"` // 是否开启HTTP/2

	// 带宽和流量
	Bandwidth    int64 `json:"bandwidth"`     // 带宽峰值(bps)
	TrafficTotal int64 `json:"traffic_total"` // 累计流量(bytes)

	// 时间信息
	CreationTime string `json:"creation_time"` // 创建时间
	ModifiedTime string `json:"modified_time"` // 修改时间

	// 项目/资源组信息
	ProjectID       string `json:"project_id"`
	ResourceGroupID string `json:"resource_group_id"`

	// 云账号信息
	CloudAccountID   int64  `json:"cloud_account_id"`
	CloudAccountName string `json:"cloud_account_name"`

	// 其他信息
	Tags        map[string]string `json:"tags"`
	Description string            `json:"description"`
	Provider    string            `json:"provider"` // 云厂商标识
}

// CDNOrigin CDN源站信息
type CDNOrigin struct {
	Address  string `json:"address"`  // 源站地址
	Type     string `json:"type"`     // 源站类型: ip/domain/oss
	Port     int    `json:"port"`     // 端口
	Priority int    `json:"priority"` // 优先级
	Weight   int    `json:"weight"`   // 权重
}

// CDNInstanceFilter CDN实例过滤条件
type CDNInstanceFilter struct {
	DomainName   string `json:"domain_name,omitempty"`   // 域名（模糊匹配）
	Status       string `json:"status,omitempty"`        // 状态
	BusinessType string `json:"business_type,omitempty"` // 业务类型
	PageNumber   int    `json:"page_number,omitempty"`
	PageSize     int    `json:"page_size,omitempty"`
}

// CDNCacheRule CDN 缓存规则(统一格式,由各厂商 GetCacheConfig 归一化产出)
type CDNCacheRule struct {
	Path     string `json:"path"`               // 匹配内容: 全站为 *;文件后缀如 jpg,png;目录/精确路径如 /foo/bar
	Type     string `json:"type"`               // 匹配类型: all/file_ext/directory/full_path
	TTL      int64  `json:"ttl"`                // 缓存时间(秒): >0 缓存时长;0 不缓存;-1 跟随源站
	Priority int    `json:"priority,omitempty"` // 优先级(可选,数字越大越优先)
}
