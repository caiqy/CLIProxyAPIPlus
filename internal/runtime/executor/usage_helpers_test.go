package executor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

// testUsagePlugin captures usage records published through the global manager.
type testUsagePlugin struct {
	mu      sync.Mutex
	records []usage.Record
	done    chan struct{}
}

func newTestUsagePlugin() *testUsagePlugin {
	return &testUsagePlugin{done: make(chan struct{}, 16)}
}

func (p *testUsagePlugin) HandleUsage(_ context.Context, record usage.Record) {
	p.mu.Lock()
	p.records = append(p.records, record)
	p.mu.Unlock()
	select {
	case p.done <- struct{}{}:
	default:
	}
}

func (p *testUsagePlugin) waitOne(t *testing.T) usage.Record {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for usage record to be published")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.records[len(p.records)-1]
}

func TestParseOpenAIUsageChatCompletions(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":5}}}`)
	detail := parseOpenAIUsage(data)
	if detail.InputTokens != 1 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 1)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 3)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

func TestParseOpenAIUsageResponses(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":9}}}`)
	detail := parseOpenAIUsage(data)
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.ReasoningTokens != 9 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 9)
	}
}

func TestParseOpenAIResponsesStreamUsage_FromResponseObject(t *testing.T) {
	line := []byte(`data: {"type":"response.completed","response":{"usage":{"input_tokens":11,"output_tokens":22,"total_tokens":33,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":5}}}}`)
	detail, ok := parseOpenAIResponsesStreamUsage(line)
	if !ok {
		t.Fatalf("parseOpenAIResponsesStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 11 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 11)
	}
	if detail.OutputTokens != 22 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 22)
	}
	if detail.TotalTokens != 33 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 33)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

// ---------------------------------------------------------------------------
// Claude usage parsing tests
// ---------------------------------------------------------------------------

func TestParseClaudeUsage_TopLevelUsage(t *testing.T) {
	// Standard non-streaming Claude response with top-level usage
	data := []byte(`{"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":200,"cache_creation_input_tokens":30}}`)
	detail := parseClaudeUsage(data)
	// InputTokens = input_tokens(100) + cache_creation(30) = 130
	if detail.InputTokens != 130 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 130)
	}
	if detail.OutputTokens != 50 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 50)
	}
	// CachedTokens = cache_read (200, non-zero so no fallback to creation)
	if detail.CachedTokens != 200 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 200)
	}
	if detail.TotalTokens != 180 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 180)
	}
}

func TestParseClaudeUsage_MessageUsageFallback(t *testing.T) {
	// message_start style: usage nested under message.usage
	data := []byte(`{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":8,"cache_read_input_tokens":20797,"cache_creation_input_tokens":2332}}}`)
	detail := parseClaudeUsage(data)
	// InputTokens = input_tokens(1) + cache_creation(2332) = 2333
	if detail.InputTokens != 2333 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 2333)
	}
	if detail.OutputTokens != 8 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 8)
	}
	if detail.CachedTokens != 20797 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 20797)
	}
	if detail.TotalTokens != 2341 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 2341)
	}
}

func TestParseClaudeUsage_CacheCreationFallbackWhenNoRead(t *testing.T) {
	// First request: cache created but no cache read yet
	data := []byte(`{"usage":{"input_tokens":5,"output_tokens":10,"cache_creation_input_tokens":500}}`)
	detail := parseClaudeUsage(data)
	// InputTokens = 5 + 500 = 505
	if detail.InputTokens != 505 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 505)
	}
	// CachedTokens falls back to cache_creation when cache_read is 0
	if detail.CachedTokens != 500 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 500)
	}
}

func TestParseClaudeUsage_NoCacheTokens(t *testing.T) {
	// No cache tokens at all
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":20}}`)
	detail := parseClaudeUsage(data)
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.CachedTokens != 0 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 0)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
}

func TestParseClaudeUsage_EmptyPayload(t *testing.T) {
	detail := parseClaudeUsage([]byte(`{}`))
	if detail.TotalTokens != 0 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 0)
	}
}

// ---------------------------------------------------------------------------
// Claude stream usage parsing tests
// ---------------------------------------------------------------------------

func TestParseClaudeStreamUsage_TopLevelUsage(t *testing.T) {
	// message_delta event: usage at top level
	line := []byte(`data: {"type":"message_delta","usage":{"output_tokens":1358}}`)
	detail, ok := parseClaudeStreamUsage(line)
	if !ok {
		t.Fatal("parseClaudeStreamUsage() ok = false, want true")
	}
	if detail.OutputTokens != 1358 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 1358)
	}
}

