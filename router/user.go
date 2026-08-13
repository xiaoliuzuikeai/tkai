package router

import (
	"GoAI/controller/user"
	"GoAI/middleware/ratelimit"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterUserRouter(r *gin.RouterGroup) {
	// [第一阶段优化-限流] 不同认证操作使用独立频率窗口。
	loginLimiter := ratelimit.New(10, time.Minute)
	registerLimiter := ratelimit.New(5, 10*time.Minute)
	captchaLimiter := ratelimit.New(3, 10*time.Minute)
	{
		r.POST("/register", registerLimiter.Handle(), user.Register)
		r.POST("/login", loginLimiter.Handle(), user.Login)
		r.POST("/captcha", captchaLimiter.Handle(), user.HandleCaptcha)
	}
}
