// [第二阶段优化-测试] 覆盖倒序数据库结果恢复为时间正序。
package message

import (
	"testing"

	"GopherAI/model"
)

func TestReverseMessages(t *testing.T) {
	messages := []model.Message{{ID: 3}, {ID: 2}, {ID: 1}}
	reverseMessages(messages)
	for index, want := range []uint{1, 2, 3} {
		if messages[index].ID != want {
			t.Fatalf("messages[%d].ID = %d, want %d", index, messages[index].ID, want)
		}
	}
}
