package jwt

import (
	"context"

	"github.com/golang-jwt/jwt/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ClientInterceptorBuilder 客户端拦截器构建器
type ClientInterceptorBuilder struct {
	jwtKey string
}

// NewClientInterceptorBuilder 创建客户端拦截器构建器
func NewClientInterceptorBuilder(jwtKey string) *ClientInterceptorBuilder {
	return &ClientInterceptorBuilder{
		jwtKey: jwtKey,
	}
}

// UnaryClientInterceptor 创建一元客户端拦截器
func (b *ClientInterceptorBuilder) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if b.hasJWTInContext(ctx) {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		jwtCtx := b.injectJWTContext(ctx)
		return invoker(jwtCtx, method, req, reply, cc, opts...)
	}
}

func (b *ClientInterceptorBuilder) hasJWTInContext(ctx context.Context) bool {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return false
	}
	authHeaders := md.Get("Authorization")
	return len(authHeaders) > 0
}

func (b *ClientInterceptorBuilder) injectJWTContext(ctx context.Context) context.Context {
	jwtAuth := NewJwtAuth(b.jwtKey)

	claims := jwt.MapClaims{
		"biz_id": float64(1),
	}

	tokenString, err := jwtAuth.Encode(claims)
	if err != nil {
		return ctx
	}

	md := metadata.New(map[string]string{
		"Authorization": "Bearer " + tokenString,
	})
	return metadata.NewOutgoingContext(ctx, md)
}
