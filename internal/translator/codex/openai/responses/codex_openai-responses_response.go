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

func ConvertCodexResponseToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []string {
	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
		if typeResult := gjson.GetBytes(rawJSON, "type"); typeResult.Exists() {
			typeStr := typeResult.String()
			if typeStr == "response.created" || typeStr == "response.in_progress" || typeStr == "response.completed" {
				if gjson.GetBytes(rawJSON, "response.instructions").Exists() {
					instructions := gjson.GetBytes(originalRequestRawJSON, "instructions").String()
					rawJSON, _ = sjson.SetBytes(rawJSON, "response.instructions", instructions)
				}
			}
		}
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
func ConvertCodexResponseToOpenAIResponsesNonStream(_ context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) string {
	rootResult := gjson.ParseBytes(rawJSON)
	// Verify this is a response.completed event
	if rootResult.Get("type").String() != "response.completed" {
		return ""
	}
	responseResult := rootResult.Get("response")
	template := responseResult.Raw
	if responseResult.Get("instructions").Exists() {
		instructions := gjson.GetBytes(originalRequestRawJSON, "instructions").String()
		template, _ = sjson.Set(template, "instructions", instructions)
	}
	return template
}
