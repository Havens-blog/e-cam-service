package domain

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---- 测试夹具：Go 标准库现场生成（Hard：不落盘 fixture 文件）----

var fixtureSerialCounter atomic.Uint64

func nextSerial() *big.Int {
	return big.NewInt(int64(fixtureSerialCounter.Add(1)))
}

// certFixture 现场生成的证书夹具。cert 在 DER 故意不可解析（如畸形 SAN）时为 nil。
type certFixture struct {
	der  []byte
	pem  []byte
	cert *x509.Certificate
	key  crypto.Signer
}

func newECDSAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	return key
}

func newRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

func signFixture(t *testing.T, tmpl *x509.Certificate, parent *certFixture, key crypto.Signer) *certFixture {
	t.Helper()
	parentCert := tmpl
	var parentKey crypto.Signer = key
	if parent != nil {
		parentCert = parent.cert
		parentKey = parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, key.Public(), parentKey)
	if err != nil {
		t.Fatalf("create certificate %q: %v", tmpl.Subject.CommonName, err)
	}
	f := &certFixture{der: der, pem: pemEncode(t, "CERTIFICATE", der), key: key}
	cert, err := x509.ParseCertificate(der)
	if err == nil {
		f.cert = cert
	}
	return f
}

// newCA 现场生成 CA（parent=nil 时自签根）。
func newCA(t *testing.T, cn string, parent *certFixture, key crypto.Signer) *certFixture {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          nextSerial(),
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"e-cam-test"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(8760 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	return signFixture(t, tmpl, parent, key)
}

// newLeaf 现场生成 leaf 证书（mutate 可覆盖有效期/扩展等字段）。
func newLeaf(t *testing.T, cn string, dnsNames []string, key crypto.Signer, parent *certFixture, mutate func(*x509.Certificate)) *certFixture {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: nextSerial(),
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"e-cam-test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(8760 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
	}
	if mutate != nil {
		mutate(tmpl)
	}
	return signFixture(t, tmpl, parent, key)
}

func pemEncode(t *testing.T, typ string, der []byte) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func pkcs8KeyPEM(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8 key: %v", err)
	}
	return pemEncode(t, "PRIVATE KEY", der)
}

func pkcs1RSAKeyPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	return pemEncode(t, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))
}

func sec1ECKeyPEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal ec key: %v", err)
	}
	return pemEncode(t, "EC PRIVATE KEY", der)
}

func concatPEM(blocks ...[]byte) []byte {
	var out []byte
	for _, b := range blocks {
		out = append(out, b...)
	}
	return out
}

// certFixtures 一次性生成全部夹具（含中间 CA 链）。
type certFixtures struct {
	root, inter *certFixture

	leafEC    *certFixture // ECDSA leaf：多 SAN + 通配符 SAN
	leafRSA   *certFixture // RSA leaf
	leafECKey *ecdsa.PrivateKey

	selfSigned *certFixture // 自签 leaf（无中间链形态）
	expired    *certFixture
	future     *certFixture
	noSAN      *certFixture // 无 DNS SAN 且无 CN
	badSAN     *certFixture // SAN 扩展值畸形（不可解析）
	edLeaf     *certFixture // Ed25519 公钥 leaf（模型口径仅 RSA|ECDSA）
	ipLeaf     *certFixture // 仅 IP SAN（有 CN）

	otherECKey  *ecdsa.PrivateKey // 与 leafEC 无关的私钥（不匹配场景）
	otherRSAKey *rsa.PrivateKey   // 与 leafRSA 无关的私钥（RSA 模数不匹配场景）
}

