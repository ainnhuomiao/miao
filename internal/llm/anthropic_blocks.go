package llm

import "encoding/json"

// anthropicBlocks 保留原始内容块的顺序、thinking 签名及 redacted_thinking。
// 只拼接 SSE 增量，不重建服务端返回的内容，避免工具续轮丢失签名。
type anthropicBlocks struct {
	blocks []json.RawMessage
	cur    map[string]json.RawMessage
	input  string
	signed bool
}

func (b *anthropicBlocks) add(ev anthStreamEvent, data []byte) {
	switch ev.Type {
	case "content_block_start":
		if ev.ContentBlock != nil && (ev.ContentBlock.Type == "thinking" || ev.ContentBlock.Type == "redacted_thinking") {
			b.signed = true
		}
		var raw struct {
			Block map[string]json.RawMessage `json:"content_block"`
		}
		if json.Unmarshal(data, &raw) == nil {
			b.cur, b.input = raw.Block, ""
		}
	case "content_block_delta":
		if b.cur == nil || ev.Delta == nil {
			return
		}
		var raw struct {
			Delta map[string]string `json:"delta"`
		}
		if json.Unmarshal(data, &raw) != nil {
			return
		}
		var field string
		switch ev.Delta.Type {
		case "text_delta":
			field = "text"
		case "thinking_delta":
			field = "thinking"
		case "signature_delta":
			field = "signature"
		case "input_json_delta":
			b.input += ev.Delta.PartialJSON
		}
		if field != "" {
			var prev string
			_ = json.Unmarshal(b.cur[field], &prev)
			b.cur[field], _ = json.Marshal(prev + raw.Delta[field])
		}
	case "content_block_stop":
		if b.cur != nil {
			if json.Valid([]byte(b.input)) {
				b.cur["input"] = json.RawMessage(b.input)
			}
			raw, _ := json.Marshal(b.cur)
			b.blocks = append(b.blocks, raw)
			b.cur = nil
		}
	}
}

func (b *anthropicBlocks) content() json.RawMessage {
	if !b.signed || len(b.blocks) == 0 {
		return nil
	}
	raw, _ := json.Marshal(b.blocks)
	return raw
}

func (c anthContent) MarshalJSON() ([]byte, error) {
	if len(c.Raw) > 0 {
		return c.Raw, nil
	}
	type plain anthContent
	return json.Marshal(plain(c))
}
