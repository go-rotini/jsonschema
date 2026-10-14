package jsonschema

import (
	"encoding/json"
	"unicode/utf8"
)

// fastDecodeMaxDepth bounds the nesting the fast decoder handles; deeper
// documents take the encoding/json path, which owns the depth limit.
const fastDecodeMaxDepth = 512

// decodeJSONFast decodes a whole JSON document into the shapes
// [encoding/json.Decoder] produces with UseNumber set: map[string]any,
// []any, string, [json.Number], bool and nil. It reports false for any
// input it does not decode exactly as encoding/json would — malformed or
// empty input, trailing content, duplicate object names, invalid UTF-8,
// unpaired surrogate escapes, deep nesting — and the caller then decodes
// with encoding/json, which produces the value or the error for it.
//
// Every string and number it returns is a copy of its own, so keeping one
// never keeps the rest of data alive.
func decodeJSONFast(data []byte) (any, bool) {
	v, _, ok := decodeJSONFastCount(data)
	return v, ok
}

// decodeJSONFastCount is decodeJSONFast that also returns the number of
// JSON values in the document, the document itself included.
func decodeJSONFastCount(data []byte) (any, int, bool) {
	d := fastDecoder{data: data}
	d.skipSpace()
	v, ok := d.value(0)
	if !ok {
		return nil, 0, false
	}
	d.skipSpace()
	if d.pos != len(d.data) {
		return nil, 0, false
	}
	return v, d.count, true
}

type fastDecoder struct {
	data []byte
	pos  int
	// stack holds the elements of the arrays and objects being decoded.
	stack []any
	// keys holds the names of the objects being decoded.
	keys []string
	// count is the number of values decoded.
	count int
}

func (d *fastDecoder) skipSpace() {
	for d.pos < len(d.data) {
		switch d.data[d.pos] {
		case ' ', '\t', '\n', '\r':
			d.pos++
		default:
			return
		}
	}
}

func (d *fastDecoder) value(depth int) (any, bool) {
	d.count++
	if d.pos >= len(d.data) {
		return nil, false
	}
	switch c := d.data[d.pos]; c {
	case '{':
		return d.object(depth + 1)
	case '[':
		return d.array(depth + 1)
	case '"':
		s, ok := d.str()
		if !ok {
			return nil, false
		}
		return s, true
	case 't':
		return true, d.literal("true")
	case 'f':
		return false, d.literal("false")
	case 'n':
		return nil, d.literal("null")
	default:
		if c == '-' || (c >= '0' && c <= '9') {
			return d.number()
		}
		return nil, false
	}
}

func (d *fastDecoder) literal(lit string) bool {
	if len(d.data)-d.pos < len(lit) || string(d.data[d.pos:d.pos+len(lit)]) != lit {
		return false
	}
	d.pos += len(lit)
	return true
}

// number scans a number literal per RFC 8259 §6.
func (d *fastDecoder) number() (any, bool) {
	start := d.pos
	data := d.data
	p := d.pos
	if data[p] == '-' {
		p++
	}
	switch {
	case p < len(data) && data[p] == '0':
		p++
	case p < len(data) && data[p] >= '1' && data[p] <= '9':
		p = skipDigits(data, p)
	default:
		return nil, false
	}
	if p < len(data) && data[p] == '.' {
		if p = skipDigits(data, p+1); data[p-1] == '.' {
			return nil, false
		}
	}
	if p < len(data) && (data[p] == 'e' || data[p] == 'E') {
		p++
		if p < len(data) && (data[p] == '+' || data[p] == '-') {
			p++
		}
		q := skipDigits(data, p)
		if q == p {
			return nil, false
		}
		p = q
	}
	d.pos = p
	return json.Number(string(d.data[start:p])), true
}

// skipDigits returns the index of the first non-digit at or after p.
func skipDigits(data []byte, p int) int {
	for p < len(data) && data[p] >= '0' && data[p] <= '9' {
		p++
	}
	return p
}

// str decodes the string starting at the opening quote.
func (d *fastDecoder) str() (string, bool) {
	data := d.data
	start := d.pos + 1
	p := start
	for p < len(data) {
		c := data[p]
		switch {
		case c == '"':
			d.pos = p + 1
			return string(d.data[start:p]), true
		case c == '\\':
			return d.escapedStr(start, p)
		case c < 0x20:
			return "", false
		case c < utf8.RuneSelf:
			p++
		default:
			r, size := utf8.DecodeRune(data[p:])
			if r == utf8.RuneError && size == 1 {
				return "", false
			}
			p += size
		}
	}
	return "", false
}

