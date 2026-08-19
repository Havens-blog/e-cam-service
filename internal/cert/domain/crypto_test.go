package domain

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// testMasterKey 生成 32 字节随机主密钥（不依赖真实 env）。
func testMasterKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, masterKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test master key: %v", err)
	}
	return key
}

// 模拟私钥明文（非真实密钥材料，仅作载荷）。用于断言错误信息不泄露明文。
const testSecretPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAfake-secret-material-DO-NOT-LEAK-0001
-----END RSA PRIVATE KEY-----`

func TestEnvelopeCryptoRoundTrip(t *testing.T) {
	crypto, err := NewEnvelopeCrypto(map[int][]byte{1: testMasterKey(t)})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto() error = %v", err)
	}
	cases := []struct {
		name      string
		plaintext []byte
	}{
		{name: "pem_private_key_like", plaintext: []byte(testSecretPEM)},
		{name: "empty", plaintext: []byte{}},
		{name: "single_byte", plaintext: []byte{0x42}},
		{name: "kubeconfig_like_4kb", plaintext: bytes.Repeat([]byte("apiVersion: v1\nclusters:\n"), 256)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, keyVersion, err := crypto.Encrypt(tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}
			if keyVersion != 1 {
				t.Errorf("Encrypt() keyVersion = %d, want 1 (single configured version)", keyVersion)
			}
			if ciphertext == "" {
				t.Fatal("Encrypt() returned empty ciphertext")
			}
			got, err := crypto.Decrypt(ciphertext, keyVersion)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			defer Zeroize(&got)
			if !bytes.Equal(got, tc.plaintext) {
				t.Errorf("Decrypt() plaintext mismatch: got %d bytes, want %d bytes", len(got), len(tc.plaintext))
			}
		})
	}
}

func TestEnvelopeCryptoEncryptRandomNonce(t *testing.T) {
	crypto, err := NewEnvelopeCrypto(map[int][]byte{1: testMasterKey(t), 2: testMasterKey(t)})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto() error = %v", err)
	}
	plaintext := []byte(testSecretPEM)
	defer Zeroize(&plaintext)

	ciphertexts := make(map[string]struct{}, 8)
	for i := 0; i < 8; i++ {
		ciphertext, keyVersion, err := crypto.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt() #%d error = %v", i, err)
		}
		if keyVersion != 2 {
			t.Errorf("Encrypt() keyVersion = %d, want 2 (max configured version)", keyVersion)
		}
		ciphertexts[ciphertext] = struct{}{}
		got, err := crypto.Decrypt(ciphertext, keyVersion)
		if err != nil {
			t.Fatalf("Decrypt() #%d error = %v", i, err)
		}
		Zeroize(&got)
	}
	if len(ciphertexts) != 8 {
		t.Errorf("same plaintext encrypted 8 times produced %d distinct ciphertexts, want 8 (random nonce)", len(ciphertexts))
	}
}

// TestEnvelopeCryptoDualVersion 双版本并存：写路径固定最新版，双读支持旧版密文。
func TestEnvelopeCryptoDualVersion(t *testing.T) {
	keyV1, keyV2 := testMasterKey(t), testMasterKey(t)
	if bytes.Equal(keyV1, keyV2) {
		t.Fatal("test setup: generated identical master keys")
	}
	oldOnly, err := NewEnvelopeCrypto(map[int][]byte{1: keyV1})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto(old) error = %v", err)
	}
	dual, err := NewEnvelopeCrypto(map[int][]byte{1: keyV1, 2: keyV2})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto(dual) error = %v", err)
	}

	if got := dual.LatestVersion(); got != 2 {
		t.Errorf("dual.LatestVersion() = %d, want 2", got)
	}
	if got := dual.Versions(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("dual.Versions() = %v, want [1 2]", got)
	}

	plaintext := []byte(testSecretPEM)
	defer Zeroize(&plaintext)

	// 旧版本（keyVersion=1）密文
	ctV1, kvV1, err := oldOnly.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("oldOnly.Encrypt() error = %v", err)
	}
	if kvV1 != 1 {
		t.Fatalf("oldOnly.Encrypt() keyVersion = %d, want 1", kvV1)
	}

	// 双读：双版本组件可解密旧版密文
	got, err := dual.Decrypt(ctV1, 1)
	if err != nil {
		t.Fatalf("dual.Decrypt(v1 ciphertext) error = %v", err)
	}
	Zeroize(&got)

	// 写路径：双版本组件加密固定用最新版
	ctNew, kvNew, err := dual.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("dual.Encrypt() error = %v", err)
	}
	if kvNew != 2 {
		t.Errorf("dual.Encrypt() keyVersion = %d, want 2 (latest)", kvNew)
	}
	got, err = dual.Decrypt(ctNew, 2)
	if err != nil {
		t.Fatalf("dual.Decrypt(v2 ciphertext) error = %v", err)
	}
	Zeroize(&got)
}

// TestEnvelopeCryptoDecryptTampered 篡改/截断密文必须显式报错，不 panic、不返回部分明文。
func TestEnvelopeCryptoDecryptTampered(t *testing.T) {
	crypto, err := NewEnvelopeCrypto(map[int][]byte{1: testMasterKey(t)})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto() error = %v", err)
	}
	ciphertext, keyVersion, err := crypto.Encrypt([]byte(testSecretPEM))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}

	tamperAt := func(pos int) string {
		forged := bytes.Clone(raw)
		forged[pos] ^= 0x01
		return base64.StdEncoding.EncodeToString(forged)
	}
	cases := []struct {
		name       string
		ciphertext string
		wantErr    error
	}{
		{name: "tamper_nonce_head", ciphertext: tamperAt(0), wantErr: ErrCiphertextAuthFailed},
		{name: "tamper_nonce_tail", ciphertext: tamperAt(gcmNonceSize - 1), wantErr: ErrCiphertextAuthFailed},
		{name: "tamper_body", ciphertext: tamperAt(len(raw) / 2), wantErr: ErrCiphertextAuthFailed},
		{name: "tamper_tag_last_byte", ciphertext: tamperAt(len(raw) - 1), wantErr: ErrCiphertextAuthFailed},
		{name: "truncate_one_byte", ciphertext: base64.StdEncoding.EncodeToString(raw[:len(raw)-1]), wantErr: ErrCiphertextAuthFailed},
		{name: "truncate_tag", ciphertext: base64.StdEncoding.EncodeToString(raw[:len(raw)-16]), wantErr: ErrCiphertextAuthFailed},
		{name: "truncate_below_nonce_plus_tag", ciphertext: base64.StdEncoding.EncodeToString(raw[:gcmNonceSize+5]), wantErr: ErrInvalidCiphertext},
		{name: "empty_ciphertext", ciphertext: "", wantErr: ErrInvalidCiphertext},
		{name: "not_base64", ciphertext: ciphertext[:len(ciphertext)-2] + "!!", wantErr: ErrInvalidCiphertext},
		{name: "unknown_key_version", ciphertext: ciphertext, wantErr: ErrUnknownKeyVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := keyVersion
			if errors.Is(tc.wantErr, ErrUnknownKeyVersion) {
				version = 99
			}
			plaintext, err := crypto.Decrypt(tc.ciphertext, version)
			if err == nil {
				Zeroize(&plaintext)
				t.Fatal("Decrypt() succeeded on tampered ciphertext, want explicit error")
			}
			if plaintext != nil {
				Zeroize(&plaintext)
				t.Error("Decrypt() returned non-nil plaintext together with error, want nil (no partial plaintext)")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Decrypt() error = %v, want %v", err, tc.wantErr)
			}
			// 渗透式自查：错误 message 不得携带明文片段
			if strings.Contains(err.Error(), testSecretPEM) || strings.Contains(err.Error(), "DO-NOT-LEAK") {
				t.Errorf("Decrypt() error message leaks plaintext: %q", err.Error())
			}
		})
	}
}

func TestEnvelopeCryptoDecryptUnknownKeyVersion(t *testing.T) {
	crypto, err := NewEnvelopeCrypto(map[int][]byte{1: testMasterKey(t)})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto() error = %v", err)
	}
	ciphertext, _, err := crypto.Encrypt([]byte(testSecretPEM))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	for _, version := range []int{0, -1, 2, 99} {
		plaintext, err := crypto.Decrypt(ciphertext, version)
		if !errors.Is(err, ErrUnknownKeyVersion) {
			t.Errorf("Decrypt(version=%d) error = %v, want ErrUnknownKeyVersion", version, err)
		}
		if plaintext != nil {
			Zeroize(&plaintext)
			t.Errorf("Decrypt(version=%d) returned non-nil plaintext with error", version)
		}
	}
}

// TestRotationMigration 轮换迁移模拟：旧版密文 → 双读解密 → 新版再加密。
func TestRotationMigration(t *testing.T) {
	keyV1, keyV2 := testMasterKey(t), testMasterKey(t)
	preRotate, err := NewEnvelopeCrypto(map[int][]byte{1: keyV1})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto(pre-rotate) error = %v", err)
	}
	dual, err := NewEnvelopeCrypto(map[int][]byte{1: keyV1, 2: keyV2})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto(dual) error = %v", err)
	}
	postRotate, err := NewEnvelopeCrypto(map[int][]byte{2: keyV2})
	if err != nil {
		t.Fatalf("NewEnvelopeCrypto(post-rotate) error = %v", err)
	}

	plaintext := []byte(testSecretPEM)
	defer Zeroize(&plaintext)

	// 迁移前：旧版本加密的存量密文
	oldCT, oldKV, err := preRotate.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("preRotate.Encrypt() error = %v", err)
	}
	if oldKV != 1 {
		t.Fatalf("preRotate.Encrypt() keyVersion = %d, want 1", oldKV)
	}

	// 迁移：双读窗口内解密旧密文 → 以最新版本再加密
	decrypted, err := dual.Decrypt(oldCT, oldKV)
	if err != nil {
		t.Fatalf("dual.Decrypt(old ciphertext) error = %v (dual-read must work during rotation)", err)
	}
	newCT, newKV, err := dual.Encrypt(decrypted)
	Zeroize(&decrypted)
	if err != nil {
		t.Fatalf("dual.Encrypt() (re-encrypt) error = %v", err)
	}
	if newKV != 2 {
		t.Errorf("re-encrypted keyVersion = %d, want 2 (latest)", newKV)
	}
	if newCT == oldCT {
		t.Error("re-encrypted ciphertext identical to old ciphertext")
	}

	// 迁移后：仅驻留新版主密钥的组件可解密再加密密文，旧版密文因版本下线不可解
	got, err := postRotate.Decrypt(newCT, newKV)
	if err != nil {
		t.Fatalf("postRotate.Decrypt(migrated ciphertext) error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Error("post-rotation decrypt plaintext mismatch")
	}
	Zeroize(&got)
	if _, err := postRotate.Decrypt(oldCT, oldKV); !errors.Is(err, ErrUnknownKeyVersion) {
		t.Errorf("postRotate.Decrypt(old ciphertext) error = %v, want ErrUnknownKeyVersion (old key decommissioned)", err)
	}
}

func TestNewEnvelopeCryptoValidation(t *testing.T) {
	valid := testMasterKey(t)
	cases := []struct {
		name    string
		keys    map[int][]byte
		wantErr error
		wantNil bool
	}{
		{name: "no_keys_fail_fast", keys: map[int][]byte{}, wantErr: ErrMasterKeyNotConfigured, wantNil: true},
		{name: "version_zero", keys: map[int][]byte{0: valid}, wantErr: ErrInvalidMasterKey, wantNil: true},
		{name: "version_negative", keys: map[int][]byte{-1: valid}, wantErr: ErrInvalidMasterKey, wantNil: true},
		{name: "key_too_short", keys: map[int][]byte{1: valid[:31]}, wantErr: ErrInvalidMasterKey, wantNil: true},
		{name: "key_too_long", keys: map[int][]byte{1: append(bytes.Clone(valid), 0x00)}, wantErr: ErrInvalidMasterKey, wantNil: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			crypto, err := NewEnvelopeCrypto(tc.keys)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("NewEnvelopeCrypto() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantNil && crypto != nil {
				t.Error("NewEnvelopeCrypto() returned non-nil instance together with error")
			}
		})
	}
}

func TestLoadMasterKeys(t *testing.T) {
	keyV1, keyV2 := testMasterKey(t), testMasterKey(t)
	shortKey := base64.StdEncoding.EncodeToString(make([]byte, 16))
	cases := []struct {
		name     string
		environ  []string
		wantKeys map[int]int // version -> key byte length
		wantErr  error
	}{
		{
			name:     "no_cert_env_vars",
			environ:  []string{"PATH=/usr/bin", "HOME=/root", "EIAM_OTHER=x"},
			wantKeys: map[int]int{},
		},
		{
			name: "dual_versions_coexist",
			environ: []string{
				"PATH=/usr/bin",
				MasterKeyEnvPrefix + "1=" + base64.StdEncoding.EncodeToString(keyV1),
				MasterKeyEnvPrefix + "2=" + base64.StdEncoding.EncodeToString(keyV2),
			},
			wantKeys: map[int]int{1: masterKeySize, 2: masterKeySize},
		},
		{
			name:    "bad_suffix_not_number",
			environ: []string{MasterKeyEnvPrefix + "x=" + base64.StdEncoding.EncodeToString(keyV1)},
			wantErr: ErrInvalidMasterKey,
		},
		{
			name:    "bad_suffix_bare_prefix",
			environ: []string{MasterKeyEnvPrefix + "=" + base64.StdEncoding.EncodeToString(keyV1)},
			wantErr: ErrInvalidMasterKey,
		},
		{
			name:    "version_zero_rejected",
			environ: []string{MasterKeyEnvPrefix + "0=" + base64.StdEncoding.EncodeToString(keyV1)},
			wantErr: ErrInvalidMasterKey,
		},
		{
			name:    "empty_value_rejected",
			environ: []string{MasterKeyEnvPrefix + "1="},
			wantErr: ErrInvalidMasterKey,
		},
		{
			name:    "not_base64_rejected",
			environ: []string{MasterKeyEnvPrefix + "1=not-base64!!"},
			wantErr: ErrInvalidMasterKey,
		},
		{
			name:    "wrong_length_rejected",
			environ: []string{MasterKeyEnvPrefix + "1=" + shortKey},
			wantErr: ErrInvalidMasterKey,
		},
		{
			name: "duplicate_version_rejected",
			environ: []string{
				MasterKeyEnvPrefix + "1=" + base64.StdEncoding.EncodeToString(keyV1),
				MasterKeyEnvPrefix + "01=" + base64.StdEncoding.EncodeToString(keyV2),
			},
			wantErr: ErrInvalidMasterKey,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys, err := loadMasterKeys(tc.environ)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("loadMasterKeys() error = %v, want %v", err, tc.wantErr)
				}
				// 配置错误信息只含变量名，不含密钥材料
				if err != nil && (strings.Contains(err.Error(), base64.StdEncoding.EncodeToString(keyV1)) || strings.Contains(err.Error(), base64.StdEncoding.EncodeToString(keyV2))) {
					t.Errorf("loadMasterKeys() error leaks key material: %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("loadMasterKeys() error = %v", err)
			}
			if len(keys) != len(tc.wantKeys) {
				t.Fatalf("loadMasterKeys() got %d keys, want %d", len(keys), len(tc.wantKeys))
			}
			for version, wantLen := range tc.wantKeys {
				got, ok := keys[version]
				if !ok {
					t.Fatalf("loadMasterKeys() missing key version %d", version)
				}
				if len(got) != wantLen {
					t.Errorf("loadMasterKeys() key version %d length = %d, want %d", version, len(got), wantLen)
				}
			}
		})
	}
}

func TestNewEnvelopeCryptoFromEnv(t *testing.T) {
	keyV1, keyV2 := testMasterKey(t), testMasterKey(t)

	t.Run("dual_versions_from_env", func(t *testing.T) {
		t.Setenv(MasterKeyEnvPrefix+"1", base64.StdEncoding.EncodeToString(keyV1))
		t.Setenv(MasterKeyEnvPrefix+"2", base64.StdEncoding.EncodeToString(keyV2))
		crypto, err := NewEnvelopeCryptoFromEnv()
		if err != nil {
			t.Fatalf("NewEnvelopeCryptoFromEnv() error = %v", err)
		}
		if got := crypto.LatestVersion(); got != 2 {
			t.Errorf("LatestVersion() = %d, want 2", got)
		}
		plaintext := []byte(testSecretPEM)
		defer Zeroize(&plaintext)
		ciphertext, keyVersion, err := crypto.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		if keyVersion != 2 {
			t.Errorf("Encrypt() keyVersion = %d, want 2", keyVersion)
		}
		got, err := crypto.Decrypt(ciphertext, keyVersion)
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}
		Zeroize(&got)
	})

	t.Run("empty_value_fail_fast", func(t *testing.T) {
		t.Setenv(MasterKeyEnvPrefix+"1", "")
		if _, err := NewEnvelopeCryptoFromEnv(); !errors.Is(err, ErrInvalidMasterKey) {
			t.Errorf("NewEnvelopeCryptoFromEnv() error = %v, want ErrInvalidMasterKey", err)
		}
	})

	t.Run("no_keys_mapped_to_not_configured", func(t *testing.T) {
		// 环境变量列表中无任何 EIAM_CERT_MASTER_KEY_V* 时映射为"未配置主密钥"（fail-fast 语义）
		keys, err := loadMasterKeys([]string{"PATH=/usr/bin"})
		if err != nil {
			t.Fatalf("loadMasterKeys() error = %v", err)
		}
		if _, err := NewEnvelopeCrypto(keys); !errors.Is(err, ErrMasterKeyNotConfigured) {
			t.Errorf("NewEnvelopeCrypto(empty) error = %v, want ErrMasterKeyNotConfigured", err)
		}
	})
}

func TestZeroize(t *testing.T) {
	secret := []byte(testSecretPEM)
	backing := secret // 别名共享底层数组，用于验证清零确实写入内存
	if len(backing) == 0 {
		t.Fatal("test setup: empty secret")
	}
	Zeroize(&secret)
	if secret != nil {
		t.Error("Zeroize() did not release slice reference, want nil")
	}
	for i, b := range backing {
		if b != 0 {
			t.Fatalf("Zeroize() backing array byte %d = %#x, want 0", i, b)
		}
	}

	var nilPtr *[]byte
	Zeroize(nilPtr) // 不得 panic

	empty := []byte{}
	Zeroize(&empty)
	if empty != nil {
		t.Error("Zeroize() empty slice should also be released to nil")
	}
}

func TestAlgoConstantMatchesSchema(t *testing.T) {
	if AlgoAES256GCM != "AES-256-GCM" {
		t.Errorf("AlgoAES256GCM = %q, want %q (schema.sql algo enum)", AlgoAES256GCM, "AES-256-GCM")
	}
}

// ---------------------------------------------------------------------
// NewEnvelopeCryptoWithFallback（module.go 降级装配的域层支撑）
// ---------------------------------------------------------------------

func TestNewEnvelopeCryptoWithFallback_NoEnvUsesFallback(t *testing.T) {
	// 测试进程未注入 EIAM_CERT_MASTER_KEY_*，fallback 提供任意长度材料即可。
	raw := []byte("shared-security-encryption-key")
	c, source, err := NewEnvelopeCryptoWithFallback(func() ([]byte, bool) { return raw, true })
	if err != nil || source != MasterKeySourceFallback {
		t.Fatalf("expect fallback source, got source=%q err=%v", source, err)
	}
	ct, ver, err := c.Encrypt([]byte("secret"))
	if err != nil || ver != 1 {
		t.Fatalf("encrypt: ver=%d err=%v", ver, err)
	}
	if _, err := c.Decrypt(ct, ver); err != nil {
		t.Fatalf("roundtrip decrypt: %v", err)
	}
	// 派生确定性：同材料再派生一次应能解旧密文。
	c2, _, _ := NewEnvelopeCryptoWithFallback(func() ([]byte, bool) { return []byte("shared-security-encryption-key"), true })
	if _, err := c2.Decrypt(ct, ver); err != nil {
		t.Fatalf("re-derived key must decrypt old ciphertext: %v", err)
	}
}

func TestNewEnvelopeCryptoWithFallback_FallbackAbsentErrors(t *testing.T) {
	if _, _, err := NewEnvelopeCryptoWithFallback(func() ([]byte, bool) { return nil, false }); err == nil {
		t.Fatal("expect ErrMasterKeyNotConfigured")
	} else if !errors.Is(err, ErrMasterKeyNotConfigured) {
		t.Fatalf("expect ErrMasterKeyNotConfigured, got %v", err)
	}
	if _, _, err := NewEnvelopeCryptoWithFallback(func() ([]byte, bool) { return []byte{}, true }); err == nil {
		t.Fatal("empty material must also fail-fast")
	}
}

func TestNewEnvelopeCryptoWithFallback_EnvWins(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	t.Setenv("EIAM_CERT_MASTER_KEY_V1", base64.StdEncoding.EncodeToString(key))
	c, source, err := NewEnvelopeCryptoWithFallback(func() ([]byte, bool) { return []byte("fallback-ignored"), true })
	if err != nil || source != MasterKeySourceEnv {
		t.Fatalf("expect env source, got source=%q err=%v", source, err)
	}
	ct, ver, err := c.Encrypt([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	// env 密钥 ≠ fallback 派生密钥：fallback 派生实现解不开 env 密文。
	sum := sha256.Sum256([]byte("fallback-ignored"))
	cfb, _ := NewEnvelopeCrypto(map[int][]byte{1: sum[:]})
	if _, err := cfb.Decrypt(ct, ver); err == nil {
		t.Fatal("fallback-derived key must NOT decrypt env-key ciphertext")
	}
}

func TestNewEnvelopeCryptoWithFallback_BadEnvStillFails(t *testing.T) {
	t.Setenv("EIAM_CERT_MASTER_KEY_V1", "!!!not-base64!!!")
	if _, _, err := NewEnvelopeCryptoWithFallback(func() ([]byte, bool) { return []byte("ok"), true }); err == nil {
		t.Fatal("invalid env must fail-fast, never silently fall back")
	}
}
