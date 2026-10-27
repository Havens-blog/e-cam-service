package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// s2sClaims 复刻 eiam client_credentials 签发 token 的 claim 形状（tenant_id + 标准声明）。
type s2sClaims struct {
	TenantID int64 `json:"tenant_id"`
	jwt.RegisteredClaims
}

func genS2SKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return key, string(pemBytes)
}

func signS2SToken(t *testing.T, key *rsa.PrivateKey, tenantID int64, exp time.Time) string {
	t.Helper()
	claims := s2sClaims{
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "opsagent",
			Issuer:    "https://iam.example.com",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := token.SignedString(key)
	require.NoError(t, err)
	return s
}

func TestS2SAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key, pubPEM := genS2SKey(t)

	tests := []struct {
		name        string
		publicKey   string
		authHeader  string
		wantStatus  int
		wantTenant  int64
	}{
		{
			name:       "公钥未配置拒绝",
			publicKey:  "",
			authHeader: "Bearer x",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "缺少 Authorization 拒绝",
			publicKey:  pubPEM,
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "无效令牌拒绝",
			publicKey:  pubPEM,
			authHeader: "Bearer not-a-jwt",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "过期令牌拒绝",
			publicKey:  pubPEM,
			authHeader: "Bearer " + signS2SToken(t, key, 3, time.Now().Add(-time.Hour)),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "缺租户(tenant_id=0)拒绝",
			publicKey:  pubPEM,
			authHeader: "Bearer " + signS2SToken(t, key, 0, time.Now().Add(time.Hour)),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "有效令牌放行并注入租户",
			publicKey:  pubPEM,
			authHeader: "Bearer " + signS2SToken(t, key, 3, time.Now().Add(time.Hour)),
			wantStatus: http.StatusOK,
			wantTenant: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			var gotTenant int64
			router.GET("/probe", S2SAuth(tt.publicKey), func(c *gin.Context) {
				gotTenant = GetTenantID(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/probe", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code, "status (body=%s)", w.Body.String())
			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, tt.wantTenant, gotTenant)
			}
		})
	}
}
