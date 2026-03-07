package executor

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/tidwall/gjson"
)

func TestClaudeSSENormalizer_HoldsStopUntilMessageDelta(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	beforeMessageDelta := collectNormalizedLines(t, n,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"he"}}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
	)
	if got := normalizedEventTypes(beforeMessageDelta); !reflect.DeepEqual(got, []string{"content_block_start", "content_block_delta"}) {
		t.Fatalf("event types before message_delta = %#v, want start+delta only", got)
	}

	afterMessageDelta := collectNormalizedLines(t, n,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		"",
	)
	if got := normalizedEventTypes(afterMessageDelta); !reflect.DeepEqual(got, []string{"content_block_stop", "message_delta"}) {
		t.Fatalf("event types after message_delta = %#v, want stop then message_delta", got)
	}
}

func TestClaudeSSENormalizer_DelaysPendingStopWhenDeltaArrivesAfterStop(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	got := append(
		collectNormalizedLines(t, n,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			"",
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"he"}}`,
			"",
			`data: {"type":"content_block_stop","index":0}`,
			"",
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"llo"}}`,
			"",
		),
		collectNormalizedLines(t, n,
			`data: {"type":"message_stop"}`,
			"",
		)...,
	)

	if gotTypes := normalizedEventTypes(got); !reflect.DeepEqual(gotTypes, []string{
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_stop",
	}) {
		t.Fatalf("event types = %#v", gotTypes)
	}
}

func TestClaudeSSENormalizer_DuplicateStopEmitsOneStop(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	got := collectNormalizedLines(t, n,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
	)

	if gotTypes := normalizedEventTypes(got); !reflect.DeepEqual(gotTypes, []string{
		"content_block_start",
		"content_block_stop",
		"message_stop",
	}) {
		t.Fatalf("event types = %#v", gotTypes)
	}
}

func TestClaudeSSENormalizer_FinalizeFlushesPendingStopAtEOF(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	beforeFinalize := collectNormalizedLines(t, n,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"he"}}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
	)
	if got := normalizedEventTypes(beforeFinalize); !reflect.DeepEqual(got, []string{"content_block_start", "content_block_delta"}) {
		t.Fatalf("event types before finalize = %#v, want start+delta only", got)
	}

	flushed, err := n.Finalize()
	if err != nil {
		t.Fatalf("Finalize error: %v", err)
	}
	if got := normalizedEventTypes(byteLinesToStrings(flushed)); !reflect.DeepEqual(got, []string{"content_block_stop"}) {
		t.Fatalf("event types from finalize = %#v, want stop", got)
	}
}

func TestClaudeSSENormalizer_PreservesSSEEventBoundariesAndPassthroughEvents(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	got := collectNormalizedLines(t, n,
		"event: ping",
		`data: {"type":"ping"}`,
		"",
	)
	want := []string{
		"event: ping",
		`data: {"type":"ping"}`,
		"",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}

func TestClaudeSSENormalizer_PreservesDeltaPayloadBytes(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	got := collectNormalizedLines(t, n,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello <> 世界"}}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
	)

	if gotText := firstDeltaValue(got, "delta.text"); gotText != "hello <> 世界" {
		t.Fatalf("delta.text = %q, want %q", gotText, "hello <> 世界")
	}
}

func TestClaudeSSENormalizer_PreservesThinkingAndPartialJSONPayloadBytes(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	got := collectNormalizedLines(t, n,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"思考中"}}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_1","name":"Read","input":{}}}`,
		"",
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.txt\"}"}}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
	)

	if gotThinking := firstDeltaValueForIndex(got, 0, "delta.thinking"); gotThinking != "思考中" {
		t.Fatalf("delta.thinking = %q, want %q", gotThinking, "思考中")
	}
	if gotJSON := firstDeltaValueForIndex(got, 1, "delta.partial_json"); gotJSON != `{"path":"a.txt"}` {
		t.Fatalf("delta.partial_json = %q, want %q", gotJSON, `{"path":"a.txt"}`)
	}
}

