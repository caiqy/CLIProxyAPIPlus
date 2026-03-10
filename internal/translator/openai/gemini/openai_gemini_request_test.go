package gemini

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiRequestToOpenAI_ToolCallIDsPaired(t *testing.T) {
	t.Parallel()

	// Repro for Copilot upstream 400 invalid_tool_call_format:
	// multiple tool calls in assistant turn, but tool responses all incorrectly
	// reference the same tool_call_id.
	input := []byte(`{
		"model":"gemini-3.1-pro-preview",
		"contents":[
			{"role":"user","parts":[{"text":"你好"}]},
			{"role":"model","parts":[
				{"functionCall":{"name":"bash","args":{"command":"ls -la"}}},
				{"functionCall":{"name":"bash","args":{"command":"cat package.json"}}},
				{"functionCall":{"name":"bash","args":{"command":"cat README.md"}}}
			]},
			{"role":"user","parts":[
				{"functionResponse":{"name":"bash","response":{"content":"total 114"}}},
				{"functionResponse":{"name":"bash","response":{"content":"cat: package.json: No such file or directory"}}},
				{"functionResponse":{"name":"bash","response":{"content":"# CLIProxyAPI Business Edition"}}}
			]}
		]
	}`)

	out := ConvertGeminiRequestToOpenAI("gemini-3.1-pro-preview", input, true)
	parsed := gjson.ParseBytes(out)

	msgs := parsed.Get("messages")
	if !msgs.Exists() || !msgs.IsArray() {
		t.Fatalf("expected messages to be an array, got: %s", string(out))
	}

	var callIDs []string
	var toolIDs []string

	for _, m := range msgs.Array() {
		role := m.Get("role").String()
		switch role {
		case "assistant":
			tcs := m.Get("tool_calls")
			if tcs.Exists() && tcs.IsArray() {
				for _, tc := range tcs.Array() {
					id := tc.Get("id").String()
					if id != "" {
						callIDs = append(callIDs, id)
					}
				}
			}
		case "tool":
			id := m.Get("tool_call_id").String()
			if id != "" {
				toolIDs = append(toolIDs, id)
			}
		}
	}

	if len(callIDs) != 3 {
		t.Fatalf("expected 3 tool call ids in assistant message, got %d: %v", len(callIDs), callIDs)
	}
	if len(toolIDs) != 3 {
		t.Fatalf("expected 3 tool response ids, got %d: %v", len(toolIDs), toolIDs)
	}

	// Each tool response must reference a distinct tool_call_id from the tool calls.
	callSet := map[string]bool{}
	for _, id := range callIDs {
		callSet[id] = true
	}

	seen := map[string]bool{}
	for _, id := range toolIDs {
		if !callSet[id] {
			t.Fatalf("tool_call_id %q not found in tool_calls ids %v", id, callIDs)
		}
		if seen[id] {
			t.Fatalf("tool_call_id %q is duplicated in tool responses: %v", id, toolIDs)
		}
		seen[id] = true
	}
}

func TestConvertGeminiRequestToOpenAI_ToolContentNotDoubleQuoted(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"model":"gemini-3.1-pro-preview",
		"contents":[
			{"role":"model","parts":[
				{"functionCall":{"name":"bash","args":{"command":"ls -la"}}}
			]},
			{"role":"user","parts":[
				{"functionResponse":{"name":"bash","response":{"content":"total 114"}}}
			]}
		]
	}`)

	out := ConvertGeminiRequestToOpenAI("gemini-3.1-pro-preview", input, true)
	parsed := gjson.ParseBytes(out)

	msgs := parsed.Get("messages")
	if !msgs.Exists() || !msgs.IsArray() {
		t.Fatalf("expected messages to be an array, got: %s", string(out))
	}

	foundTool := false
	for _, m := range msgs.Array() {
		if m.Get("role").String() != "tool" {
			continue
		}
		foundTool = true
		content := m.Get("content").String()
		if content != "total 114" {
			t.Fatalf("expected tool content %q, got %q", "total 114", content)
		}
	}
	if !foundTool {
		t.Fatalf("expected at least one tool message, got: %s", string(out))
	}
}
