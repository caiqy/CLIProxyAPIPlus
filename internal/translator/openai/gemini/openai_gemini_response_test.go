package gemini

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponseToGemini_StreamReasoningText(t *testing.T) {
	t.Parallel()

	chunk := []byte(`data: {"choices":[{"index":0,"delta":{"reasoning_text":"thinking step"}}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`)

	var param any
	results := ConvertOpenAIResponseToGemini(context.Background(), "", nil, nil, chunk, &param)

	if len(results) == 0 {
		t.Fatal("expected at least one result, got none")
	}

	combined := strings.Join(results, "\n")
	if !strings.Contains(combined, `"thought":true`) {
		t.Errorf("expected output to contain \"thought\":true, got: %s", combined)
	}
	if !strings.Contains(combined, `"text":"thinking step"`) {
		t.Errorf("expected output to contain \"text\":\"thinking step\", got: %s", combined)
	}
}

func TestConvertOpenAIResponseToGemini_StreamReasoningContent(t *testing.T) {
	t.Parallel()

	chunk := []byte(`data: {"choices":[{"index":0,"delta":{"reasoning_content":"thinking step"}}],"created":1773047134,"id":"test-id","model":"o4-mini"}`)

	var param any
	results := ConvertOpenAIResponseToGemini(context.Background(), "", nil, nil, chunk, &param)

	if len(results) == 0 {
		t.Fatal("expected at least one result, got none")
	}

	combined := strings.Join(results, "\n")
	if !strings.Contains(combined, `"thought":true`) {
		t.Errorf("expected output to contain \"thought\":true, got: %s", combined)
	}
	if !strings.Contains(combined, `"text":"thinking step"`) {
		t.Errorf("expected output to contain \"text\":\"thinking step\", got: %s", combined)
	}
}

func TestConvertOpenAIResponseToGemini_ToolCallIndexCollision(t *testing.T) {
	t.Parallel()

	chunks := [][]byte{
		[]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]}}],"id":"test-id","model":"gemini-3.1-pro-preview"}`),
		[]byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_002","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"b.txt\",\"content\":\"hello\"}"}}]}}],"id":"test-id","model":"gemini-3.1-pro-preview"}`),
		// Copilot Gemini often sends the final tool_call AND finish_reason in the same chunk.
		[]byte(`data: {"choices":[{"finish_reason":"tool_calls","index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_003","type":"function","function":{"name":"list_dir","arguments":"{\"path\":\".\"}"}}]}}],"id":"test-id","model":"gemini-3.1-pro-preview"}`),
	}

	var param any
	var allResults []string
	for _, chunk := range chunks {
		results := ConvertOpenAIResponseToGemini(context.Background(), "", nil, nil, chunk, &param)
		allResults = append(allResults, results...)
	}

	if len(allResults) == 0 {
		t.Fatal("expected at least one result, got none")
	}

	// The final result (from the finish_reason chunk) should contain all 3 functionCall entries
	finalResult := allResults[len(allResults)-1]
	parsed := gjson.Parse(finalResult)

	parts := parsed.Get("candidates.0.content.parts")
	if !parts.Exists() || !parts.IsArray() {
		t.Fatalf("expected candidates.0.content.parts to be an array, got: %s", finalResult)
	}

	functionCalls := parts.Array()
	if len(functionCalls) != 3 {
		t.Fatalf("expected 3 functionCall parts, got %d: %s", len(functionCalls), finalResult)
	}

	expectedNames := []string{"read_file", "write_file", "list_dir"}
	for i, fc := range functionCalls {
		name := fc.Get("functionCall.name").String()
		if name != expectedNames[i] {
			t.Errorf("functionCall[%d]: expected name %q, got %q", i, expectedNames[i], name)
		}
	}

	// Also verify via counting functionCall occurrences in the combined output
	combined := strings.Join(allResults, "\n")
	count := strings.Count(combined, `"functionCall"`)
	if count != 3 {
		t.Errorf("expected 3 functionCall occurrences in combined output, got %d", count)
	}
}

func TestConvertOpenAIResponseToGeminiNonStream_ReasoningText(t *testing.T) {
	t.Parallel()

	rawJSON := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","reasoning_text":"non-stream thinking","content":"the answer"},"finish_reason":"stop"}],"model":"gemini-3.1-pro-preview","usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)

	result := ConvertOpenAIResponseToGeminiNonStream(context.Background(), "", nil, nil, rawJSON, nil)

	if !strings.Contains(result, `"thought":true`) {
		t.Errorf("expected output to contain \"thought\":true, got: %s", result)
	}
	if !strings.Contains(result, `"text":"non-stream thinking"`) {
		t.Errorf("expected output to contain \"text\":\"non-stream thinking\", got: %s", result)
	}
	if !strings.Contains(result, `"text":"the answer"`) {
		t.Errorf("expected output to contain \"text\":\"the answer\", got: %s", result)
	}
}

