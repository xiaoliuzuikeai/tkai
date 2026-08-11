// [第一阶段优化-测试] 覆盖限流后的 429 和 Retry-After 响应。
package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := New(1, time.Minute)
	router := gin.New()
	router.GET("/", limiter.Handle(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}

	if got := request().Code; got != http.StatusNoContent {
		t.Fatalf("first request status = %d, want %d", got, http.StatusNoContent)
	}
	second := request()
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("limited response is missing Retry-After")
	}
}
