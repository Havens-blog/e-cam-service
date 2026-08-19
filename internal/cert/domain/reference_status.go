package domain

// ReferenceStatus 引用三态派生量（DTO 派生，非存储字段；任务 2.3）。
// tech-design"引用状态语义"：**"未发现引用" ≠ "无引用"**——
// no_refs_scanned 仅表示"已扫描但无匹配"，可能因云 API 权限不足、产品未覆盖而漏报；
// blind_spot 表示无成功快照或扫描范围未覆盖，禁止据此放行删除。
type ReferenceStatus string

const (
	// RefStatusHasRefs 有引用：最新成功快照中该指纹的 CertReference 计数 > 0。
	RefStatusHasRefs ReferenceStatus = "has_refs"
	// RefStatusNoRefsScanned 未发现引用（扫描无匹配）：最新成功快照已覆盖该证书
	// 涉及的云/产品，且引用计数 = 0。
	RefStatusNoRefsScanned ReferenceStatus = "no_refs_scanned"
	// RefStatusBlindSpot 盲区：无成功快照，或该证书涉及的云/产品未纳入本期扫描范围。
	RefStatusBlindSpot ReferenceStatus = "blind_spot"
)
