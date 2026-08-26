package domain

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ParsedCert 证书解析产物：要素字段与 Certificate 台账模型 1:1 对应
// （fingerprint/commonName/sans/issuer/serialNumber/notBefore/notAfter/keyAlgorithm），
// 供 2.2 导入服务落库与 4.4 巡检复检复用。
//
// 硬约束：不携带任何私钥材料——私钥仅在 ParseCertAndKey 内部完成公钥比对，不进入返回值。
type ParsedCert struct {
	Fingerprint  string       // SHA256(leaf.Raw) 的小写 hex（64 字符），台账聚合主键
	CommonName   string       // leaf Subject CN
	Sans         []string     // DNS SAN 列表；IP SAN 不计入域名口径
	Issuer       string       // 签发者 DN（RFC 2253 文本形式）
	SerialNumber string       // 序列号（十进制文本）
	NotBefore    time.Time    // 有效期起始
	NotAfter     time.Time    // 有效期截止
	KeyAlgorithm KeyAlgorithm // RSA | ECDSA（模型口径，其余算法拒绝）
}

// ParseCertAndKey 证书解析与完整性校验（纯函数：不访问 DB、不发网络请求）。
//
// certPEM 为 PEM 证书束：首个 CERTIFICATE 块视为 leaf，其余按中间链/自签根参与链校验；
// keyPEM 为可选私钥 PEM（PKCS#8 / PKCS#1 / SEC1），空值走 fingerprint_only 形态（不校验私钥、不报错）。
//
// 校验顺序（前序失败短路，四类拦截码见 errors.go）：
//  1. PEM/x509 解析 + keyAlgorithm（RSA|ECDSA）     → CERT_PARSE_FAIL
//  2. 有效期（已过期/未生效）                        → CERT_PARSE_FAIL
//  3. SAN 结构（无 DNS SAN 且无 CN 视为结构异常）    → CERT_PARSE_FAIL
//  4. 证书链完整性（链可构建验证到自签根）           → CERT_CHAIN_INCOMPLETE
//  5. 私钥匹配（证书公钥 vs 私钥派生公钥）           → CERT_KEY_MISMATCH
//
// 链完整性口径：leaf 自签即视为完整；否则证书束内必须存在自签根作为信任锚，
// 且 leaf 经中间链可构建通过签名验证的路径（缺中间链或缺根均拦截）。
//
// Hard Rules：
//   - 私钥明文仅在函数内比对后即清零（复用 1.1 Zeroize）——函数自有的 DER 副本比对后置零；
//     输入 keyPEM 缓冲区归调用方所有（2.2 校验通过后仍需对其信封加密落库），本函数不做改写。
//   - 任何错误 message/日志不得含私钥或密文片段：仅静态文案与时间/算法名等安全参数。
func ParseCertAndKey(certPEM, keyPEM []byte) (*ParsedCert, error) {
	certs, err := decodeCertificates(certPEM)
	if err != nil {
		return nil, err
	}
	leaf := certs[0]

	parsed, err := buildParsedCert(leaf)
	if err != nil {
		return nil, err
	}
	if err := checkValidity(leaf); err != nil {
		return nil, err
	}
	if err := checkSANStructure(leaf); err != nil {
		return nil, err
	}
	if err := checkChain(certs); err != nil {
		return nil, err
	}
	if len(keyPEM) > 0 {
		if err := checkKeyMatch(leaf, keyPEM); err != nil {
			return nil, err
		}
	}
	return parsed, nil
}

// ParseCertForInventory 盘点容忍解析（发现导入专用，cert-cloud-discovery-import
// 联调后产品决策）：存量回溯登记的口径是"云侧有什么就登记什么"——
// 已过期/未生效与证书链不完整不再拦截，改为返回 MaterialIssue 标记由台账留痕。
//
// 仍严格拦截（无法登记）：PEM/x509 非法、非 RSA/ECDSA 算法、无 SAN 且无 CN
// （结构异常的解析产物不可信）。
//
// 标记优先级：expired > chain_incomplete（同时命中记运营主导事实——过期证书
// 的处置动作是换证，链缺陷随换证材料一并解决）。
func ParseCertForInventory(certPEM []byte) (*ParsedCert, MaterialIssue, error) {
	certs, err := decodeCertificates(certPEM)
	if err != nil {
		return nil, "", err
	}
	leaf := certs[0]

	parsed, err := buildParsedCert(leaf)
	if err != nil {
		return nil, "", err
	}
	if err := checkSANStructure(leaf); err != nil {
		return nil, "", err
	}

	issue := MaterialIssue("")
	if err := checkValidity(leaf); err != nil {
		issue = MaterialIssueExpired
	}
	if err := checkChain(certs); err != nil && issue == "" {
		issue = MaterialIssueChainIncomplete
	}
	return parsed, issue, nil
}

