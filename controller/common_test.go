// [第一阶段优化-测试] 覆盖业务码到 HTTP 状态码的映射。
package controller

import (
	"GoAI/common/code"
	"net/http"
	"testing"
)

func TestHTTPStatus(t *testing.T) {
	tests := map[code.Code]int{
		code.CodeSuccess:         http.StatusOK,
		code.CodeInvalidParams:   http.StatusBadRequest,
		code.CodeInvalidToken:    http.StatusUnauthorized,
		code.CodeForbidden:       http.StatusForbidden,
		code.CodeRecordNotFound:  http.StatusNotFound,
		code.CodeUserExist:       http.StatusConflict,
		code.CodeTooManyRequests: http.StatusTooManyRequests,
		code.AIModelFail:         http.StatusBadGateway,
		code.CodeServerBusy:      http.StatusInternalServerError,
	}
	for appCode, want := range tests {
		if got := HTTPStatus(appCode); got != want {
			t.Errorf("HTTPStatus(%d) = %d, want %d", appCode, got, want)
		}
	}
}
