package repository

import (
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
)

// Repositories cert 域全部仓储的聚合装配体（ioc/module 装配便捷入口）。
// 集合校验器与索引注册见 EnsureIndexes（module init 调用）。
type Repositories struct {
	Certificates   domain.CertificateRepository
	CertReferences domain.CertReferenceRepository
	ScanSnapshots  domain.ScanSnapshotRepository
	ChangeOrders   domain.ChangeOrderRepository
	ChangeItems    domain.ChangeItemRepository
	CloudMappings  domain.CloudCertMappingRepository
	ProbeResults   domain.ProbeResultRepository
	Exemptions     domain.ExemptionRepository
	AlertConfig    domain.AlertConfigRepository
	K8sCredentials domain.K8sCredentialRepository
	BatchSessions  domain.CertBatchSessionRepository
	CrdRegs        domain.CrdRegistrationRepository
	// DiscoveryImportSessions 云端发现导入会话仓储（cert-cloud-discovery-import 任务 2）。
	DiscoveryImportSessions domain.DiscoveryImportSessionRepository
}

// NewRepositories 装配全部 cert 仓储。
func NewRepositories(db *mongox.Mongo) *Repositories {
	return &Repositories{
		Certificates:            NewCertificateRepository(db),
		CertReferences:          NewCertReferenceRepository(db),
		ScanSnapshots:           NewScanSnapshotRepository(db),
		ChangeOrders:            NewChangeOrderRepository(db),
		ChangeItems:             NewChangeItemRepository(db),
		CloudMappings:           NewCloudCertMappingRepository(db),
		ProbeResults:            NewProbeResultRepository(db),
		Exemptions:              NewExemptionRepository(db),
		AlertConfig:             NewAlertConfigRepository(db),
		K8sCredentials:          NewK8sCredentialRepository(db),
		BatchSessions:           NewCertBatchSessionRepository(db),
		CrdRegs:                 NewCrdRegistrationRepository(db),
		DiscoveryImportSessions: NewDiscoveryImportSessionRepository(db),
	}
}