// CheckSANCover 比对 SAN 列表对期望域名的覆盖情况，返回缺失清单。
// 仅提示性比对、不拦截——强制拦截点在 5.2 变更清单生成预检（SAN ⊇ 目标域名）。
//
// 匹配规则：大小写不敏感精确匹配；通配符 SAN（*.example.com）按单标签通配——
// 覆盖 a.example.com，不覆盖裸域 example.com 与多级 a.b.example.com。
// 缺失清单保持期望清单的原始顺序与写法。
func CheckSANCover(sans, expected []string) (missing []string) {
	exact := make(map[string]struct{}, len(sans))
	var wildcardSuffixes []string
	for _, san := range sans {
		s := strings.ToLower(strings.TrimSpace(san))
		if suffix, ok := strings.CutPrefix(s, "*."); ok && suffix != "" {
			wildcardSuffixes = append(wildcardSuffixes, suffix)
			continue
		}
		exact[s] = struct{}{}
	}
	for _, want := range expected {
		w := strings.ToLower(strings.TrimSpace(want))
		if _, ok := exact[w]; ok {
			continue
		}
		if label, rest, found := strings.Cut(w, "."); found && label != "" && slices.Contains(wildcardSuffixes, rest) {
			continue
		}
		missing = append(missing, want)
	}
	return missing
}

// decodeCertificates 解析证书束中的全部 CERTIFICATE 块（跳过其余 PEM 块类型）。
func decodeCertificates(certPEM []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := certPEM
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remaining
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid certificate block #%d: %v", ErrParseFail, len(certs)+1, err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("%w: no CERTIFICATE PEM block found", ErrParseFail)
	}
	return certs, nil
}

// buildParsedCert 提取 leaf 要素字段并计算 SHA256 指纹（小写 hex）。
func buildParsedCert(leaf *x509.Certificate) (*ParsedCert, error) {
	alg, err := keyAlgorithmOf(leaf.PublicKey)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(leaf.Raw)
	return &ParsedCert{
		Fingerprint:  hex.EncodeToString(sum[:]),
		CommonName:   leaf.Subject.CommonName,
		Sans:         append([]string(nil), leaf.DNSNames...),
		Issuer:       leaf.Issuer.String(),
		SerialNumber: leaf.SerialNumber.String(),
		NotBefore:    leaf.NotBefore,
		NotAfter:     leaf.NotAfter,
		KeyAlgorithm: alg,
	}, nil
}

// keyAlgorithmOf 映射公钥算法到模型枚举（RSA|ECDSA）；其余算法（如 Ed25519）不在
// schema.sql keyAlgorithm enum 口径内，按解析失败拦截。
func keyAlgorithmOf(pub any) (KeyAlgorithm, error) {
	switch pub.(type) {
	case *rsa.PublicKey:
		return KeyAlgorithmRSA, nil
	case *ecdsa.PublicKey:
		return KeyAlgorithmECDSA, nil
	default:
		return "", fmt.Errorf("%w: unsupported public key algorithm %T (expect RSA or ECDSA)", ErrParseFail, pub)
	}
}

