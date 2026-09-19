package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// This file owns the second byte contract of the JSON layer: the shape Python's
// plain json.dumps(value, ensure_ascii=False) produces. Unlike the canonical
// format (sorted keys, compact separators), this encoding keeps the object's
// insertion/document order and uses Python's default separators ", " and ": ".
// The number and string rules are NOT reimplemented here: canonicalNumber,
// pythonFloatRepr and writeCanonicalString (canonical.go) remain the single
// implementation of both.
//
// PyDumpsRaw is this file's only export. Everything else serves the native
// transport in this package, where the two observable byte-fidelity surfaces
// (JSON embedded in error texts, the schema embedded in the prompt) and the
// Python decoded-value semantics (dict order, truthiness, equality, str())
// live; the outbound protocol lines, protocol.jsonl and stdout use the
// canonical compact form instead (registered bounded differences).

// ordObj is a JSON object that remembers its key order, the way a Python dict
// does. Go's map iteration order is randomised, and a map that leaks into this
// encoding would make the bytes non-deterministic, so unordered maps are
// rejected and every ordered object is built with dict.
type ordObj struct {
	keys   []string
	values map[string]any
}

// dict builds an ordered object from alternating key/value arguments, the Go
// spelling of a Python dict literal. A repeated key keeps its first position
// and takes the last value, exactly like Python. The keys must be strings; a
// non-string key or an odd argument count is a programming error and panics.
func dict(pairs ...any) ordObj {
	if len(pairs)%2 != 0 {
		panic(fmt.Sprintf("dict: odd number of arguments (%d)", len(pairs)))
	}
	object := ordObj{values: make(map[string]any, len(pairs)/2)}
	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			panic(fmt.Sprintf("dict: argument %d is not a string key (%T)", i, pairs[i]))
		}
		object.Set(key, pairs[i+1])
	}
	return object
}

// Get returns the value stored under key.
func (o ordObj) Get(key string) (any, bool) {
	if o.values == nil {
		return nil, false
	}
	value, ok := o.values[key]
	return value, ok
}

// Has reports whether key is present, including an explicit null value.
func (o ordObj) Has(key string) bool {
	_, ok := o.Get(key)
	return ok
}

