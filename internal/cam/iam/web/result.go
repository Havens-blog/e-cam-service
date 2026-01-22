package web

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// Result 统一响应结构
type Result struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

// PageResult 分页响应结构
type PageResult struct {
	Code  int         `json:"code"`
	Msg   string      `json:"msg"`
	Data  interface{} `json:"data,omitempty"`
	Total int64       `json:"total"`
	Page  int64       `json:"page"`
	Size  int64       `json:"size"`
}

// Success 成功响应
func Success(data interface{}) *Result {
	return &Result{
		Code: errs.Success.Code,
		Msg:  errs.Success.Msg,
		Data: data,
	}
}

// SuccessWithMsg 成功响应（自定义消息）
func SuccessWithMsg(msg string, data interface{}) *Result {
	return &Result{
		Code: errs.Success.Code,
		Msg:  msg,
		Data: data,
	}
}

// Error 错误响应
func Error(err error) *Result {
	if errCode, ok := err.(errs.ErrorCode); ok {
		return &Result{
			Code: errCode.Code,
			Msg:  errCode.Msg,
		}
	}
	return &Result{
		Code: errs.SystemError.Code,
		Msg:  err.Error(),
	}
}

// ErrorWithCode 错误响应（指定错误码）
func ErrorWithCode(errCode errs.ErrorCode) *Result {
	return &Result{
		Code: errCode.Code,
		Msg:  errCode.Msg,
	}
}

// PageSuccess 分页成功响应
func PageSuccess(data interface{}, total, page, size int64) *PageResult {
	return &PageResult{
		Code:  errs.Success.Code,
		Msg:   errs.Success.Msg,
		Data:  data,
		Total: total,
		Page:  page,
		Size:  size,
	}
}
