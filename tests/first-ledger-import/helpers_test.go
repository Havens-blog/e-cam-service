// @feature cert-cloud-discovery-import @api-functional
//
// Shared world builders for the first-ledger-import journey tests.
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package first_ledger_import

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/require"
)

// leafNotAfter parses the leaf certificate's NotAfter out of a PEM chain.
func leafNotAfter(t *testing.T, pemBytes []byte) time.Time {
	t.Helper()
	block, _ := pem.Decode(pemBytes)
	require.NotNil(t, block, "leaf PEM block present")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert.NotAfter
}

// aliyunWorld builds a harness with a registered aliyun material adapter
// and one active aliyun account "acct-a" ( fixture: CloudAccount min_count 1,
// name aligned with the item accountKey ).
func aliyunWorld(t *testing.T) (*discoverytest.Harness, *discoverytest.StubCertAdapter) {
	t.Helper()
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{aliyun}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun: {discoverytest.ActiveAccount(domain.CloudAliyun, "acct-a")},
		}
	})
	return h, aliyun
}

// materialFor registers an existing in-cloud certificate ( cleaned chain
// PEM from the generated fixture bundle ).
func materialFor(b *certtest.CertBundle) service.DiscoveryCertMaterial {
	return service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
}

// garbagePEMMaterial returns in-cloud material whose PEM armor wraps
// non-DER bytes ( parse failure path ).
func garbagePEMMaterial() service.DiscoveryCertMaterial {
	return service.DiscoveryCertMaterial{
		Exists:       true,
		CertChainPEM: "-----BEGIN CERTIFICATE-----\nZ2FyYmFnZS1ub3QtYS1kZXI=\n-----END CERTIFICATE-----\n",
	}
}

// reasonCode extracts the leading "CODE: " prefix of an item error reason.
func reasonCode(reason string) string {
	for i := 0; i < len(reason); i++ {
		if reason[i] == ':' {
			return reason[:i]
		}
	}
	return reason
}
