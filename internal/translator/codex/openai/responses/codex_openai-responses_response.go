package responses

import (
	"bytes"
	"context"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertCodexResponseToOpenAIResponses converts OpenAI Chat Completions streaming chunks
// to OpenAI Responses SSE events (response.*).

func ConvertCodexResponseToOpenAIResponses(_ context.Context, _ string, _, _, rawJSON []byte, _ *any) []string {
	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
		out := fmt.Sprintf("data: %s", string(rawJSON))
		return []string{out}
	}

	// 检查是否是错误 JSON，如果是则转换为 OpenAI Responses API 格式
	// 格式: {"type":"error","error":{...},"sequence_number":0}
	if errorResult := gjson.GetBytes(rawJSON, "error"); errorResult.Exists() {
		errorEvent := `{"type":"error","sequence_number":0}`
		errorEvent, _ = sjson.SetRaw(errorEvent, "error", errorResult.Raw)
		return []string{fmt.Sprintf("data: %s", errorEvent)}
	}

	return []string{string(rawJSON)}
}

// ConvertCodexResponseToOpenAIResponsesNonStream builds a single Responses JSON
// from a non-streaming OpenAI Chat Completions response.
func ConvertCodexResponseToOpenAIResponsesNonStream(_ context.Context, _ string, _, _, rawJSON []byte, _ *any) string {
	rootResult := gjson.ParseBytes(rawJSON)
	// Verify this is a response.completed event
	if rootResult.Get("type").String() != "response.completed" {
		return ""
	}
	responseResult := rootResult.Get("response")
	return responseResult.Raw
}
