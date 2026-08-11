// [第二阶段优化-测试] 覆盖历史分页边界和 Unicode 会话标题截断。
package session

import (
	"strings"
	"testing"
)

func TestNormalizePagination(t *testing.T) {
	if page, size := normalizePagination(0, 0); page != 1 || size != 50 {
		t.Fatalf("normalizePagination(0, 0) = (%d, %d), want (1, 50)", page, size)
	}
	if page, size := normalizePagination(2, 1000); page != 2 || size != 100 {
		t.Fatalf("normalizePagination(2, 1000) = (%d, %d), want (2, 100)", page, size)
	}
}

func TestSessionTitle(t *testing.T) {
	question := strings.Repeat("你", 120)
	title := sessionTitle(question)
	if got := len([]rune(title)); got != 100 {
		t.Fatalf("sessionTitle() rune length = %d, want 100", got)
	}
}