func TestClaudeSSENormalizer_TracksIndependentIndexesAndBlockTypes(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	got := append(
		collectNormalizedLines(t, n,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			"",
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"a"}}`,
			"",
			`data: {"type":"content_block_stop","index":0}`,
			"",
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_1","name":"Read","input":{}}}`,
			"",
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
			"",
			`data: {"type":"message_stop"}`,
			"",
		),
		byteLinesToStrings(mustFinalizeLines(t, n))...,
	)

	if gotTypes := normalizedEventTypes(got); !reflect.DeepEqual(gotTypes, []string{
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"content_block_start",
		"content_block_delta",
		"message_stop",
	}) {
		t.Fatalf("event types = %#v", gotTypes)
	}
}

func TestClaudeSSENormalizer_FlushesPendingStopsInIndexOrder(t *testing.T) {
	t.Parallel()
	n := newAnthropicSSELifecycleNormalizer()

	got := collectNormalizedLines(t, n,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_stop","index":1}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
	)

	if gotDescriptors := normalizedEventDescriptors(got); !reflect.DeepEqual(gotDescriptors, []string{
		"content_block_start#1",
		"content_block_start#0",
		"content_block_stop#0",
		"content_block_stop#1",
		"message_stop",
	}) {
		t.Fatalf("event descriptors = %#v", gotDescriptors)
	}
}

func collectNormalizedLines(t *testing.T, n *AnthropicSSELifecycleNormalizer, lines ...string) []string {
	t.Helper()
	var out []string
	for _, line := range lines {
		normalized, err := n.ProcessLine([]byte(line))
		if err != nil {
			t.Fatalf("ProcessLine(%q) error: %v", line, err)
		}
		out = append(out, byteLinesToStrings(normalized)...)
	}
	return out
}

func mustFinalizeLines(t *testing.T, n *AnthropicSSELifecycleNormalizer) [][]byte {
	t.Helper()
	lines, err := n.Finalize()
	if err != nil {
		t.Fatalf("Finalize error: %v", err)
	}
	return lines
}

func byteLinesToStrings(lines [][]byte) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, string(line))
	}
	return out
}

func normalizedEventTypes(lines []string) []string {
	types := make([]string, 0, len(lines))
	for _, line := range lines {
		payload := jsonPayload([]byte(line))
		if len(payload) == 0 {
			continue
		}
		eventType := gjson.GetBytes(payload, "type").String()
		if eventType == "" {
			panic(fmt.Sprintf("missing type in payload: %s", payload))
		}
		types = append(types, eventType)
	}
	return types
}

func normalizedEventDescriptors(lines []string) []string {
	descriptors := make([]string, 0, len(lines))
	for _, line := range lines {
		payload := jsonPayload([]byte(line))
		if len(payload) == 0 {
			continue
		}
		eventType := gjson.GetBytes(payload, "type").String()
		if eventType == "" {
			panic(fmt.Sprintf("missing type in payload: %s", payload))
		}
		idx := gjson.GetBytes(payload, "index")
		if idx.Exists() {
			descriptors = append(descriptors, fmt.Sprintf("%s#%d", eventType, idx.Int()))
			continue
		}
		descriptors = append(descriptors, eventType)
	}
	return descriptors
}

func firstDeltaValue(lines []string, path string) string {
	for _, line := range lines {
		payload := jsonPayload([]byte(line))
		if len(payload) == 0 {
			continue
		}
		if gjson.GetBytes(payload, "type").String() != "content_block_delta" {
			continue
		}
		return gjson.GetBytes(payload, path).String()
	}
	return ""
}

func firstDeltaValueForIndex(lines []string, index int, path string) string {
	for _, line := range lines {
		payload := jsonPayload([]byte(line))
		if len(payload) == 0 {
			continue
		}
		if gjson.GetBytes(payload, "type").String() != "content_block_delta" {
			continue
		}
		if int(gjson.GetBytes(payload, "index").Int()) != index {
			continue
		}
		return gjson.GetBytes(payload, path).String()
	}
	return ""
}