func newCertFixtures(t *testing.T) *certFixtures {
	t.Helper()
	fx := &certFixtures{
		root:       newCA(t, "e-cam Test Root CA", nil, newECDSAKey(t)),
		leafECKey:  newECDSAKey(t),
		otherECKey: newECDSAKey(t),
	}
	fx.inter = newCA(t, "e-cam Test Intermediate CA", fx.root, newECDSAKey(t))

	fx.leafEC = newLeaf(t, "www.example.com",
		[]string{"www.example.com", "api.example.com", "*.wild.example.com"},
		fx.leafECKey, fx.inter, nil)

	rsaKey := newRSAKey(t)
	fx.leafRSA = newLeaf(t, "rsa.example.com", []string{"rsa.example.com"}, rsaKey, fx.inter, nil)
	fx.otherRSAKey = newRSAKey(t)

	fx.selfSigned = newLeaf(t, "self.example.com", []string{"self.example.com"}, fx.leafECKey, nil, nil)

	fx.expired = newLeaf(t, "expired.example.com", []string{"expired.example.com"}, fx.leafECKey, fx.inter,
		func(c *x509.Certificate) {
			c.NotBefore = time.Now().Add(-48 * time.Hour)
			c.NotAfter = time.Now().Add(-24 * time.Hour)
		})
	fx.future = newLeaf(t, "future.example.com", []string{"future.example.com"}, fx.leafECKey, fx.inter,
		func(c *x509.Certificate) {
			c.NotBefore = time.Now().Add(24 * time.Hour)
			c.NotAfter = time.Now().Add(48 * time.Hour)
		})
	fx.noSAN = newLeaf(t, "", nil, fx.leafECKey, fx.inter,
		func(c *x509.Certificate) { c.Subject = pkix.Name{} })
	fx.badSAN = newLeaf(t, "", nil, fx.leafECKey, fx.inter,
		func(c *x509.Certificate) {
			// OID 2.5.29.17 (subjectAltName) 携带非法 DER 值 → SAN 结构无法解析
			c.ExtraExtensions = []pkix.Extension{{
				Id:    asn1.ObjectIdentifier{2, 5, 29, 17},
				Value: []byte{0xFF, 0xFF, 0xFF},
			}}
		})
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	edTmpl := &x509.Certificate{
		SerialNumber: nextSerial(),
		Subject:      pkix.Name{CommonName: "ed.example.com", Organization: []string{"e-cam-test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(8760 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"ed.example.com"},
	}
	edDer, err := x509.CreateCertificate(rand.Reader, edTmpl, fx.inter.cert, edPub, fx.inter.key)
	if err != nil {
		t.Fatalf("create ed25519 leaf: %v", err)
	}
	edCert, _ := x509.ParseCertificate(edDer)
	fx.edLeaf = &certFixture{der: edDer, pem: pemEncode(t, "CERTIFICATE", edDer), cert: edCert, key: edPriv}

	fx.ipLeaf = newLeaf(t, "ip.example.com", nil, fx.leafECKey, fx.inter,
		func(c *x509.Certificate) { c.IPAddresses = []net.IP{net.ParseIP("10.0.0.1")} })
	return fx
}

// chain 完整链 PEM：leaf + 中间 CA + 自签根。
func (fx *certFixtures) chain(leaf *certFixture) []byte {
	return concatPEM(leaf.pem, fx.inter.pem, fx.root.pem)
}

// ---- AC6：表驱动主测试（四类拦截 + 通过路径）----

func TestParseCertAndKey(t *testing.T) {
	fx := newCertFixtures(t)

	tests := []struct {
		name    string
		certPEM []byte
		keyPEM  []byte
		wantErr string // CERT_* 错误码；空 = 期待成功
		check   func(t *testing.T, p *ParsedCert)
	}{
		{
			name:    "self-signed leaf with matching key passes",
			certPEM: fx.selfSigned.pem,
			keyPEM:  sec1ECKeyPEM(t, fx.selfSigned.key.(*ecdsa.PrivateKey)),
			check: func(t *testing.T, p *ParsedCert) {
				if p.KeyAlgorithm != KeyAlgorithmECDSA {
					t.Errorf("KeyAlgorithm = %q, want ECDSA", p.KeyAlgorithm)
				}
				if !slices.Contains(p.Sans, "self.example.com") {
					t.Errorf("Sans = %v, want contains self.example.com", p.Sans)
				}
			},
		},
		{
			name:    "full chain leaf+intermediate+root with matching ECDSA key passes",
			certPEM: fx.chain(fx.leafEC),
			keyPEM:  pkcs8KeyPEM(t, fx.leafECKey),
			check: func(t *testing.T, p *ParsedCert) {
				if p.KeyAlgorithm != KeyAlgorithmECDSA {
					t.Errorf("KeyAlgorithm = %q, want ECDSA", p.KeyAlgorithm)
				}
			},
		},
		{
			name:    "full chain RSA leaf with matching RSA key passes",
			certPEM: fx.chain(fx.leafRSA),
			keyPEM:  pkcs1RSAKeyPEM(t, fx.leafRSA.key.(*rsa.PrivateKey)),
			check: func(t *testing.T, p *ParsedCert) {
				if p.KeyAlgorithm != KeyAlgorithmRSA {
					t.Errorf("KeyAlgorithm = %q, want RSA", p.KeyAlgorithm)
				}
			},
		},
		{
			name:    "fingerprint only without key passes (no error)",
			certPEM: fx.chain(fx.leafEC),
			keyPEM:  nil,
		},
		{
			name:    "mismatched ECDSA key is rejected",
			certPEM: fx.chain(fx.leafEC),
			keyPEM:  pkcs8KeyPEM(t, fx.otherECKey),
			wantErr: CodeCertKeyMismatch,
		},
		{
			name:    "RSA certificate with ECDSA key is rejected",
			certPEM: fx.chain(fx.leafRSA),
			keyPEM:  pkcs8KeyPEM(t, fx.leafECKey),
			wantErr: CodeCertKeyMismatch,
		},
		{
			name:    "mismatched RSA key (modulus differs) is rejected",
			certPEM: fx.chain(fx.leafRSA),
			keyPEM:  pkcs1RSAKeyPEM(t, fx.otherRSAKey),
			wantErr: CodeCertKeyMismatch,
		},
		{
			name:    "unsupported key PEM block type is rejected",
			certPEM: fx.chain(fx.leafEC),
			keyPEM:  pemEncode(t, "PUBLIC KEY", fx.leafEC.cert.RawSubjectPublicKeyInfo),
			wantErr: CodeCertParseFail,
		},
		{
			name:    "leaf without any chain is rejected",
			certPEM: fx.leafEC.pem,
			keyPEM:  pkcs8KeyPEM(t, fx.leafECKey),
			wantErr: CodeCertChainIncomplete,
		},
		{
			name:    "chain missing intermediate CA is rejected",
			certPEM: concatPEM(fx.leafEC.pem, fx.root.pem),
			keyPEM:  pkcs8KeyPEM(t, fx.leafECKey),
			wantErr: CodeCertChainIncomplete,
		},
		{
			name:    "expired certificate is rejected",
			certPEM: fx.chain(fx.expired),
			keyPEM:  nil,
			wantErr: CodeCertParseFail,
		},
		{
			name:    "not yet valid certificate is rejected",
			certPEM: fx.chain(fx.future),
			keyPEM:  nil,
			wantErr: CodeCertParseFail,
		},
		{
			name:    "certificate without DNS SAN and common name is rejected",
			certPEM: fx.chain(fx.noSAN),
			keyPEM:  nil,
			wantErr: CodeCertParseFail,
		},
		{
			name:    "malformed SAN extension is rejected",
			certPEM: fx.chain(fx.badSAN),
			keyPEM:  nil,
			wantErr: CodeCertParseFail,
		},
		{
			name:    "garbage cert PEM is rejected",
			certPEM: []byte("this is not a pem at all"),
			keyPEM:  nil,
			wantErr: CodeCertParseFail,
		},
		{
			name:    "PEM without certificate block is rejected",
			certPEM: pemEncode(t, "CERTIFICATE REQUEST", []byte{0x30, 0x00}),
			keyPEM:  nil,
			wantErr: CodeCertParseFail,
		},
		{
			name:    "garbage key PEM is rejected",
			certPEM: fx.chain(fx.leafEC),
			keyPEM:  []byte("garbage key"),
			wantErr: CodeCertParseFail,
		},
		{
			name:    "encrypted key PEM is rejected",
			certPEM: fx.chain(fx.leafEC),
			keyPEM:  pemEncode(t, "ENCRYPTED PRIVATE KEY", []byte{0x01}),
			wantErr: CodeCertParseFail,
		},
		{
			name:    "unsupported leaf key algorithm is rejected",
			certPEM: fx.chain(fx.edLeaf),
			keyPEM:  nil,
			wantErr: CodeCertParseFail,
		},
		{
			name:    "IP SAN only parses fine and is not counted as domain SAN",
			certPEM: fx.chain(fx.ipLeaf),
			keyPEM:  nil,
			check: func(t *testing.T, p *ParsedCert) {
				if len(p.Sans) != 0 {
					t.Errorf("Sans = %v, want empty (IP SAN 不计入域名口径)", p.Sans)
				}
				if p.CommonName != "ip.example.com" {
					t.Errorf("CommonName = %q, want ip.example.com", p.CommonName)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseCertAndKey(tt.certPEM, tt.keyPEM)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseCertAndKey() unexpected error: %v", err)
				}
				if p == nil {
					t.Fatal("ParseCertAndKey() returned nil ParsedCert without error")
				}
				if tt.check != nil {
					tt.check(t, p)
				}
				return
			}

			if err == nil {
				t.Fatalf("ParseCertAndKey() expected error code %s, got nil (parsed=%+v)", tt.wantErr, p)
			}
			ce, ok := AsCertError(err)
			if !ok {
				t.Fatalf("ParseCertAndKey() error does not carry CertError: %v", err)
			}
			if ce.Code() != tt.wantErr {
				t.Fatalf("ParseCertAndKey() error code = %s, want %s (err: %v)", ce.Code(), tt.wantErr, err)
			}
			assertNoKeyMaterial(t, err, tt.keyPEM, tt.certPEM)
		})
	}
}

// assertNoKeyMaterial Hard Rule：错误 message 不得含私钥/密文/PEM 片段。
func assertNoKeyMaterial(t *testing.T, err error, keyPEM, certPEM []byte) {
	t.Helper()
	msg := err.Error()
	if strings.Contains(msg, "-----BEGIN") {
		t.Errorf("error message leaks PEM block: %q", msg)
	}
	if len(keyPEM) > 0 && strings.Contains(msg, string(keyPEM)) {
		t.Errorf("error message leaks private key PEM content")
	}
	if len(certPEM) > 0 && strings.Contains(msg, string(certPEM)) {
		t.Errorf("error message leaks certificate PEM content")
	}
}

// ---- AC1：要素解析字段逐一断言 ----

func TestParseCertAndKeyExtractsFields(t *testing.T) {
	fx := newCertFixtures(t)
	p, err := ParseCertAndKey(fx.chain(fx.leafEC), pkcs8KeyPEM(t, fx.leafECKey))
	if err != nil {
		t.Fatalf("ParseCertAndKey() error: %v", err)
	}

	// SHA256 指纹：64 位小写 hex，与现场独立计算一致
	sum := sha256.Sum256(fx.leafEC.der)
	wantFP := hex.EncodeToString(sum[:])
	if p.Fingerprint != wantFP {
		t.Errorf("Fingerprint = %q, want %q", p.Fingerprint, wantFP)
	}
	if len(p.Fingerprint) != 64 || p.Fingerprint != strings.ToLower(p.Fingerprint) {
		t.Errorf("Fingerprint = %q, want 64-char lowercase hex", p.Fingerprint)
	}

	if p.CommonName != "www.example.com" {
		t.Errorf("CommonName = %q, want www.example.com", p.CommonName)
	}
	got := slices.Clone(p.Sans)
	slices.Sort(got)
	want := []string{"*.wild.example.com", "api.example.com", "www.example.com"}
	if !slices.Equal(got, want) {
		t.Errorf("Sans = %v, want %v", got, want)
	}
	if !strings.Contains(p.Issuer, "CN=e-cam Test Intermediate CA") {
		t.Errorf("Issuer = %q, want contains intermediate CA CN", p.Issuer)
	}
	if p.SerialNumber != fx.leafEC.cert.SerialNumber.String() {
		t.Errorf("SerialNumber = %q, want %q", p.SerialNumber, fx.leafEC.cert.SerialNumber.String())
	}
	// x509 时间编码精确到秒，按 Unix 秒比对
	if p.NotBefore.Unix() != fx.leafEC.cert.NotBefore.Unix() {
		t.Errorf("NotBefore = %v, want %v", p.NotBefore, fx.leafEC.cert.NotBefore)
	}
	if p.NotAfter.Unix() != fx.leafEC.cert.NotAfter.Unix() {
		t.Errorf("NotAfter = %v, want %v", p.NotAfter, fx.leafEC.cert.NotAfter)
	}
	if p.KeyAlgorithm != KeyAlgorithmECDSA {
		t.Errorf("KeyAlgorithm = %q, want ECDSA", p.KeyAlgorithm)
	}
}

// ---- Hard Rule：输入缓冲区归调用方所有（2.2 校验后仍需对 keyPEM 加密落库）----

func TestParseCertAndKeyDoesNotMutateInputs(t *testing.T) {
	fx := newCertFixtures(t)
	keyPEM := pkcs8KeyPEM(t, fx.leafECKey)
	mismatchPEM := pkcs8KeyPEM(t, fx.otherECKey)

	for name, tc := range map[string]struct {
		certPEM, keyPEM []byte
	}{
		"success path":  {fx.chain(fx.leafEC), keyPEM},
		"mismatch path": {fx.chain(fx.leafEC), mismatchPEM},
	} {
		t.Run(name, func(t *testing.T) {
			certCopy := slices.Clone(tc.certPEM)
			keyCopy := slices.Clone(tc.keyPEM)
			_, _ = ParseCertAndKey(tc.certPEM, tc.keyPEM)
			if !slices.Equal(tc.certPEM, certCopy) {
				t.Error("certPEM was mutated by ParseCertAndKey")
			}
			if !slices.Equal(tc.keyPEM, keyCopy) {
				t.Error("keyPEM was mutated by ParseCertAndKey")
			}
		})
	}
}

// ---- AC4：CheckSANCover 仅提示性比对 ----

func TestCheckSANCover(t *testing.T) {
	sans := []string{"www.example.com", "API.Example.com", "*.wild.example.com"}

	tests := []struct {
		name     string
		sans     []string
		expected []string
		want     []string
	}{
		{"all covered", sans, []string{"www.example.com", "api.example.com"}, nil},
		{"case-insensitive exact match", sans, []string{"WWW.EXAMPLE.COM"}, nil},
		{"wildcard covers single label", sans, []string{"a.wild.example.com"}, nil},
		{"wildcard does not cover bare domain", sans, []string{"wild.example.com"}, []string{"wild.example.com"}},
		{"wildcard does not cover two labels", sans, []string{"a.b.wild.example.com"}, []string{"a.b.wild.example.com"}},
		{"unknown domain missing", sans, []string{"blog.example.com"}, []string{"blog.example.com"}},
		{"empty expected yields no missing", sans, nil, nil},
		{"empty sans misses everything", nil, []string{"a.com", "b.com"}, []string{"a.com", "b.com"}},
		{"missing preserves expected order", sans, []string{"z.example.com", "api.example.com", "y.example.com"}, []string{"z.example.com", "y.example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckSANCover(tt.sans, tt.expected)
			if !slices.Equal(got, tt.want) {
				t.Errorf("CheckSANCover() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- AC5：错误实现 error 接口并携带错误码 ----

func TestCertErrorCodeAndWrap(t *testing.T) {
	all := []struct {
		err  *CertError
		code string
	}{
		{ErrKeyMismatch, "CERT_KEY_MISMATCH"},
		{ErrChainIncomplete, "CERT_CHAIN_INCOMPLETE"},
		{ErrParseFail, "CERT_PARSE_FAIL"},
	}
	for _, tt := range all {
		var e error = tt.err // 实现 error 接口
		if e.Error() == "" {
			t.Errorf("CertError %s has empty message", tt.code)
		}
		if !strings.HasPrefix(e.Error(), "cert: ") {
			t.Errorf("CertError %s message = %q, want \"cert: \" prefix", tt.code, e.Error())
		}
		if tt.err.Code() != tt.code {
			t.Errorf("Code() = %s, want %s", tt.err.Code(), tt.code)
		}
	}

	// %w 包装保留错误码与哨兵身份（供 2.2 web 层 errors.Is/errors.As 映射）
	wrapped := fmt.Errorf("import failed: %w", ErrKeyMismatch)
	if !errors.Is(wrapped, ErrKeyMismatch) {
		t.Error("errors.Is(wrapped, ErrKeyMismatch) = false, want true")
	}
	ce, ok := AsCertError(wrapped)
	if !ok || ce.Code() != "CERT_KEY_MISMATCH" {
		t.Errorf("AsCertError(wrapped) = %v, %v; want CERT_KEY_MISMATCH, true", ce, ok)
	}

	if ce, ok := AsCertError(errors.New("plain")); ok {
		t.Errorf("AsCertError(plain error) = %v, want false", ce)
	}
}

func TestParseCertForInventory(t *testing.T) {
	fx := newCertFixtures(t)
	tests := []struct {
		name    string
		certPEM []byte
		wantErr string // 空=期望成功
		wantIssue MaterialIssue
	}{
		{name: "完整有效证书无标记", certPEM: fx.chain(fx.leafEC), wantIssue: ""},
		{name: "已过期证书标记 expired", certPEM: fx.chain(fx.expired), wantIssue: MaterialIssueExpired},
		{name: "未生效证书标记 expired", certPEM: fx.chain(fx.future), wantIssue: MaterialIssueExpired},
		{name: "缺链证书标记 chain_incomplete", certPEM: fx.leafEC.pem, wantIssue: MaterialIssueChainIncomplete},
		{name: "缺中间链标记 chain_incomplete", certPEM: concatPEM(fx.leafEC.pem, fx.root.pem), wantIssue: MaterialIssueChainIncomplete},
		{name: "过期且缺链优先 expired", certPEM: fx.expired.pem, wantIssue: MaterialIssueExpired},
		{name: "无 SAN 无 CN 仍拒绝", certPEM: fx.chain(fx.noSAN), wantErr: CodeCertParseFail},
		{name: "非法 PEM 仍拒绝", certPEM: []byte("not a pem"), wantErr: CodeCertParseFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, issue, err := ParseCertForInventory(tt.certPEM)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("期望错误 %s，实际成功", tt.wantErr)
				}
				var ce *CertError
				if !errors.As(err, &ce) || ce.Code() != tt.wantErr {
					t.Fatalf("错误码 = %v, want %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if parsed.Fingerprint == "" {
				t.Fatal("fingerprint 为空")
			}
			if issue != tt.wantIssue {
				t.Fatalf("issue = %q, want %q", issue, tt.wantIssue)
			}
		})
	}
}
