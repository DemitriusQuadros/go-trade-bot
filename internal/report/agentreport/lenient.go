package agentreport

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DecodeBlockLenient decodes one model-written block. The canonical form is
// {"type": "...", "data": {...}}. Models sometimes flatten the data fields
// next to "type" ({"type":"summary","text":"..."}); when "data" is absent
// (or null) and other keys are present, those keys are moved into Data and
// moved reports true (fix-02 B3). A block that is not a JSON object is an
// error; anything else is left for Validate to judge.
func DecodeBlockLenient(raw json.RawMessage) (b Block, moved bool, err error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Block{}, false, fmt.Errorf("each block must be a {type, data} object")
	}
	if t, ok := fields["type"]; ok {
		if err := json.Unmarshal(t, &b.Type); err != nil {
			return Block{}, false, fmt.Errorf("block type must be a string")
		}
	}
	if data, ok := fields["data"]; ok && !bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		b.Data = data
		return b, false, nil
	}
	delete(fields, "type")
	delete(fields, "data")
	if len(fields) == 0 {
		return b, false, nil // no data at all: Validate reports "missing data"
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return Block{}, false, err
	}
	b.Data = data
	return b, true, nil
}

// DecodeBlocksLenient applies DecodeBlockLenient to each block and returns
// how many were flattened (for a debug log line).
func DecodeBlocksLenient(raws []json.RawMessage) ([]Block, int, error) {
	out := make([]Block, 0, len(raws))
	moved := 0
	for i, raw := range raws {
		b, m, err := DecodeBlockLenient(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("block %d: %w", i, err)
		}
		if m {
			moved++
		}
		out = append(out, b)
	}
	return out, moved, nil
}
