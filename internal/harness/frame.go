package harness

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
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

	value, err := decodeUnique(body)
	if err == nil {
		return asObject(value)
	}
	if refusal, ok := refusalFor(err); ok {
		return nil, refusal
	}

	// The text is not a single JSON document. Accept one fenced block, or one
	// bare object embedded in prose, but never choose between competitors.
	// Every check below is scoped to the fragment actually being parsed: prose
	// that merely mentions NaN or Infinity is not itself a value.
	var outside string
	if fences := fenceBlocks(body); len(fences) == 0 && !strings.Contains(body, "```") {
		start := bareObjectStart(body)
		if start < 0 {
			return nil, refusalPrefix + "no unambiguous result object"
		}
		fragment, length, err := rawDecode(body[start:])
		if err != nil {
			if refusal, ok := refusalFor(err); ok {
				return nil, refusal
			}
			return nil, refusalPrefix + "invalid result object"
		}
		value = fragment
		outside = body[:start] + body[start+length:]
	} else {
		if len(fences) != 1 || strings.Count(body, "```") != 2 {
			return nil, refusalPrefix + "final message is not one unambiguous JSON object"
		}
		value, err = decodeUnique(fences[0].inner)
		if err != nil {
			if refusal, ok := refusalFor(err); ok {
				return nil, refusal
			}
			return nil, refusalPrefix + "invalid JSON result fence"
		}
		outside = body[:fences[0].start] + body[fences[0].end:]
	}
	if hasJSONStructure(outside) {
		return nil, refusalPrefix + "competing structured content outside result"
	}
	return asObject(value)
}

// Only JSON structure competes with the result; ordinary non-JSON prose may
// contain delimiters. Typed refusals and unfinished structures still compete.
func hasJSONStructure(text string) bool {
	for i, ch := range text {
		if ch != '{' && ch != '[' {
			continue
		}
		// An opening attached to an identifier or closed expression belongs to
		// that same text token, rather than starting an independent JSON value.
		if i > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:i])
			if unicode.IsLetter(previous) || unicode.IsNumber(previous) || unicode.IsMark(previous) || strings.ContainsRune("_.)]}", previous) {
				continue
			}
		}
		_, _, err := rawDecode(text[i:])
		_, typed := refusalFor(err)
		if err == nil || typed || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return true
		}
		decoder := newDecoder(text[i:])
		if _, err := decoder.Token(); err != nil {
			continue
		}
		_, err = decoder.Token()
		var syntax *json.SyntaxError
		// A member token or a syntax error after consuming its first byte
		// proves JSON has started; invalid non-JSON content fails at entry.
		if err == nil || errors.As(err, &syntax) && syntax.Offset > decoder.InputOffset()+1 {
			return true
		}
	}
	return false
}

const refusalPrefix = "NATIVE_RESULT: "

func refusalDuplicate(key string) string { return refusalPrefix + "duplicate JSON field " + key }
func refusalNonFinite(lit string) string { return refusalPrefix + "non-finite number " + lit }

// refusalFor maps a typed framing error to its refusal text. Returning false
// means "not one of the typed refusals", which sends the caller down the
// fallback path.
func refusalFor(err error) (string, bool) {
	switch typed := err.(type) {
	case *duplicateFieldError:
		return refusalDuplicate(typed.key), true
	case *nonFiniteError:
		return refusalNonFinite(typed.literal), true
	}
	return "", false
}

func asObject(value any) (map[string]any, string) {
	if object, ok := value.(map[string]any); ok {
		return object, ""
	}
	return nil, refusalPrefix + "final result must be an object"
}

type duplicateFieldError struct{ key string }

func (e *duplicateFieldError) Error() string { return "duplicate JSON field " + e.key }

type nonFiniteError struct{ literal string }

func (e *nonFiniteError) Error() string { return "non-finite number " + e.literal }

// decodeUnique parses one JSON document, refusing repeated object keys.
//
// A repeated key is reported only once its object closes, which is when a
// streaming parser would have completed that object.
func decodeUnique(text string) (any, error) {
	if err := scanUnique(text); err != nil {
		return nil, err
	}
	decoder := newDecoder(text)
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func newDecoder(text string) *json.Decoder {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	return decoder
}

type framingError string

func (e framingError) Error() string { return string(e) }

const (
	errExactlyOneDocument = framingError("exactly one JSON document is required")
	errInvalidObjectKey   = framingError("invalid JSON object key")
)

// scanUnique parses one document and requires that nothing follows it.
func scanUnique(text string) error {
	decoder := newDecoder(text)
	if err := walkTokens(decoder, text); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errExactlyOneDocument
	}
	return nil
}

// walkTokens consumes exactly one JSON value, reporting repeated object keys and
// non-finite literals. It does not require the remainder of the text to be
// empty; callers decide what may follow.
func walkTokens(decoder *json.Decoder, text string) error {
	var visit func() error
	visit = func() error {
		token, err := decoder.Token()
		if err != nil {
			return classify(text, decoder, err)
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			// Tracked separately from the key itself: an empty key is a legal
			// JSON key, so the empty string cannot double as "no duplicate".
			repeated := ""
			found := false
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return classify(text, decoder, err)
				}
				name, ok := key.(string)
				if !ok {
					return errInvalidObjectKey
				}
				if seen[name] && !found {
					repeated, found = name, true
				}
				seen[name] = true
				if err := visit(); err != nil {
					return err
				}
			}
			if _, err := decoder.Token(); err != nil {
				return classify(text, decoder, err)
			}
			if found {
				return &duplicateFieldError{key: repeated}
			}
			return nil
		case '[':
			for decoder.More() {
				if err := visit(); err != nil {
					return err
				}
			}
			if _, err := decoder.Token(); err != nil {
				return classify(text, decoder, err)
			}
			return nil
		}
		return nil
	}
	return visit()
}

// classify turns a decoder error into a typed refusal when the text at the
// decoder's current position is a non-finite literal.
//
// The check is deliberately scoped to the text handed to this walk. An earlier
// revision scanned the whole message up front, which refused legitimate results
// whose surrounding prose merely mentioned "NaN" or "Infinity".
func classify(text string, decoder *json.Decoder, err error) error {
	offset := int(decoder.InputOffset())
	if offset < 0 || offset > len(text) {
		return err
	}
	rest := strings.TrimLeft(text[offset:], " \t\r\n")
	switch {
	case strings.HasPrefix(rest, "-Infinity"):
		return &nonFiniteError{literal: "-Infinity"}
	case strings.HasPrefix(rest, "Infinity"):
		return &nonFiniteError{literal: "Infinity"}
	case strings.HasPrefix(rest, "NaN"):
		return &nonFiniteError{literal: "NaN"}
	}
	return err
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

// rawDecode reads one JSON value from the start of text and reports how many
// bytes it consumed, so a caller can account for the text around it.
func rawDecode(text string) (any, int, error) {
	decoder := newDecoder(text)
	if err := walkTokens(decoder, text); err != nil {
		return nil, 0, err
	}
	length := int(decoder.InputOffset())
	if length > len(text) {
		length = len(text)
	}
	value, err := decodeUnique(strings.TrimSpace(text[:length]))
	if err != nil {
		return nil, 0, err
	}
	return value, length, nil
}
