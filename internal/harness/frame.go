package harness

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// FrameResult accepts exactly one JSON object out of a model's final message,
// or refuses with a reason. It performs no I/O and reads no clock or randomness.
//
// This is the framing question only: "is there exactly one unambiguous result
// object here?". Whether the object is an acceptable role result is a different
// contract, owned by ValidateWorker.
//
// The refusal strings are observable behaviour: existing tests assert them and
// they surface in user-facing reports, so they are reproduced verbatim.
func FrameResult(text string) (map[string]any, string) {
	body := strings.TrimSpace(text)
	if literal := firstNonFinite(body); literal != "" {
		return nil, refusalNonFinite(literal)
	}
	value, err := decodeUnique(body)
	if err == nil {
		return asObject(value)
	}
	if typed, ok := err.(*duplicateFieldError); ok {
		return nil, refusalDuplicate(typed.key)
	}

	// The text is not a single JSON document. Accept one fenced block, or one
	// bare object embedded in prose, but never choose between competitors.
	var outside string
	if fences := fenceBlocks(body); len(fences) == 0 && !strings.Contains(body, "```") {
		start := bareObjectStart(body)
		if start < 0 {
			return nil, refusalPrefix + "no unambiguous result object"
		}
		head, length, err := rawDecode(body[start:])
		if err != nil {
			return nil, refusalPrefix + "invalid result object"
		}
		value, err = decodeUnique(strings.TrimSpace(string(head)))
		if err != nil {
			if typed, ok := err.(*duplicateFieldError); ok {
				return nil, refusalDuplicate(typed.key)
			}
			return nil, refusalPrefix + "invalid result object"
		}
		outside = body[:start] + body[start+length:]
	} else {
		if len(fences) != 1 || strings.Count(body, "```") != 2 {
			return nil, refusalPrefix + "final message is not one unambiguous JSON object"
		}
		value, err = decodeUnique(fences[0].inner)
		if err != nil {
			if typed, ok := err.(*duplicateFieldError); ok {
				return nil, refusalDuplicate(typed.key)
			}
			return nil, refusalPrefix + "invalid JSON result fence"
		}
		outside = body[:fences[0].start] + body[fences[0].end:]
	}
	if strings.ContainsAny(outside, "{}[]") {
		return nil, refusalPrefix + "competing structured content outside result"
	}
	return asObject(value)
}

const refusalPrefix = "NATIVE_RESULT: "

func refusalDuplicate(key string) string { return refusalPrefix + "duplicate JSON field " + key }
func refusalNonFinite(lit string) string { return refusalPrefix + "non-finite number " + lit }

func asObject(value any) (map[string]any, string) {
	if object, ok := value.(map[string]any); ok {
		return object, ""
	}
	return nil, refusalPrefix + "final result must be an object"
}

type duplicateFieldError struct{ key string }

func (e *duplicateFieldError) Error() string { return "duplicate JSON field " + e.key }

// decodeUnique parses one JSON document, refusing repeated object keys.
//
// A repeated key is reported only once its object closes, which is when a
// streaming parser would have completed that object.
func decodeUnique(text string) (any, error) {
	if err := scanUnique(text); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func scanUnique(text string) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var visit func() error
	visit = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			repeated := ""
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok {
					return &duplicateFieldError{key: ""}
				}
				if seen[name] && repeated == "" {
					repeated = name
				}
				seen[name] = true
				if err := visit(); err != nil {
					return err
				}
			}
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if repeated != "" {
				return &duplicateFieldError{key: repeated}
			}
			return nil
		case '[':
			for decoder.More() {
				if err := visit(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		}
		return nil
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errExactlyOneDocument
	}
	return nil
}

type framingError string

func (e framingError) Error() string { return string(e) }

const errExactlyOneDocument = framingError("exactly one JSON document is required")

// firstNonFinite reports the first NaN/Infinity/-Infinity that appears where a
// value may begin, ignoring string contents. Go's decoder would report these as
// generic syntax errors; the refusal has to name the literal instead.
func firstNonFinite(text string) string {
	inString := false
	escaped := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if !valueMayStartAt(text, i) {
			continue
		}
		switch {
		case strings.HasPrefix(text[i:], "-Infinity"):
			return "-Infinity"
		case strings.HasPrefix(text[i:], "Infinity"):
			return "Infinity"
		case strings.HasPrefix(text[i:], "NaN"):
			return "NaN"
		}
	}
	return ""
}

// valueMayStartAt reports whether the previous non-space byte allows a value to
// begin here.
func valueMayStartAt(text string, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch text[j] {
		case ' ', '\t', '\r', '\n':
			continue
		case ':', ',', '[':
			return true
		default:
			return false
		}
	}
	return true
}

var fencePattern = regexp.MustCompile("(?ims)^```(?:json)?[ \t]*\r?\n(.*?)^```[ \t]*(?:\r?\n|\\z)")
var bareObjectPattern = regexp.MustCompile("(?m)^[ \t]*\\{")

type fenceBlock struct {
	inner string
	start int
	end   int
}

func fenceBlocks(body string) []fenceBlock {
	matches := fencePattern.FindAllStringSubmatchIndex(body, -1)
	blocks := make([]fenceBlock, 0, len(matches))
	for _, m := range matches {
		blocks = append(blocks, fenceBlock{
			inner: body[m[2]:m[3]],
			start: m[0],
			end:   m[1],
		})
	}
	return blocks
}

func bareObjectStart(body string) int {
	loc := bareObjectPattern.FindStringIndex(body)
	if loc == nil {
		return -1
	}
	// The match spans leading indentation, so point at the brace itself.
	return loc[0] + strings.Index(body[loc[0]:loc[1]], "{")
}

// rawDecode reads one JSON document from the start of text and reports how many
// bytes it consumed.
func rawDecode(text string) (json.RawMessage, int, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, 0, err
	}
	return value, int(decoder.InputOffset()), nil
}
