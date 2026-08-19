package domain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// AlgoAES256GCM 信封加密算法标识，与 schema.sql 中 encryptedPrivateKey.algo /
// kubeconfig.algo 的 enum 保持一致；由仓储层随密文持久化到 algo 字段。
const AlgoAES256GCM = "AES-256-GCM"

// MasterKeyEnvPrefix 主密钥环境变量前缀：版本号由 env 后缀解析，
// 如 EIAM_CERT_MASTER_KEY_V1 / EIAM_CERT_MASTER_KEY_V2 并存（轮换双读窗口）。
// 主密钥仅经 env 注入、永不落库；迁移期不下线旧版本主密钥。
const MasterKeyEnvPrefix = "EIAM_CERT_MASTER_KEY_V"

const (
	// masterKeySize AES-256 主密钥字节长度；env 值为 base64(StdEncoding)，
	// 解码后必须恰好 32 字节，否则视为配置错误 fail-fast。
	masterKeySize = 32
	// gcmNonceSize AES-GCM 标准 nonce 长度；每次加密随机生成，
	// 前置于密文后整体 base64 输出。
	gcmNonceSize = 12
)

// 信封加密错误。所有错误 message 均为静态文案/版本号/长度等安全参数，
// 永不携带主密钥、密文对应明文或私钥/凭证片段。
var (
	// ErrMasterKeyNotConfigured 未配置任何版本的主密钥（启动期 fail-fast 依据）。
	ErrMasterKeyNotConfigured = errors.New("cert: master key not configured")
	// ErrInvalidMasterKey 主密钥配置非法（长度、编码、版本号、重复版本）。
	ErrInvalidMasterKey = errors.New("cert: invalid master key")
	// ErrUnknownKeyVersion 密文携带的主密钥版本当前未驻留（未配置或已下线）。
	ErrUnknownKeyVersion = errors.New("cert: unknown master key version")
	// ErrInvalidCiphertext 密文格式非法（非 base64、长度不足 nonce+tag）。
	ErrInvalidCiphertext = errors.New("cert: invalid ciphertext")
	// ErrCiphertextAuthFailed GCM 认证失败（密文被篡改/截断或主密钥不匹配）。
	ErrCiphertextAuthFailed = errors.New("cert: ciphertext authentication failed")
)

// EnvelopeCrypto AES-256-GCM 信封加密组件：加密私钥与 kubeconfig 等敏感载荷。
//
// 主密钥按 keyVersion 版本化驻留内存，支持轮换双读：
//   - Decrypt 按密文携带的 keyVersion 路由到对应主密钥（多版本并存均可解密）；
//   - Encrypt 固定使用已配置的最大版本（写路径始终写最新版）。
//
// 密文格式：base64(StdEncoding, nonce(12B) || AES-256-GCM(plaintext))，
// 与 schema.sql 中 {ciphertext, keyVersion, algo} 字段形状配套，algo 恒为 AES-256-GCM。
//
// 构造完成后内部状态只读，Encrypt/Decrypt 可并发调用。
// 构造时密钥即展开进 AES 轮密钥（cipher.AEAD），不再引用外部密钥切片；
// 主密钥不落库、不进日志，仅 env 注入（NewEnvelopeCryptoFromEnv）。
type EnvelopeCrypto struct {
	aeads  map[int]cipher.AEAD // keyVersion -> AEAD 实例（构造期创建，之后只读）
	latest int                 // 写路径主密钥版本 = 已配置的最大版本
}

