package controller

import (
	"GoAI/common/code"
	"net/http"
)

type Response struct {
	StatusCode code.Code `json:"status_code"`
	StatusMsg  string    `json:"status_msg,omitempty"`
}

func (r *Response) CodeOf(code code.Code) Response {
	if nil == r {
		r = new(Response)
	}
	r.StatusCode = code
	r.StatusMsg = code.Msg()
	return *r
}

func (r *Response) Success() {
	r.CodeOf(code.CodeSuccess)
}

// [第一阶段优化-HTTP] 将业务码映射为真实 HTTP 状态码。
func HTTPStatus(appCode code.Code) int {
	switch appCode {
	case code.CodeSuccess:
		return http.StatusOK
	case code.CodeInvalidParams, code.CodeIllegalPassword:
		return http.StatusBadRequest
	case code.CodeInvalidToken, code.CodeNotLogin, code.CodeInvalidPassword:
		return http.StatusUnauthorized
	case code.CodeInvalidCaptcha:
		return http.StatusBadRequest
	case code.CodeForbidden:
		return http.StatusForbidden
	case code.CodeUserExist:
		return http.StatusConflict
	case code.CodeUserNotExist, code.CodeRecordNotFound:
		return http.StatusNotFound
	case code.CodeTooManyRequests:
		return http.StatusTooManyRequests
	case code.AIModelNotFind, code.AIModelCannotOpen, code.AIModelFail, code.TTSFail:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
