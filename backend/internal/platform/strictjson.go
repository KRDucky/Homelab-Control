// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package platform

import (
	"bytes"
	"encoding/json"
	"io"
)

// DecodeBounded decodes exactly one JSON value of at most max bytes after
// rejecting duplicate object keys and nesting deeper than maxDepth, so two
// parsers can never disagree about which field is authoritative. Unknown
// fields are allowed: platform APIs return far more than adapters read, and
// adapters copy only the fields they validate.
func DecodeBounded(b []byte, max, maxDepth int, v any) error {
	if len(b) == 0 || len(b) > max || !uniqueKeys(b, maxDepth) {
		return ErrUnavailable
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if dec.Decode(v) != nil {
		return ErrUnavailable
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return ErrUnavailable
	}
	return nil
}

func uniqueKeys(b []byte, maxDepth int) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > maxDepth {
			return false
		}
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for dec.More() {
				k, err := dec.Token()
				key, ok := k.(string)
				if err != nil || !ok || keys[key] {
					return false
				}
				keys[key] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := dec.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for dec.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := dec.Token()
			return err == nil && end == json.Delim(']')
		}
		return false
	}
	if !value(0) {
		return false
	}
	_, err := dec.Token()
	return err == io.EOF
}
