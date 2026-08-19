package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

func now() time.Time { return time.Now() }

func testFingerprint(seed byte) string {
	fp := make([]byte, 64)
	hexDigits := "0123456789abcdef"
	for i := range fp {
		fp[i] = hexDigits[(int(seed)+i)%16]
	}
	return string(fp)
}

// TestCertificateRepo_CreateAndRoundTrip（集成）写入默认值填充 + 读回一致 + 指纹冲突哨兵。
func TestCertificateRepo_CreateAndRoundTrip(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCertificateRepository(db)

	cert := &domain.Certificate{
		Fingerprint:   testFingerprint(1),
		CommonName:    "example.com",
		Sans:          []string{"example.com", "www.example.com"},
		Issuer:        "Example CA",
		SerialNumber:  "00a1",
		NotBefore:     time.Now().Add(-24 * time.Hour),
		NotAfter:      time.Now().Add(24 * time.Hour),
		KeyAlgorithm:  domain.KeyAlgorithmRSA,
		HostingStatus: domain.HostingStatusComplete,
		EncryptedPrivateKey: &domain.EncryptedSecret{
			Ciphertext: "aGVsbG8=", KeyVersion: 1, Algo: domain.AlgoAES256GCM,
		},
	}
	require.NoError(t, repo.Create(ctx, cert))

	// DEFAULT 填充：createdAt/expiryAlertLevel
	assert.False(t, cert.CreatedAt.IsZero())
	assert.Equal(t, domain.ExpiryAlertNone, cert.ExpiryAlertLevel)

	got, err := repo.GetByFingerprint(ctx, cert.Fingerprint)
	require.NoError(t, err)
	assert.Equal(t, cert.Fingerprint, got.Fingerprint)
	assert.Equal(t, domain.HostingStatusComplete, got.HostingStatus)
	assert.Equal(t, domain.KeyAlgorithmRSA, got.KeyAlgorithm)
	require.NotNil(t, got.EncryptedPrivateKey)
	assert.Equal(t, "aGVsbG8=", got.EncryptedPrivateKey.Ciphertext)
	assert.Equal(t, 1, got.EncryptedPrivateKey.KeyVersion)
	assert.Equal(t, "AES-256-GCM", got.EncryptedPrivateKey.Algo)
	assert.WithinDuration(t, cert.NotAfter, got.NotAfter, time.Second)

	// 指纹唯一冲突 → ErrDuplicateFingerprint（供 2.2 映射 409）
	err = repo.Create(ctx, &domain.Certificate{
		Fingerprint:   cert.Fingerprint,
		HostingStatus: domain.HostingStatusFingerprintOnly,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrDuplicateFingerprint)

	// 仅指纹登记：密文对象缺省
	fpOnly := &domain.Certificate{
		Fingerprint:   testFingerprint(2),
		HostingStatus: domain.HostingStatusFingerprintOnly,
	}
	require.NoError(t, repo.Create(ctx, fpOnly))
	got2, err := repo.GetByFingerprint(ctx, fpOnly.Fingerprint)
	require.NoError(t, err)
	assert.Nil(t, got2.EncryptedPrivateKey)

	// List 返回全部台账
	list, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// 到期分级状态更新
	require.NoError(t, repo.UpdateExpiryAlertLevel(ctx, cert.Fingerprint, domain.ExpiryAlertL14))
	got3, err := repo.GetByFingerprint(ctx, cert.Fingerprint)
	require.NoError(t, err)
	assert.Equal(t, domain.ExpiryAlertL14, got3.ExpiryAlertLevel)

	// 删除
	require.NoError(t, repo.DeleteByFingerprint(ctx, fpOnly.Fingerprint))
	_, err = repo.GetByFingerprint(ctx, fpOnly.Fingerprint)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
}

// TestCertificateRepo_GetByIDAndAttachPrivateKey（集成）补传私钥升级路径：
// 按 ID 定位 + 密文写入与 hostingStatus=complete 同一原子 update。
func TestCertificateRepo_GetByIDAndAttachPrivateKey(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCertificateRepository(db)

	created := &domain.Certificate{
		Fingerprint:   testFingerprint(9),
		HostingStatus: domain.HostingStatusFingerprintOnly,
		CertPEM:       "-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----",
	}
	require.NoError(t, repo.Create(ctx, created))
	id := created.ID.Hex()

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, created.Fingerprint, got.Fingerprint)
	assert.Nil(t, got.EncryptedPrivateKey)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, got.HostingStatus)

	secret := &domain.EncryptedSecret{Ciphertext: "Y2lwaGVydGV4dA==", KeyVersion: 1, Algo: domain.AlgoAES256GCM}
	require.NoError(t, repo.AttachPrivateKey(ctx, id, secret))

	upgraded, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusComplete, upgraded.HostingStatus)
	require.NotNil(t, upgraded.EncryptedPrivateKey)
	assert.Equal(t, secret.Ciphertext, upgraded.EncryptedPrivateKey.Ciphertext)
	assert.Equal(t, 1, upgraded.EncryptedPrivateKey.KeyVersion)

	// 非法 ID / 未命中
	_, err = repo.GetByID(ctx, "not-hex")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
	err = repo.AttachPrivateKey(ctx, "not-hex", secret)
	assert.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.GetByID(ctx, "000000000000000000000000")
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
}