func TestParseClaudeStreamUsage_MessageUsageFallback(t *testing.T) {
	// message_start event: usage under message.usage (production data sample)
	line := []byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":8,"cache_read_input_tokens":20797,"cache_creation_input_tokens":2332}}}`)
	detail, ok := parseClaudeStreamUsage(line)
	if !ok {
		t.Fatal("parseClaudeStreamUsage() ok = false, want true")
	}
	// InputTokens = 1 + 2332 = 2333
	if detail.InputTokens != 2333 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 2333)
	}
	if detail.CachedTokens != 20797 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 20797)
	}
	if detail.OutputTokens != 8 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 8)
	}
}

func TestParseClaudeStreamUsage_NoUsage(t *testing.T) {
	// content_block_delta: no usage at all
	line := []byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`)
	_, ok := parseClaudeStreamUsage(line)
	if ok {
		t.Fatal("parseClaudeStreamUsage() ok = true, want false for non-usage event")
	}
}

func TestParseClaudeStreamUsage_EventPrefix(t *testing.T) {
	// event: lines should be skipped
	line := []byte(`event: message_start`)
	_, ok := parseClaudeStreamUsage(line)
	if ok {
		t.Fatal("parseClaudeStreamUsage() should return false for event: lines")
	}
}

// ---------------------------------------------------------------------------
// accumulateClaudeStreamUsage tests
// ---------------------------------------------------------------------------

func TestAccumulateClaudeStreamUsage_MergesAcrossEvents(t *testing.T) {
	// Simulate real production: message_start has input/cache, message_delta has output
	var accum usage.Detail

	// message_start event
	messageStart := usage.Detail{
		InputTokens:  2333, // 1 + 2332 (cache creation)
		OutputTokens: 8,    // initial small output
		CachedTokens: 20797,
	}
	accumulateClaudeStreamUsage(&accum, messageStart)

	if accum.InputTokens != 2333 {
		t.Fatalf("after message_start: input = %d, want %d", accum.InputTokens, 2333)
	}
	if accum.CachedTokens != 20797 {
		t.Fatalf("after message_start: cached = %d, want %d", accum.CachedTokens, 20797)
	}
	if accum.OutputTokens != 8 {
		t.Fatalf("after message_start: output = %d, want %d", accum.OutputTokens, 8)
	}

	// message_delta event (final output tokens)
	messageDelta := usage.Detail{
		OutputTokens: 1358,
	}
	accumulateClaudeStreamUsage(&accum, messageDelta)

	// max() should keep input/cache from message_start and output from message_delta
	if accum.InputTokens != 2333 {
		t.Fatalf("after accumulation: input = %d, want %d", accum.InputTokens, 2333)
	}
	if accum.CachedTokens != 20797 {
		t.Fatalf("after accumulation: cached = %d, want %d", accum.CachedTokens, 20797)
	}
	if accum.OutputTokens != 1358 {
		t.Fatalf("after accumulation: output = %d, want %d", accum.OutputTokens, 1358)
	}
}

func TestAccumulateClaudeStreamUsage_ReasoningTokens(t *testing.T) {
	var accum usage.Detail

	accumulateClaudeStreamUsage(&accum, usage.Detail{ReasoningTokens: 100})
	accumulateClaudeStreamUsage(&accum, usage.Detail{ReasoningTokens: 50})

	// max() should keep 100
	if accum.ReasoningTokens != 100 {
		t.Fatalf("reasoning tokens = %d, want %d", accum.ReasoningTokens, 100)
	}
}

func TestAccumulateClaudeStreamUsage_ZeroEvents(t *testing.T) {
	var accum usage.Detail
	// No accumulation: should remain zero
	if accum.InputTokens != 0 || accum.OutputTokens != 0 || accum.CachedTokens != 0 {
		t.Fatal("zero-value accumulator should have all zeros")
	}
}

// ---------------------------------------------------------------------------
// publishAccumulatedClaudeUsage tests
// ---------------------------------------------------------------------------

func TestPublishAccumulatedClaudeUsage_SetsTotalTokens(t *testing.T) {
	plugin := newTestUsagePlugin()
	usage.RegisterPlugin(plugin)

	reporter := &usageReporter{provider: "test-claude", model: "claude-test"}
	accum := usage.Detail{
		InputTokens:     2333,
		OutputTokens:    1358,
		CachedTokens:    20797,
		ReasoningTokens: 42,
	}
	publishAccumulatedClaudeUsage(context.Background(), reporter, accum)

	record := plugin.waitOne(t)
	// TotalTokens = Input(2333) + Output(1358) + Reasoning(42) = 3733
	if record.Detail.TotalTokens != 3733 {
		t.Fatalf("total tokens = %d, want %d", record.Detail.TotalTokens, 3733)
	}
	if record.Detail.InputTokens != 2333 {
		t.Fatalf("input tokens = %d, want %d", record.Detail.InputTokens, 2333)
	}
	if record.Detail.OutputTokens != 1358 {
		t.Fatalf("output tokens = %d, want %d", record.Detail.OutputTokens, 1358)
	}
	if record.Detail.CachedTokens != 20797 {
		t.Fatalf("cached tokens = %d, want %d", record.Detail.CachedTokens, 20797)
	}
	if record.Detail.ReasoningTokens != 42 {
		t.Fatalf("reasoning tokens = %d, want %d", record.Detail.ReasoningTokens, 42)
	}
}

func TestPublishAccumulatedClaudeUsage_SkipsAllZero(t *testing.T) {
	// Zero accumulator should not attempt to publish (no panic with nil reporter)
	accum := usage.Detail{}
	// This should not panic even with nil reporter because it returns early
	publishAccumulatedClaudeUsage(nil, nil, accum)
}

// ---------------------------------------------------------------------------
// End-to-end: production data scenario
// ---------------------------------------------------------------------------

func TestClaudeUsage_ProductionScenario(t *testing.T) {
	// Simulate the exact production SSE sequence from Copilot Claude response:
	// 1. message_start with input/cache usage
	// 2. multiple content_block_delta (no usage)
	// 3. message_delta with final output_tokens

	lines := [][]byte{
		// message_start
		[]byte(`data: {"type":"message_start","message":{"id":"msg_xxx","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"stop_sequence":null,"usage":{"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":2332},"cache_creation_input_tokens":2332,"cache_read_input_tokens":20797,"input_tokens":1,"output_tokens":8}}}`),
		// content_block_start (no usage)
		[]byte(`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		// content_block_delta (no usage)
		[]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		// content_block_stop (no usage)
		[]byte(`data: {"type":"content_block_stop","index":0}`),
		// message_delta (final output tokens)
		[]byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1358}}`),
		// message_stop (no usage)
		[]byte(`data: {"type":"message_stop"}`),
	}

	// Parse and accumulate (same as executor streaming loop)
	var accum usage.Detail
	for _, line := range lines {
		if detail, ok := parseClaudeStreamUsage(line); ok {
			accumulateClaudeStreamUsage(&accum, detail)
		}
	}

	// Verify accumulated values before publish
	if accum.InputTokens != 2333 {
		t.Fatalf("input tokens = %d, want %d (1 + 2332 cache_creation)", accum.InputTokens, 2333)
	}
	if accum.CachedTokens != 20797 {
		t.Fatalf("cached tokens = %d, want %d", accum.CachedTokens, 20797)
	}
	if accum.OutputTokens != 1358 {
		t.Fatalf("output tokens = %d, want %d", accum.OutputTokens, 1358)
	}

	// Publish through the real path and verify via plugin
	plugin := newTestUsagePlugin()
	usage.RegisterPlugin(plugin)

	reporter := &usageReporter{provider: "github-copilot", model: "claude-sonnet-4-20250514"}
	publishAccumulatedClaudeUsage(context.Background(), reporter, accum)

	record := plugin.waitOne(t)
	if record.Detail.InputTokens != 2333 {
		t.Fatalf("published input tokens = %d, want %d", record.Detail.InputTokens, 2333)
	}
	if record.Detail.CachedTokens != 20797 {
		t.Fatalf("published cached tokens = %d, want %d", record.Detail.CachedTokens, 20797)
	}
	if record.Detail.OutputTokens != 1358 {
		t.Fatalf("published output tokens = %d, want %d", record.Detail.OutputTokens, 1358)
	}
	// TotalTokens = Input(2333) + Output(1358) + Reasoning(0) = 3691
	if record.Detail.TotalTokens != 3691 {
		t.Fatalf("published total tokens = %d, want %d", record.Detail.TotalTokens, 3691)
	}
}

func TestParseOpenAIStreamUsage_ResponsesFieldNames(t *testing.T) {
	line := []byte(`data: {"usage":{"input_tokens":11,"output_tokens":22,"total_tokens":33,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":5}}}`)
	detail, ok := parseOpenAIStreamUsage(line)
	if !ok {
		t.Fatalf("parseOpenAIStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 11 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 11)
	}
	if detail.OutputTokens != 22 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 22)
	}
	if detail.TotalTokens != 33 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 33)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}
