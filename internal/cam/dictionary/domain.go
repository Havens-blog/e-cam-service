package dictionary

// DictType 字典类型
type DictType struct {
	ID          int64  `bson:"id" json:"id"`
	Code        string `bson:"code" json:"code"`
	Name        string `bson:"name" json:"name"`
	Description string `bson:"description" json:"description"`
	Status      string `bson:"status" json:"status"`
	TenantID    string `bson:"tenant_id" json:"tenant_id"`
	Ctime       int64  `bson:"ctime" json:"created_at"`
	Utime       int64  `bson:"utime" json:"updated_at"`
}

// DictItem 字典项
type DictItem struct {
	ID         int64                  `bson:"id" json:"id"`
	DictTypeID int64                  `bson:"dict_type_id" json:"dict_type_id"`
	Value      string                 `bson:"value" json:"value"`
	Label      string                 `bson:"label" json:"label"`
	SortOrder  int                    `bson:"sort_order" json:"sort_order"`
	Status     string                 `bson:"status" json:"status"`
	Extra      map[string]interface{} `bson:"extra" json:"extra"`
	Ctime      int64                  `bson:"ctime" json:"created_at"`
	Utime      int64                  `bson:"utime" json:"updated_at"`
}

// CreateTypeReq 创建字典类型请求
type CreateTypeReq struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateTypeReq 更新字典类型请求
type UpdateTypeReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateItemReq 创建字典项请求
type CreateItemReq struct {
	Value     string                 `json:"value"`
	Label     string                 `json:"label"`
	SortOrder int                    `json:"sort_order"`
	Extra     map[string]interface{} `json:"extra"`
}

// UpdateItemReq 更新字典项请求
type UpdateItemReq struct {
	Label     string                 `json:"label"`
	SortOrder int                    `json:"sort_order"`
	Status    string                 `json:"status"`
	Extra     map[string]interface{} `json:"extra"`
}

// TypeFilter 字典类型查询过滤条件
type TypeFilter struct {
	TenantID string `json:"tenant_id"`
	Keyword  string `json:"keyword"`
	Status   string `json:"status"`
	Offset   int64  `json:"offset"`
	Limit    int64  `json:"limit"`
}
