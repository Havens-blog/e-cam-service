package endpoint

import (
	"github.com/Havens-blog/e-cam-service/internal/endpoint/domain"
	"github.com/Havens-blog/e-cam-service/internal/endpoint/service"
	"github.com/Havens-blog/e-cam-service/internal/endpoint/web"
)

type Handler = web.Handler

type Service = service.Service

type Endpoint = domain.Endpoint
