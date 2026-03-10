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

func TestConvertOpenAIResponseToClaude_ToolCallIndexCollision(t *testing.T) {
	t.Parallel()

	var param any
	originalReq := []byte(`{"stream":true}`)

	chunks := [][]byte{
		[]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`),
		[]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_002","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"b.txt\",\"content\":\"hello\"}"}}]}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`),
		[]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_003","type":"function","function":{"name":"list_dir","arguments":"{\"path\":\".\"}"}}]}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`),
		[]byte(`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview","usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}`),
		[]byte(`data: [DONE]`),
	}

	var allOut []string
	for _, chunk := range chunks {
		out := ConvertOpenAIResponseToClaude(context.Background(), "gemini-3.1-pro-preview", originalReq, nil, chunk, &param)
		allOut = append(allOut, out...)
	}
	joined := strings.Join(allOut, "")

	// Should contain exactly 3 tool_use content_block_start events
	toolUseCount := strings.Count(joined, `"type":"tool_use"`)
	if toolUseCount != 3 {
		t.Fatalf("expected exactly 3 tool_use content blocks, got %d\noutput: %s", toolUseCount, joined)
	}

	// Should contain all three tool names
	for _, name := range []string{`"name":"read_file"`, `"name":"write_file"`, `"name":"list_dir"`} {
		if !strings.Contains(joined, name) {
			t.Fatalf("expected %s in output, got: %s", name, joined)
		}
	}

	// Should contain all three tool call IDs
	for _, id := range []string{`"id":"call_001"`, `"id":"call_002"`, `"id":"call_003"`} {
		if !strings.Contains(joined, id) {
			t.Fatalf("expected %s in output, got: %s", id, joined)
		}
	}

	// Should have content_block_stop for each tool use
	stopCount := strings.Count(joined, `"type":"content_block_stop"`)
	if stopCount < 3 {
		t.Fatalf("expected at least 3 content_block_stop events, got %d\noutput: %s", stopCount, joined)
	}
}

func TestConvertOpenAIResponseToClaude_NonStream_ReasoningText(t *testing.T) {
	t.Parallel()

	var param any
	originalReq := []byte(`{"stream":false}`)
	chunk := []byte(`data: {"choices":[{"index":0,"message":{"role":"assistant","reasoning_text":"non-stream thinking","content":"the answer"},"finish_reason":"stop"}],"model":"gemini-3.1-pro-preview","usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)

	out := ConvertOpenAIResponseToClaude(context.Background(), "gemini-3.1-pro-preview", originalReq, nil, chunk, &param)
	joined := strings.Join(out, "")

	if !strings.Contains(joined, `"type":"thinking"`) {
		t.Fatalf("expected thinking block in non-stream output, got: %s", joined)
	}
	if !strings.Contains(joined, `"thinking":"non-stream thinking"`) {
		t.Fatalf("expected thinking text in non-stream output, got: %s", joined)
	}
	if !strings.Contains(joined, `"type":"text"`) {
		t.Fatalf("expected text block in non-stream output, got: %s", joined)
	}
	if !strings.Contains(joined, `"text":"the answer"`) {
		t.Fatalf("expected text content in non-stream output, got: %s", joined)
	}
}