// NewEnvelopeCrypto 以显式密钥表构造信封加密组件（测试与依赖注入用）。
// 传入的密钥切片在构造后不再被引用，调用方可自行 Zeroize 释放。
func NewEnvelopeCrypto(keys map[int][]byte) (*EnvelopeCrypto, error) {
	if len(keys) == 0 {
		return nil, ErrMasterKeyNotConfigured
	}
	aeads := make(map[int]cipher.AEAD, len(keys))
	latest := 0
	for version, key := range keys {
		if version < 1 {
			return nil, fmt.Errorf("%w: version %d out of range, expect >= 1", ErrInvalidMasterKey, version)
		}
		if len(key) != masterKeySize {
			return nil, fmt.Errorf("%w: version %d expects %d bytes, got %d", ErrInvalidMasterKey, version, masterKeySize, len(key))
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("cert: failed to init aes cipher for key version %d: %w", version, err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("cert: failed to init gcm for key version %d: %w", version, err)
		}
		aeads[version] = gcm
		if version > latest {
			latest = version
		}
	}
	return &EnvelopeCrypto{aeads: aeads, latest: latest}, nil
}

// NewEnvelopeCryptoFromEnv 从环境变量加载各版本主密钥并构造组件。
// 扫描所有 EIAM_CERT_MASTER_KEY_V<n> 变量（多版本并存），
// 任一变量配置非法（空值/非 base64/长度非 32 字节/版本号非法）即返回错误。
// 启动期（ioc 装配）必须调用本函数，出错即启动失败（fail-fast）；
// 一个版本都未配置同样返回 ErrMasterKeyNotConfigured。
func NewEnvelopeCryptoFromEnv() (*EnvelopeCrypto, error) {
	keys, err := loadMasterKeys(os.Environ())
	if err != nil {
		return nil, err
	}
	return NewEnvelopeCrypto(keys)
}

// loadMasterKeys 从环境变量键值对列表解析各版本主密钥。
// 错误信息仅含变量名与原因，永不包含变量值（主密钥材料）。
func loadMasterKeys(environ []string) (map[int][]byte, error) {
	keys := make(map[int][]byte)
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, MasterKeyEnvPrefix) {
			continue
		}
		suffix := strings.TrimPrefix(name, MasterKeyEnvPrefix)
		version, err := strconv.Atoi(suffix)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("%w: env %s suffix is not a valid key version", ErrInvalidMasterKey, name)
		}
		if _, dup := keys[version]; dup {
			return nil, fmt.Errorf("%w: env %s duplicates key version %d", ErrInvalidMasterKey, name, version)
		}
		if value == "" {
			return nil, fmt.Errorf("%w: env %s is empty", ErrInvalidMasterKey, name)
		}
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("%w: env %s is not valid base64: %v", ErrInvalidMasterKey, name, err)
		}
		if len(key) != masterKeySize {
			return nil, fmt.Errorf("%w: env %s decodes to %d bytes, expect %d", ErrInvalidMasterKey, name, len(key), masterKeySize)
		}
		keys[version] = key
	}
	return keys, nil
}

// Encrypt 用写路径主密钥（已配置的最大版本）以 AES-256-GCM 加密敏感载荷。
// nonce 每次随机生成，同一明文多次加密产生不同密文。
// 返回 base64 密文与其使用的主密钥版本（随密文持久化，供 Decrypt 双读路由）。
//
// 硬约束：明文与密文均不落日志/审计/响应体；调用方对明文用后必须 Zeroize。
func (c *EnvelopeCrypto) Encrypt(plaintext []byte) (ciphertext string, keyVersion int, err error) {
	gcm, ok := c.aeads[c.latest]
	if !ok {
		// 构造保证 latest 必有对应 AEAD，此处为防御性兜底
		return "", 0, fmt.Errorf("%w: %d", ErrUnknownKeyVersion, c.latest)
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", 0, fmt.Errorf("cert: failed to generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), c.latest, nil
}

// Decrypt 按密文携带的 keyVersion 路由到对应主密钥解密（轮换双读：
// 多版本主密钥并存时，任意已驻留版本的密文均可解密）。
//
// 认证失败（篡改/截断/主密钥不匹配）、未知版本、非法 base64 均返回显式错误，
// 不 panic、不返回部分明文（失败时返回 nil）。
// 返回的明文仅存内存，调用方使用完毕后必须 defer Zeroize(&plaintext)，
// 且严禁写入日志、错误 message、响应体或序列化为 string。
func (c *EnvelopeCrypto) Decrypt(ciphertext string, keyVersion int) ([]byte, error) {
	gcm, ok := c.aeads[keyVersion]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownKeyVersion, keyVersion)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: not valid base64", ErrInvalidCiphertext)
	}
	if len(raw) < gcmNonceSize+gcm.Overhead() {
		return nil, fmt.Errorf("%w: length %d is shorter than nonce+tag", ErrInvalidCiphertext, len(raw))
	}
	nonce, body := raw[:gcmNonceSize], raw[gcmNonceSize:]
	plaintext, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: keyVersion=%d: %w", ErrCiphertextAuthFailed, keyVersion, err)
	}
	return plaintext, nil
}

// LatestVersion 返回写路径主密钥版本（已配置的最大版本）。
func (c *EnvelopeCrypto) LatestVersion() int {
	return c.latest
}

// Versions 返回当前驻留的全部主密钥版本（升序），
// 供密钥迁移任务统计旧版本密文与确认双读窗口。
func (c *EnvelopeCrypto) Versions() []int {
	versions := make([]int, 0, len(c.aeads))
	for version := range c.aeads {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	return versions
}

// Zeroize 原地清零敏感数据并释放切片引用（明文用后置零）。
// 用法：defer Zeroize(&plaintext)。对 nil 指针与空切片安全。
// 清零通过指针写回底层数组，之后置 *b = nil 释放引用，
// 防止已置零缓冲被继续读用或明文引用逃逸。
func Zeroize(b *[]byte) {
	if b == nil {
		return
	}
	for i := range *b {
		(*b)[i] = 0
	}
	*b = nil
}
