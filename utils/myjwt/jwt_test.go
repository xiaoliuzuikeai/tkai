// [第一阶段优化-测试] 覆盖 JWT 正常解析和错误签发者拒绝逻辑。
package myjwt

import (
	"GoAI/config"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func TestGenerateAndParseToken(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDir) })

	t.Setenv("MYSQL_HOST", "127.0.0.1")
	t.Setenv("MYSQL_USER", "test")
	t.Setenv("MYSQL_DATABASE", "test")
	t.Setenv("JWT_SECRET", "test-secret-that-is-at-least-32-characters-long")
	t.Setenv("JWT_EXPIRE_HOURS", "24")
	t.Setenv("JWT_ISSUER", "GoAI-test")
	t.Setenv("JWT_SUBJECT", "access-token-test")
	t.Setenv("DASHSCOP_API_KEY", "test-key")
	t.Setenv("DASHSCOP_BASE_URL", "https://example.invalid/v1")
	t.Setenv("OPENAI_MODEL_NAME", "test-model")
	if err := config.InitConfig(); err != nil {
		t.Fatalf("InitConfig() error = %v", err)
	}

	token, err := GenerateToken(1, "alice")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if username, ok := ParseToken(token); !ok || username != "alice" {
		t.Fatalf("ParseToken() = (%q, %v), want (alice, true)", username, ok)
	}

	conf := config.GetConfig()
	wrongClaims := Claims{
		Username: "alice",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "wrong-issuer",
			Subject:   conf.Subject,
		},
	}
	wrongToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, wrongClaims).SignedString([]byte(conf.Key))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ParseToken(wrongToken); ok {
		t.Fatal("ParseToken() accepted a token with the wrong issuer")
	}
}