// escapedStr decodes the rest of a string that has an escape sequence at
// p; data[start:p] is its unescaped prefix.
func (d *fastDecoder) escapedStr(start, p int) (string, bool) {
	data := d.data
	buf := make([]byte, 0, p-start+16)
	buf = append(buf, data[start:p]...)
	for p < len(data) {
		c := data[p]
		switch {
		case c == '"':
			d.pos = p + 1
			return string(buf), true
		case c == '\\':
			var ok bool
			if buf, p, ok = appendEscape(buf, data, p); !ok {
				return "", false
			}
		case c < 0x20:
			return "", false
		case c < utf8.RuneSelf:
			buf = append(buf, c)
			p++
		default:
			r, size := utf8.DecodeRune(data[p:])
			if r == utf8.RuneError && size == 1 {
				return "", false
			}
			buf = append(buf, data[p:p+size]...)
			p += size
		}
	}
	return "", false
}

// appendEscape appends the character of the escape sequence at data[p] to
// buf and returns the index after the sequence.
func appendEscape(buf, data []byte, p int) ([]byte, int, bool) {
	if p+1 >= len(data) {
		return nil, 0, false
	}
	switch data[p+1] {
	case '"', '\\', '/':
		return append(buf, data[p+1]), p + 2, true
	case 'b':
		return append(buf, '\b'), p + 2, true
	case 'f':
		return append(buf, '\f'), p + 2, true
	case 'n':
		return append(buf, '\n'), p + 2, true
	case 'r':
		return append(buf, '\r'), p + 2, true
	case 't':
		return append(buf, '\t'), p + 2, true
	case 'u':
		r, ok := hex4(data, p+2)
		if !ok {
			return nil, 0, false
		}
		p += 6
		if r >= 0xD800 && r < 0xE000 {
			// Only a high surrogate followed by an escaped low surrogate is
			// decoded here.
			if r >= 0xDC00 || p+1 >= len(data) || data[p] != '\\' || data[p+1] != 'u' {
				return nil, 0, false
			}
			lo, ok := hex4(data, p+2)
			if !ok || lo < 0xDC00 || lo >= 0xE000 {
				return nil, 0, false
			}
			p += 6
			r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
		}
		return utf8.AppendRune(buf, r), p, true
	}
	return nil, 0, false
}

// hex4 decodes the four hex digits at data[p:p+4].
func hex4(data []byte, p int) (rune, bool) {
	if p+4 > len(data) {
		return 0, false
	}
	var r rune
	for _, c := range data[p : p+4] {
		switch {
		case c >= '0' && c <= '9':
			c -= '0'
		case c >= 'a' && c <= 'f':
			c = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			c = c - 'A' + 10
		default:
			return 0, false
		}
		r = r<<4 | rune(c)
	}
	return r, true
}

func (d *fastDecoder) array(depth int) (any, bool) {
	if depth > fastDecodeMaxDepth {
		return nil, false
	}
	d.pos++ // '['
	base := len(d.stack)
	d.skipSpace()
	if d.pos < len(d.data) && d.data[d.pos] == ']' {
		d.pos++
		return []any{}, true
	}
	for {
		d.skipSpace()
		v, ok := d.value(depth)
		if !ok {
			return nil, false
		}
		d.stack = append(d.stack, v)
		d.skipSpace()
		if d.pos >= len(d.data) {
			return nil, false
		}
		c := d.data[d.pos]
		d.pos++
		if c == ']' {
			break
		}
		if c != ',' {
			return nil, false
		}
	}
	out := make([]any, len(d.stack)-base)
	copy(out, d.stack[base:])
	clear(d.stack[base:])
	d.stack = d.stack[:base]
	return out, true
}

func (d *fastDecoder) object(depth int) (any, bool) {
	if depth > fastDecodeMaxDepth {
		return nil, false
	}
	d.pos++ // '{'
	base := len(d.stack)
	d.skipSpace()
	if d.pos < len(d.data) && d.data[d.pos] == '}' {
		d.pos++
		return map[string]any{}, true
	}
	for {
		d.skipSpace()
		if d.pos >= len(d.data) || d.data[d.pos] != '"' {
			return nil, false
		}
		k, ok := d.str()
		if !ok {
			return nil, false
		}
		d.skipSpace()
		if d.pos >= len(d.data) || d.data[d.pos] != ':' {
			return nil, false
		}
		d.pos++
		d.skipSpace()
		v, ok := d.value(depth)
		if !ok {
			return nil, false
		}
		d.keys = append(d.keys, k)
		d.stack = append(d.stack, v)
		d.skipSpace()
		if d.pos >= len(d.data) {
			return nil, false
		}
		c := d.data[d.pos]
		d.pos++
		if c == '}' {
			break
		}
		if c != ',' {
			return nil, false
		}
	}
	n := len(d.stack) - base
	kbase := len(d.keys) - n
	out := make(map[string]any, n)
	for i := range n {
		k := d.keys[kbase+i]
		if _, dup := out[k]; dup {
			return nil, false
		}
		out[k] = d.stack[base+i]
	}
	clear(d.stack[base:])
	d.stack = d.stack[:base]
	d.keys = d.keys[:kbase]
	return out, true
}
