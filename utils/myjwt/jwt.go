package myjwt

import (
	"GoAI/config"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func GenerateToken(id int64, username string) (string, error) {
	claims := Claims{
		ID:       id,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(config.GetConfig().ExpireDuration) * time.Hour)), //过期时间
			Issuer:    config.GetConfig().Issuer,                                                                        //签发者
			Subject:   config.GetConfig().Subject,                                                                       //主题
			IssuedAt:  jwt.NewNumericDate(time.Now()),                                                                   //签发时间
		},
	}

	// 生成 token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.GetConfig().Key))
}

// [第一阶段优化-认证] 严格校验 HS256、签发者、用途、有效期和用户名。
func ParseToken(token string) (string, bool) {
	claims := new(Claims)
	conf := config.GetConfig()
	t, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", t.Method.Alg())
		}
		return []byte(conf.Key), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || t == nil || !t.Valid || claims.Issuer != conf.Issuer || claims.Subject != conf.Subject || claims.Username == "" {
		return "", false
	}
	return claims.Username, true
}