// TestK8sCredentialRepo_ClusterNameUnique（集成）集群名唯一冲突哨兵 + 密文形态往返。
func TestK8sCredentialRepo_ClusterNameUnique(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewK8sCredentialRepository(db)

	cred := &domain.K8sCredential{
		ClusterName: "prod-cluster",
		Kubeconfig: &domain.EncryptedSecret{
			Ciphertext: "a3ViZWNvbmZpZw==", KeyVersion: 2, Algo: domain.AlgoAES256GCM,
		},
		APIEndpoint: "https://k8s.example.com:6443",
	}
	require.NoError(t, repo.Create(ctx, cred))

	got, err := repo.GetByClusterName(ctx, "prod-cluster")
	require.NoError(t, err)
	require.NotNil(t, got.Kubeconfig)
	assert.Equal(t, "a3ViZWNvbmZpZw==", got.Kubeconfig.Ciphertext)
	assert.Equal(t, 2, got.Kubeconfig.KeyVersion)

	err = repo.Create(ctx, &domain.K8sCredential{
		ClusterName: "prod-cluster",
		Kubeconfig:  &domain.EncryptedSecret{Ciphertext: "eA==", KeyVersion: 1, Algo: domain.AlgoAES256GCM},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrDuplicateClusterName)

	require.NoError(t, repo.DeleteByClusterName(ctx, "prod-cluster"))
	_, err = repo.GetByClusterName(ctx, "prod-cluster")
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
}

// TestCertificateRepo_SetProtectUntil（集成，任务 5.1）
// 回滚保护期固化：缺省写入、更晚截止延长、更早截止不缩短、未命中无操作。
func TestCertificateRepo_SetProtectUntil(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewCertificateRepository(db)

	fp := testFingerprint(20)
	require.NoError(t, repo.Create(ctx, &domain.Certificate{
		Fingerprint:   fp,
		HostingStatus: domain.HostingStatusFingerprintOnly,
	}))

	// 缺省写入
	first := now().AddDate(0, 0, 7)
	require.NoError(t, repo.SetProtectUntil(ctx, fp, first))
	got, err := repo.GetByFingerprint(ctx, fp)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectUntil)
	assert.WithinDuration(t, first, *got.ProtectUntil, time.Second) // BSON date 毫秒精度

	// 更晚截止：延长
	later := first.Add(24 * time.Hour)
	require.NoError(t, repo.SetProtectUntil(ctx, fp, later))
	got, err = repo.GetByFingerprint(ctx, fp)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectUntil)
	assert.WithinDuration(t, later, *got.ProtectUntil, time.Second, "保护期只延长")

	// 更早截止：不缩短
	earlier := first.Add(-24 * time.Hour)
	require.NoError(t, repo.SetProtectUntil(ctx, fp, earlier))
	got, err = repo.GetByFingerprint(ctx, fp)
	require.NoError(t, err)
	require.NotNil(t, got.ProtectUntil)
	assert.WithinDuration(t, later, *got.ProtectUntil, time.Second, "保护期不缩短")

	// 未命中指纹：无操作不报错
	require.NoError(t, repo.SetProtectUntil(ctx, testFingerprint(21), first))
}
