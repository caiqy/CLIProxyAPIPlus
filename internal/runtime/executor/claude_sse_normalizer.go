package executor

import (
	"bytes"
	"sort"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type AnthropicSSELifecycleNormalizer struct {
	currentEventLines [][]byte
	blocks            map[int]*anthropicSSEBlockState
}

type anthropicSSEBlockState struct {
	pendingStop      bool
	closed           bool
	pendingStopEvent [][]byte
}

func newAnthropicSSELifecycleNormalizer() *AnthropicSSELifecycleNormalizer {
	return &AnthropicSSELifecycleNormalizer{
		blocks: make(map[int]*anthropicSSEBlockState),
	}
}

func (n *AnthropicSSELifecycleNormalizer) ProcessLine(line []byte) ([][]byte, error) {
	if len(line) > 0 {
		n.currentEventLines = append(n.currentEventLines, bytes.Clone(line))
		return nil, nil
	}
	return n.finalizeBufferedEvent()
}

func (n *AnthropicSSELifecycleNormalizer) Finalize() ([][]byte, error) {
	var out [][]byte
	if len(n.currentEventLines) > 0 {
		eventOut, err := n.finalizeBufferedEvent()
		if err != nil {
			return nil, err
		}
		out = append(out, eventOut...)
	}
	out = append(out, n.flushPendingStops()...)
	return out, nil
}

func (n *AnthropicSSELifecycleNormalizer) finalizeBufferedEvent() ([][]byte, error) {
	if len(n.currentEventLines) == 0 {
		return [][]byte{{}}, nil
	}

	eventLines := n.currentEventLines
	n.currentEventLines = nil

	payload := anthropicSSEEventPayload(eventLines)
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return appendEventBoundary(eventLines), nil
	}

	eventType := gjson.GetBytes(payload, "type").String()
	if eventType == "" {
		return appendEventBoundary(eventLines), nil
	}

	switch eventType {
	case "content_block_start":
		idx, ok := anthropicEventIndex(payload)
		if !ok {
			log.Warnf("claude sse normalizer: missing index for event_type=%s; passthrough", eventType)
			return appendEventBoundary(eventLines), nil
		}
		out := n.flushPendingStops()
		block := n.block(idx)
		block.closed = false
		out = append(out, appendEventBoundary(eventLines)...)
		return out, nil
	case "content_block_delta":
		idx, ok := anthropicEventIndex(payload)
		if !ok {
			log.Warnf("claude sse normalizer: missing index for event_type=%s; passthrough", eventType)
			return appendEventBoundary(eventLines), nil
		}
		out := n.flushPendingStopsExcept(idx)
		block := n.block(idx)
		if block.closed {
			log.Warnf("claude sse normalizer: delta arrived after closed block index=%d event_type=%s; passthrough", idx, eventType)
			block.closed = false
		}
		if block.pendingStop {
			log.Debugf("claude sse normalizer: keep pending stop after later delta index=%d event_type=%s action=delay_stop", idx, eventType)
		}
		out = append(out, appendEventBoundary(eventLines)...)
		return out, nil
	case "content_block_stop":
		idx, ok := anthropicEventIndex(payload)
		if !ok {
			log.Warnf("claude sse normalizer: missing index for event_type=%s; passthrough", eventType)
			return appendEventBoundary(eventLines), nil
		}
		block := n.block(idx)
		if block.pendingStop {
			log.Debugf("claude sse normalizer: duplicate stop index=%d action=replace_pending_stop", idx)
		}
		block.pendingStop = true
		block.pendingStopEvent = cloneLines(eventLines)
		log.Debugf("claude sse normalizer: hold stop index=%d action=hold_stop", idx)
		return nil, nil
	case "message_delta", "message_stop":
		out := n.flushPendingStops()
		out = append(out, appendEventBoundary(eventLines)...)
		return out, nil
	default:
		return appendEventBoundary(eventLines), nil
	}
}

func (n *AnthropicSSELifecycleNormalizer) flushPendingStops() [][]byte {
	return n.flushPendingStopsExcept(-1)
}

func (n *AnthropicSSELifecycleNormalizer) flushPendingStopsExcept(exceptIndex int) [][]byte {
	if len(n.blocks) == 0 {
		return nil
	}

	indexes := make([]int, 0, len(n.blocks))
	for idx, block := range n.blocks {
		if idx == exceptIndex {
			continue
		}
		if block.pendingStop && len(block.pendingStopEvent) > 0 {
			indexes = append(indexes, idx)
		}
	}
	sort.Ints(indexes)

	out := make([][]byte, 0, len(indexes)*3)
	for _, idx := range indexes {
		block := n.blocks[idx]
		log.Debugf("claude sse normalizer: release pending stop index=%d action=release_pending_stop", idx)
		out = append(out, appendEventBoundary(block.pendingStopEvent)...)
		block.pendingStop = false
		block.pendingStopEvent = nil
		block.closed = true
	}
	return out
}

func (n *AnthropicSSELifecycleNormalizer) block(index int) *anthropicSSEBlockState {
	block, ok := n.blocks[index]
	if !ok {
		block = &anthropicSSEBlockState{}
		n.blocks[index] = block
	}
	return block
}

func anthropicSSEEventPayload(lines [][]byte) []byte {
	var payload []byte
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		trimmed = bytes.TrimSpace(trimmed[len("data:"):])
		if len(trimmed) == 0 {
			continue
		}
		if len(payload) > 0 {
			payload = append(payload, '\n')
		}
		payload = append(payload, trimmed...)
	}
	return payload
}

func appendEventBoundary(lines [][]byte) [][]byte {
	out := cloneLines(lines)
	return append(out, []byte{})
}

func cloneLines(lines [][]byte) [][]byte {
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		out = append(out, bytes.Clone(line))
	}
	return out
}

func anthropicEventIndex(payload []byte) (int, bool) {
	idx := gjson.GetBytes(payload, "index")
	if !idx.Exists() {
		return 0, false
	}
	return int(idx.Int()), true
}
