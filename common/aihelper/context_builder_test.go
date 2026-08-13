package aihelper

import (
	"GoAI/model"
	"fmt"
	"strings"
	"testing"
)

func TestContextBuilderRespectsTokenBudgetAndRecentTurns(t *testing.T) {
	messages := make([]*model.Message, 0, 20)
	for i := 1; i <= 20; i++ {
		messages = append(messages, &model.Message{
			ID: uint(i), IsUser: i%2 == 1,
			Content: fmt.Sprintf("第%d条消息 %s", i, strings.Repeat("上下文", 30)),
			Status:  model.MessageStatusCompleted,
		})
	}
	builder := ContextBuilder{MaxContextTokens: 500, MaxOutputTokens: 100, RecentTurns: 2}
	result := builder.Build(messages, "", 0)
	if result.EstimatedTokens > 400 {
		t.Fatalf("estimated tokens = %d, want <= 400", result.EstimatedTokens)
	}
	if len(result.Messages) < 4 {
		t.Fatalf("messages = %d, latest two turns were not retained", len(result.Messages))
	}
	if result.Summary == "" || result.SummaryUpToMessageID == 0 {
		t.Fatal("older messages were not summarized")
	}
	if !strings.Contains(result.Summary, "## Facts") {
		t.Fatalf("summary is not structured: %q", result.Summary)
	}
}

func TestEstimateTokensHandlesChineseAndASCII(t *testing.T) {
	if EstimateTokens("hello world") <= 0 || EstimateTokens("你好世界") < 4 {
		t.Fatal("token estimator returned an invalid value")
	}
}
