package claude

import (
	"context"
	"strings"
	"testing"
)

func TestConvertOpenAIResponseToClaude_StreamReasoningTextToThinkingDelta(t *testing.T) {
	t.Parallel()

	var param any
	originalReq := []byte(`{"stream":true}`)
	chunk := []byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_text":"推理内容"}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`)

	out := ConvertOpenAIResponseToClaude(context.Background(), "gemini-3.1-pro-preview", originalReq, nil, chunk, &param)
	joined := strings.Join(out, "")

	if !strings.Contains(joined, `"type":"thinking_delta"`) {
		t.Fatalf("expected thinking_delta event from reasoning_text, got: %s", joined)
	}
	if !strings.Contains(joined, `"thinking":"推理内容"`) {
		t.Fatalf("expected thinking text to be preserved, got: %s", joined)
	}
}
