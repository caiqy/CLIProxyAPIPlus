package responses

import (
	"context"
	"strings"
	"testing"
)

func TestConvertToResponses_StreamReasoningText(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var param any
	raw := []byte(`data: {"choices":[{"index":0,"delta":{"reasoning_text":"thinking step"}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`)

	results := ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, "gemini-3.1-pro-preview", nil, nil, raw, &param)
	combined := strings.Join(results, "\n")

	if !strings.Contains(combined, `"response.reasoning_summary_text.delta"`) && !strings.Contains(combined, `response.reasoning_summary_text.delta`) {
		t.Errorf("expected reasoning_summary_text.delta event, got:\n%s", combined)
	}
	if !strings.Contains(combined, `thinking step`) {
		t.Errorf("expected 'thinking step' in output, got:\n%s", combined)
	}
}

func TestConvertToResponses_StreamReasoningContent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var param any
	raw := []byte(`data: {"choices":[{"index":0,"delta":{"reasoning_content":"thinking step"}}],"created":1773047134,"id":"test-id","model":"o4-mini"}`)

	results := ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, "o4-mini", nil, nil, raw, &param)
	combined := strings.Join(results, "\n")

	if !strings.Contains(combined, `response.reasoning_summary_text.delta`) {
		t.Errorf("expected reasoning_summary_text.delta event, got:\n%s", combined)
	}
	if !strings.Contains(combined, `thinking step`) {
		t.Errorf("expected 'thinking step' in output, got:\n%s", combined)
	}
}

func TestConvertToResponses_ToolCallIndexCollision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var param any

	chunks := [][]byte{
		[]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`),
		[]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_002","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"b.txt\"}"}}]}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`),
		[]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_003","type":"function","function":{"name":"list_dir","arguments":"{\"path\":\".\"}"}}]}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`),
		[]byte(`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview","usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}`),
	}

	var allResults []string
	for _, chunk := range chunks {
		results := ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, "gemini-3.1-pro-preview", nil, nil, chunk, &param)
		allResults = append(allResults, results...)
	}

	combined := strings.Join(allResults, "\n")

	// Should contain 3 function_call items (one per tool call)
	funcCallCount := strings.Count(combined, `"type":"function_call"`)
	if funcCallCount < 3 {
		t.Errorf("expected at least 3 occurrences of \"type\":\"function_call\", got %d\n%s", funcCallCount, combined)
	}

	// Should contain all 3 call IDs
	for _, callID := range []string{"call_001", "call_002", "call_003"} {
		if !strings.Contains(combined, callID) {
			t.Errorf("expected %s in output, got:\n%s", callID, combined)
		}
	}

	// Should contain all 3 function names
	for _, name := range []string{"read_file", "write_file", "list_dir"} {
		if !strings.Contains(combined, name) {
			t.Errorf("expected %s in output, got:\n%s", name, combined)
		}
	}

	// Count SSE event lines (not the "type" field inside JSON data) for
	// response.function_call_arguments.done. Each SSE event produces a line
	// like: event: response.function_call_arguments.done
	argsDoneCount := strings.Count(combined, "event: response.function_call_arguments.done")
	if argsDoneCount != 3 {
		t.Errorf("expected 3 response.function_call_arguments.done events, got %d\n%s", argsDoneCount, combined)
	}

	// Should contain 3 response.output_item.done SSE events with function_call type
	outputItemDoneCount := strings.Count(combined, "event: response.output_item.done")
	if outputItemDoneCount < 3 {
		t.Errorf("expected at least 3 response.output_item.done events, got %d\n%s", outputItemDoneCount, combined)
	}
}

func TestConvertToResponsesNonStream_ReasoningText(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	raw := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","reasoning_text":"non-stream thinking","content":"the answer"},"finish_reason":"stop"}],"model":"gemini-3.1-pro-preview","id":"test-id","created":1773047134,"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)

	result := ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(ctx, "gemini-3.1-pro-preview", nil, nil, raw, nil)

	if !strings.Contains(result, `"type":"reasoning"`) {
		t.Errorf("expected '\"type\":\"reasoning\"' in output, got:\n%s", result)
	}
	if !strings.Contains(result, `summary_text`) {
		t.Errorf("expected 'summary_text' in output, got:\n%s", result)
	}
	if !strings.Contains(result, `non-stream thinking`) {
		t.Errorf("expected 'non-stream thinking' in output, got:\n%s", result)
	}
	if !strings.Contains(result, `"type":"message"`) {
		t.Errorf("expected '\"type\":\"message\"' in output, got:\n%s", result)
	}
	if !strings.Contains(result, `the answer`) {
		t.Errorf("expected 'the answer' in output, got:\n%s", result)
	}
}

func TestConvertToResponses_StreamMultipleToolCallsInSingleChunk(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var param any

	chunk := []byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"tool_a","arguments":"{}"}},{"index":1,"id":"call_002","type":"function","function":{"name":"tool_b","arguments":"{}"}}]}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`)
	finish := []byte(`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`)

	out1 := ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, "gemini-3.1-pro-preview", nil, nil, chunk, &param)
	out2 := ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, "gemini-3.1-pro-preview", nil, nil, finish, &param)
	combined := strings.Join(append(out1, out2...), "\n")

	// Should include both tool calls (IDs + names)
	for _, s := range []string{"call_001", "call_002", "tool_a", "tool_b"} {
		if !strings.Contains(combined, s) {
			t.Fatalf("expected %q in output, got:\n%s", s, combined)
		}
	}

	addedCount := strings.Count(combined, "event: response.output_item.added")
	if addedCount < 2 {
		t.Fatalf("expected at least 2 output_item.added events for tool calls, got %d\n%s", addedCount, combined)
	}
}