// Set stores value under key, replacing an existing entry in place or
// appending a new key at the end (Python dict assignment semantics).
func (o *ordObj) Set(key string, value any) {
	if o.values == nil {
		o.values = make(map[string]any)
	}
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Len returns the number of entries.
func (o ordObj) Len() int { return len(o.keys) }

// Keys returns the key order as a fresh slice.
func (o ordObj) Keys() []string {
	out := make([]string, len(o.keys))
	copy(out, o.keys)
	return out
}

// MarshalJSON renders the ordered object as compact JSON so the value can
// travel through encoding/json based callers (the bridge envelope) without
// losing its order.
func (o ordObj) MarshalJSON() ([]byte, error) {
	var out strings.Builder
	if err := encodePyJSON(&out, o, ",", ":"); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// pyDumps reproduces Python's json.dumps(value, ensure_ascii=False) with the
// default separators: ", " between items, ": " after every key, and object keys
// in insertion/document order. Ordered objects must be built with dict; a plain
// map[string]any is refused because its iteration order would leak into the
// byte contract.
func pyDumps(value any) (string, error) {
	var out strings.Builder
	if err := encodePyJSON(&out, value, ", ", ": "); err != nil {
		return "", err
	}
	return out.String(), nil
}

// PyDumpsRaw re-encodes raw JSON bytes the way Python's
// json.dumps(json.loads(raw), ensure_ascii=False) does: key order is the
// document order, duplicate keys collapse to the last value in the first
// position, integer literals keep arbitrary precision and float literals are
// re-rendered with Python's repr rules (1e2 becomes 100.0). It exists for the
// two observable surfaces that must stay byte-identical to the pre-migration
// implementation: JSON embedded in error texts and the schema embedded in the
// prompt text.
func PyDumpsRaw(raw []byte) (string, error) {
	value, err := decodeOrdered(raw)
	if err != nil {
		return "", err
	}
	return pyDumps(value)
}

// encodePyJSON walks value writing Python's plain json.dumps shape. Separators
// are parameters so the compact ordered form used by ordObj.MarshalJSON shares
// this walk instead of growing a second encoder.
func encodePyJSON(out *strings.Builder, value any, itemSep, keySep string) error {
	switch typed := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if typed {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case json.Number:
		encoded, err := canonicalNumber(typed.String())
		if err != nil {
			return err
		}
		out.WriteString(encoded)
	case int:
		out.WriteString(fmt.Sprintf("%d", typed))
	case int64:
		out.WriteString(fmt.Sprintf("%d", typed))
	case float64:
		out.WriteString(pythonFloatRepr(typed))
	case string:
		writeCanonicalString(typed, out)
	case []any:
		out.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				out.WriteString(itemSep)
			}
			if err := encodePyJSON(out, item, itemSep, keySep); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case ordObj:
		out.WriteByte('{')
		for i, key := range typed.keys {
			if i > 0 {
				out.WriteString(itemSep)
			}
			writeCanonicalString(key, out)
			out.WriteString(keySep)
			if err := encodePyJSON(out, typed.values[key], itemSep, keySep); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case map[string]any:
		return fmt.Errorf("pydumps: map[string]any has no key order; build ordered objects with dict")
	default:
		return fmt.Errorf("pydumps: unsupported value type %T", value)
	}
	return nil
}

// jsonSyntaxError reproduces CPython's json.JSONDecodeError rendering so the
// observable error text of a malformed document survives the migration.
type jsonSyntaxError struct {
	message string
	line    int
	column  int
	char    int
}

func (e *jsonSyntaxError) Error() string {
	return fmt.Sprintf("%s: line %d column %d (char %d)", e.message, e.line, e.column, e.char)
}

// decodeOrdered parses one JSON document preserving object key order, with
// json.loads semantics: duplicate keys collapse to the last value while the
// first position is kept. Numbers stay as json.Number literals so the encoders
// can decide between integer and float the way Python does. Python's
// non-standard Infinity/-Infinity/NaN literals are accepted and become floats,
// which both encoders then render as Python does.
func decodeOrdered(raw []byte) (any, error) {
	return decodePythonJSON(raw, false)
}

// decodeOrderedUnique is decodeOrdered with infra.read semantics instead: a
// repeated object key is an error, never silently collapsed.
func decodeOrderedUnique(raw []byte) (any, error) {
	return decodePythonJSON(raw, true)
}

func decodePythonJSON(raw []byte, unique bool) (any, error) {
	parser := &pythonJSONParser{doc: raw, unique: unique}
	parser.skipWhitespace()
	value, err := parser.parseValue()
	if err != nil {
		return nil, err
	}
	parser.skipWhitespace()
	if parser.pos < len(parser.doc) {
		return nil, parser.errorAt(parser.pos, "Extra data")
	}
	return value, nil
}

type pythonJSONParser struct {
	doc    []byte
	pos    int
	unique bool
}

func (p *pythonJSONParser) skipWhitespace() {
	for p.pos < len(p.doc) {
		switch p.doc[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// errorAt formats an error the way CPython's JSONDecodeError does: the message
// plus 1-based line and column and the 0-based character offset.
func (p *pythonJSONParser) errorAt(pos int, message string) error {
	if pos > len(p.doc) {
		pos = len(p.doc)
	}
	if pos < 0 {
		pos = 0
	}
	line := 1 + bytes.Count(p.doc[:pos], []byte{'\n'})
	lastNewline := bytes.LastIndexByte(p.doc[:pos], '\n')
	column := pos - lastNewline // LastIndexByte returns -1 when absent, giving pos+1
	return &jsonSyntaxError{message: message, line: line, column: column, char: pos}
}

func (p *pythonJSONParser) parseValue() (any, error) {
	p.skipWhitespace()
	if p.pos >= len(p.doc) {
		return nil, p.errorAt(p.pos, "Expecting value")
	}
	switch c := p.doc[p.pos]; {
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseArray()
	case c == '"':
		return p.parseString()
	case c == 't':
		return p.parseLiteral("true", true)
	case c == 'f':
		return p.parseLiteral("false", false)
	case c == 'n':
		return p.parseLiteral("null", nil)
	case c == 'I':
		return p.parseLiteral("Infinity", math.Inf(1))
	case c == 'N':
		return p.parseLiteral("NaN", math.NaN())
	case c == '-' && p.pos+1 < len(p.doc) && p.doc[p.pos+1] == 'I':
		return p.parseLiteral("-Infinity", math.Inf(-1))
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return nil, p.errorAt(p.pos, "Expecting value")
	}
}

func (p *pythonJSONParser) parseLiteral(word string, value any) (any, error) {
	start := p.pos
	if p.pos+len(word) <= len(p.doc) && string(p.doc[p.pos:p.pos+len(word)]) == word {
		p.pos += len(word)
		return value, nil
	}
	return nil, p.errorAt(start, "Expecting value")
}

func (p *pythonJSONParser) parseNumber() (any, error) {
	start := p.pos
	if p.doc[p.pos] == '-' {
		p.pos++
	}
	// JSON's integer part is 0 | [1-9][0-9]*: a leading zero stands alone, so
	// "01" parses as 0 with "1" left over (CPython then reports Extra data).
	if p.pos < len(p.doc) && p.doc[p.pos] == '0' {
		p.pos++
	} else if !p.digits() {
		return nil, p.errorAt(start, "Expecting value")
	}
	if p.pos < len(p.doc) && p.doc[p.pos] == '.' {
		mark := p.pos
		p.pos++
		if !p.digits() {
			// A fraction without digits is not part of the number: CPython's
			// number regex stops before the dot and the caller reports the rest
			// (for example "Extra data" at the top level, pointing at the dot).
			p.pos = mark
		}
	}
	if p.pos < len(p.doc) && (p.doc[p.pos] == 'e' || p.doc[p.pos] == 'E') {
		mark := p.pos
		p.pos++
		if p.pos < len(p.doc) && (p.doc[p.pos] == '+' || p.doc[p.pos] == '-') {
			p.pos++
		}
		if !p.digits() {
			p.pos = mark // exponent without digits is not part of the number
		}
	}
	return json.Number(string(p.doc[start:p.pos])), nil
}

func (p *pythonJSONParser) digits() bool {
	digits := false
	for p.pos < len(p.doc) && p.doc[p.pos] >= '0' && p.doc[p.pos] <= '9' {
		p.pos++
		digits = true
	}
	return digits
}

func (p *pythonJSONParser) parseObject() (any, error) {
	object := ordObj{values: map[string]any{}}
	p.pos++ // '{'
	p.skipWhitespace()
	if p.pos < len(p.doc) && p.doc[p.pos] == '}' {
		p.pos++
		return object, nil
	}
	for {
		p.skipWhitespace()
		if p.pos >= len(p.doc) || p.doc[p.pos] != '"' {
			return nil, p.errorAt(min(p.pos, len(p.doc)), "Expecting property name enclosed in double quotes")
		}
		key, err := p.parseString()
		if err != nil {
			return nil, err
		}
		p.skipWhitespace()
		if p.pos >= len(p.doc) || p.doc[p.pos] != ':' {
			return nil, p.errorAt(min(p.pos, len(p.doc)), "Expecting ':' delimiter")
		}
		p.pos++
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if p.unique && object.Has(key) {
			return nil, fmt.Errorf("duplicate JSON field: %s", key)
		}
		object.Set(key, value)
		p.skipWhitespace()
		if p.pos >= len(p.doc) {
			return nil, p.errorAt(p.pos, "Expecting ',' delimiter")
		}
		switch p.doc[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return object, nil
		default:
			return nil, p.errorAt(p.pos, "Expecting ',' delimiter")
		}
	}
}

func (p *pythonJSONParser) parseArray() (any, error) {
	array := []any{}
	p.pos++ // '['
	p.skipWhitespace()
	if p.pos < len(p.doc) && p.doc[p.pos] == ']' {
		p.pos++
		return array, nil
	}
	for {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		array = append(array, value)
		p.skipWhitespace()
		if p.pos >= len(p.doc) {
			return nil, p.errorAt(p.pos, "Expecting ',' delimiter")
		}
		switch p.doc[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return array, nil
		default:
			return nil, p.errorAt(p.pos, "Expecting ',' delimiter")
		}
	}
}

func (p *pythonJSONParser) parseString() (string, error) {
	start := p.pos
	p.pos++ // opening quote
	var body []byte
	for {
		if p.pos >= len(p.doc) {
			return "", p.errorAt(start, "Unterminated string starting at")
		}
		c := p.doc[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(body), nil
		case c == '\\':
			p.pos++
			if p.pos >= len(p.doc) {
				return "", p.errorAt(start, "Unterminated string starting at")
			}
			switch escape := p.doc[p.pos]; escape {
			case '"', '\\', '/':
				body = append(body, escape)
			case 'b':
				body = append(body, '\b')
			case 'f':
				body = append(body, '\f')
			case 'n':
				body = append(body, '\n')
			case 'r':
				body = append(body, '\r')
			case 't':
				body = append(body, '\t')
			case 'u':
				p.pos++
				run, err := p.parseHex4()
				if err != nil {
					return "", err
				}
				// A high surrogate is only usable as the first half of a pair,
				// which is also the only shape CPython combines.
				if run >= 0xD800 && run <= 0xDBFF && p.pos+1 < len(p.doc) &&
					p.doc[p.pos] == '\\' && p.doc[p.pos+1] == 'u' {
					save := p.pos
					p.pos += 2
					low, err := p.parseHex4()
					if err == nil && low >= 0xDC00 && low <= 0xDFFF {
						run = 0x10000 + (run-0xD800)<<10 + (low - 0xDC00)
					} else {
						p.pos = save
					}
				}
				body = append(body, []byte(string(rune(run)))...)
				continue
			default:
				return "", p.errorAt(p.pos-1, "Invalid \\escape")
			}
			p.pos++
		case c < 0x20:
			return "", p.errorAt(p.pos, "Invalid control character at")
		default:
			body = append(body, c)
			p.pos++
		}
	}
}

func (p *pythonJSONParser) parseHex4() (int, error) {
	if p.pos+4 > len(p.doc) {
		return 0, p.errorAt(p.pos, "Unterminated string starting at")
	}
	value := 0
	for i := 0; i < 4; i++ {
		c := p.doc[p.pos+i]
		value <<= 4
		switch {
		case c >= '0' && c <= '9':
			value += int(c - '0')
		case c >= 'a' && c <= 'f':
			value += int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			value += int(c-'A') + 10
		default:
			return 0, p.errorAt(p.pos, "Invalid \\escape")
		}
	}
	p.pos += 4
	return value, nil
}

// canonicalValue rewrites ordered objects as plain maps for the canonical
// encoder. The canonical contract sorts keys, so an ordered object and its map
// form encode to identical bytes; this keeps the C3 encoder itself untouched
// (its accepted input types are exactly what they were) while letting a value
// cross from the ordered contract into the canonical one unchanged.
func canonicalValue(value any) any {
	switch typed := value.(type) {
	case ordObj:
		out := make(map[string]any, typed.Len())
		for _, key := range typed.Keys() {
			item, _ := typed.Get(key)
			out[key] = canonicalValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = canonicalValue(item)
		}
		return out
	case []ordObj:
		// The transport's interaction log is a []ordObj; it crosses the same way.
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = canonicalValue(item)
		}
		return out
	}
	return value
}

// writeNativeJSON lands one native evidence file through the supervisor's
// single atomic writer (temp file, fsync, rename; supervise.go stays untouched
// and keeps its own unexported writeAtomicJSON). Ordered objects cross into
// the canonical contract through canonicalValue, whose key sorting keeps the
// bytes identical to the Python reference's atomic() output.
func writeNativeJSON(path string, value any) error {
	return writeAtomicJSON(path, canonicalValue(value))
}