// checkValidity 有效期校验：已过期或未生效 → CERT_PARSE_FAIL（PRD 完整性四项之一）。
func checkValidity(leaf *x509.Certificate) error {
	now := time.Now()
	if leaf.NotAfter.Before(now) {
		return fmt.Errorf("%w: certificate expired at %s", ErrParseFail, leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if leaf.NotBefore.After(now) {
		return fmt.Errorf("%w: certificate not yet valid until %s", ErrParseFail, leaf.NotBefore.UTC().Format(time.RFC3339))
	}
	return nil
}

// checkSANStructure SAN 结构校验：leaf 无任何域名标识（DNS SAN 与 CN 均缺失）视为
// 结构异常。畸形 SAN 扩展已在 x509.ParseCertificate 阶段拦截（decodeCertificates）。
// IP SAN 不计入域名口径但本身不触发错误。
func checkSANStructure(leaf *x509.Certificate) error {
	if len(leaf.DNSNames) == 0 && leaf.Subject.CommonName == "" {
		return fmt.Errorf("%w: certificate has no DNS SAN and no common name", ErrParseFail)
	}
	return nil
}

// checkChain 证书链完整性校验：leaf 自签即完整；否则证书束内须有自签根作信任锚，
// 且 leaf 经中间链可构建通过验证的路径（缺中间链/缺根 → CERT_CHAIN_INCOMPLETE）。
func checkChain(certs []*x509.Certificate) error {
	leaf := certs[0]
	if isSelfSigned(leaf) {
		return nil
	}
	roots := x509.NewCertPool()
	intermediates := x509.NewCertPool()
	hasAnchor := false
	for _, c := range certs[1:] {
		if isSelfSigned(c) {
			roots.AddCert(c)
			hasAnchor = true
		} else {
			intermediates.AddCert(c)
		}
	}
	if !hasAnchor {
		return fmt.Errorf("%w: %d certificate(s) provided without self-signed root anchor", ErrChainIncomplete, len(certs))
	}
	// 有效期已单独显式校验；链构建取 leaf 有效期中点作验证时钟，避免过期干扰链缺失判定
	currentTime := leaf.NotBefore.Add(leaf.NotAfter.Sub(leaf.NotBefore) / 2)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   currentTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return fmt.Errorf("%w: chain does not verify: %v", ErrChainIncomplete, err)
	}
	return nil
}

// isSelfSigned 判定自签：Subject 与 Issuer DER 一致且自签名验证通过。
func isSelfSigned(c *x509.Certificate) bool {
	return bytes.Equal(c.RawSubject, c.RawIssuer) &&
		c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) == nil
}

// checkKeyMatch 私钥匹配：证书公钥与私钥派生公钥比对（RSA 模数/指数、ECDSA 曲线点），
// 不匹配 → CERT_KEY_MISMATCH；算法不同（证书 RSA/私钥 ECDSA 等）同判不匹配。
func checkKeyMatch(leaf *x509.Certificate, keyPEM []byte) error {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return fmt.Errorf("%w: no PEM private key block found", ErrParseFail)
	}
	if block.Type == "ENCRYPTED PRIVATE KEY" || len(block.Headers) > 0 {
		return fmt.Errorf("%w: encrypted private keys are not supported", ErrParseFail)
	}
	// 函数自有私钥 DER 副本：仅用于公钥派生比对，比对后即清零（复用 1.1 Zeroize）；
	// 输入 keyPEM 缓冲区归调用方所有（2.2 校验通过后仍需对其加密落库），不做改写。
	der := append([]byte(nil), block.Bytes...)
	defer Zeroize(&der)

	priv, err := parsePrivateKey(block.Type, der)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrParseFail, err)
	}

	switch pub := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		key, ok := priv.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("%w: certificate public key is RSA but private key is %T", ErrKeyMismatch, priv)
		}
		if !pub.Equal(&key.PublicKey) {
			return fmt.Errorf("%w: RSA modulus/exponent differ", ErrKeyMismatch)
		}
	case *ecdsa.PublicKey:
		key, ok := priv.(*ecdsa.PrivateKey)
		if !ok {
			return fmt.Errorf("%w: certificate public key is ECDSA but private key is %T", ErrKeyMismatch, priv)
		}
		if !pub.Equal(&key.PublicKey) {
			return fmt.Errorf("%w: ECDSA curve point differs", ErrKeyMismatch)
		}
	default:
		return fmt.Errorf("%w: unsupported public key algorithm %T", ErrParseFail, pub)
	}
	return nil
}

// parsePrivateKey 按 PEM block 类型解析私钥（PKCS#8 / PKCS#1 / SEC1 三种明文形态）。
// 错误信息为 x509 静态文案，不含密钥材料。
func parsePrivateKey(blockType string, der []byte) (any, error) {
	switch blockType {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(der)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(der)
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(der)
	default:
		return nil, fmt.Errorf("unsupported private key PEM block type %q", blockType)
	}
}
