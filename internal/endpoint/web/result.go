package web

import (
	"github.com/Havens-blog/e-cam-service/internal/endpoint/errs"
	"github.com/Havens-blog/e-common-go/ginx"
)

var (
	systemErrorResult = ginx.Result{
		Code: errs.SystemError.Code,
		Msg:  errs.SystemError.Msg,
	}
)