func TestConvertOpenAIResponseToGemini_StreamFinishReasonWithReasoningSameChunk(t *testing.T) {
	t.Parallel()

	// Some providers may include finish_reason in the same chunk as the last delta.
	chunk := []byte(`data: {"choices":[{"index":0,"delta":{"reasoning_text":"last thought"},"finish_reason":"stop"}],"created":1773047134,"id":"test-id","model":"gemini-3.1-pro-preview"}`)

	var param any
	results := ConvertOpenAIResponseToGemini(context.Background(), "", nil, nil, chunk, &param)
	combined := strings.Join(results, "\n")

	if !strings.Contains(combined, `"thought":true`) || !strings.Contains(combined, `"text":"last thought"`) {
		t.Fatalf("expected reasoning thought delta in output, got: %s", combined)
	}
	if !strings.Contains(combined, `"finishReason"`) {
		t.Fatalf("expected finishReason to be emitted even when in same chunk, got: %s", combined)
	}
}

func TestConvertOpenAIResponseToGemini_StreamUsageCarriedToFinishChunk(t *testing.T) {
	t.Parallel()

	var param any
	// First chunk has content + usage.
	chunk1 := []byte(`data: {"choices":[{"index":0,"delta":{"content":"hi"}}],"id":"test-id","model":"gemini-3.1-pro-preview","usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
	// Finish chunk has no usage.
	chunk2 := []byte(`data: {"choices":[{"index":0,"finish_reason":"stop"}],"id":"test-id","model":"gemini-3.1-pro-preview"}`)

	res1 := ConvertOpenAIResponseToGemini(context.Background(), "", nil, nil, chunk1, &param)
	res2 := ConvertOpenAIResponseToGemini(context.Background(), "", nil, nil, chunk2, &param)

	if len(res1) != 1 {
		t.Fatalf("expected first chunk to emit exactly 1 output (content delta), got %d: %v", len(res1), res1)
	}
	final := strings.Join(res2, "\n")
	if !strings.Contains(final, `"finishReason"`) {
		t.Fatalf("expected finishReason in final output, got: %s", final)
	}
	if !strings.Contains(final, `"usageMetadata"`) {
		t.Fatalf("expected usageMetadata to be carried into finish chunk, got: %s", final)
	}
	if !strings.Contains(final, `"promptTokenCount":10`) || !strings.Contains(final, `"candidatesTokenCount":20`) || !strings.Contains(final, `"totalTokenCount":30`) {
		t.Fatalf("expected carried token counts in final output, got: %s", final)
	}
}

func TestConvertOpenAIResponseToGemini_StreamIgnoreNullFinishReason(t *testing.T) {
	t.Parallel()

	// OpenAI-style streaming often includes finish_reason: null on intermediate chunks.
	chunk := []byte(`data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}],"id":"test-id","model":"gpt-4.1"}`)

	var param any
	results := ConvertOpenAIResponseToGemini(context.Background(), "", nil, nil, chunk, &param)
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 output for intermediate chunk, got %d: %v", len(results), results)
	}
	if strings.Contains(results[0], `"finishReason"`) {
		t.Fatalf("did not expect finishReason for null finish_reason, got: %s", results[0])
	}
	if !strings.Contains(results[0], `"text":"hi"`) {
		t.Fatalf("expected content delta to be emitted, got: %s", results[0])
	}
}

func TestCollectReasoningTextsFromFields(t *testing.T) {
	t.Parallel()

	t.Run("both fields present", func(t *testing.T) {
		t.Parallel()
		parent := gjson.Parse(`{"reasoning_content":"text1","reasoning_text":"text2","other":"ignored"}`)
		texts := collectReasoningTextsFromFields(parent, "reasoning_content", "reasoning_text")
		if len(texts) != 2 {
			t.Fatalf("expected 2 texts, got %d: %v", len(texts), texts)
		}
		if texts[0] != "text1" {
			t.Errorf("expected texts[0] = %q, got %q", "text1", texts[0])
		}
		if texts[1] != "text2" {
			t.Errorf("expected texts[1] = %q, got %q", "text2", texts[1])
		}
	})

	t.Run("only reasoning_text present", func(t *testing.T) {
		t.Parallel()
		parent := gjson.Parse(`{"reasoning_text":"text2"}`)
		texts := collectReasoningTextsFromFields(parent, "reasoning_content", "reasoning_text")
		if len(texts) != 1 {
			t.Fatalf("expected 1 text, got %d: %v", len(texts), texts)
		}
		if texts[0] != "text2" {
			t.Errorf("expected texts[0] = %q, got %q", "text2", texts[0])
		}
	})

	t.Run("neither field present", func(t *testing.T) {
		t.Parallel()
		parent := gjson.Parse(`{"other":"ignored"}`)
		texts := collectReasoningTextsFromFields(parent, "reasoning_content", "reasoning_text")
		if len(texts) != 0 {
			t.Fatalf("expected 0 texts, got %d: %v", len(texts), texts)
		}
	})
}
