// [第一阶段优化-限流] 为登录、注册和验证码接口提供进程内 IP 限流。
package ratelimit

import (
	"GoAI/common/code"
	"GoAI/controller"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type entry struct {
	count   int
	resetAt time.Time
}

type Limiter struct {
	mu      sync.Mutex
	entries map[string]entry
	max     int
	window  time.Duration
}

func New(max int, window time.Duration) *Limiter {
	return &Limiter{entries: make(map[string]entry), max: max, window: window}
}

func (l *Limiter) Handle() gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now()
		key := c.ClientIP()

		l.mu.Lock()
		// [第一阶段优化-限流] 定期清理过期访客，限制内存增长。
		if len(l.entries) > 10000 {
			for ip, item := range l.entries {
				if now.After(item.resetAt) {
					delete(l.entries, ip)
				}
			}
		}
		current := l.entries[key]
		if current.resetAt.IsZero() || now.After(current.resetAt) {
			current = entry{resetAt: now.Add(l.window)}
		}
		current.count++
		l.entries[key] = current
		limited := current.count > l.max
		retryAfter := int(time.Until(current.resetAt).Seconds()) + 1
		l.mu.Unlock()

		if limited {
			res := new(controller.Response)
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.JSON(controller.HTTPStatus(code.CodeTooManyRequests), res.CodeOf(code.CodeTooManyRequests))
			c.Abort()
			return
		}
		c.Next()
	}
}
