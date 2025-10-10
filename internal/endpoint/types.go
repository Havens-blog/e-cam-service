package endpoint

import (
	"github.com/Havens-blog/e-cam-service/internal/endpoint/internal/domain"
	"github.com/Havens-blog/e-cam-service/internal/endpoint/internal/service"
	"github.com/Havens-blog/e-cam-service/internal/endpoint/internal/web"
)

type Handler = web.Handler

type Service = service.Service

type Endpoint = domain.Endpoint
