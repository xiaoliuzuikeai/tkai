package jwt

import (
	"GopherAI/common/code"
	"GopherAI/controller"
	"GopherAI/utils/myjwt"
	"strings"

	"github.com/gin-gonic/gin"
)

// 读取jwt
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		res := new(controller.Response)

		// [第一阶段优化-认证] 只接受 Authorization Bearer，不再从 URL 读取或记录 Token。
		authHeader := c.GetHeader("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") || token == "" {
			c.JSON(controller.HTTPStatus(code.CodeInvalidToken), res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		userName, ok := myjwt.ParseToken(token)
		if !ok {
			c.JSON(controller.HTTPStatus(code.CodeInvalidToken), res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		c.Set("userName", userName)
		c.Next()
	}
}
