package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// CanonicalJSON reproduces the harness's canonical JSON encoding byte for byte:
//
//	json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
//
// This is the content-addressing format: artifact references, operation ids,
// evidence digests and job input digests are all derived from these bytes, so
// the encoding is a contract rather than an implementation detail. Go's default
// encoder is not usable here - it html-escapes <, > and & and leaves the
// canonical number formatting to strconv - so the encoding is written out
// explicitly below.
func CanonicalJSON(value any) ([]byte, error) {
	var out strings.Builder
	if err := encodeCanonical(value, &out); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// CanonicalDigest is the SHA-256 of CanonicalJSON, in lower-case hex.
func CanonicalDigest(value any) (string, error) {
	encoded, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func encodeCanonical(value any, out *strings.Builder) error {
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
		out.WriteString(strconv.Itoa(typed))
	case int64:
		out.WriteString(strconv.FormatInt(typed, 10))
	case float64:
		out.WriteString(pythonFloatRepr(typed))
	case string:
		writeCanonicalString(typed, out)
	case []any:
		out.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := encodeCanonical(item, out); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		// UTF-8 byte order equals code point order, which is what Python's
		// sort_keys does for str keys.
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeCanonicalString(key, out)
			out.WriteByte(':')
			if err := encodeCanonical(typed[key], out); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("canonical json: unsupported value type %T", value)
	}
	return nil
}

// canonicalNumber renders a JSON number literal the way Python would after
// parsing it: integer literals become ints (so -0 becomes 0 and arbitrary
// precision survives), everything else becomes a float.
func canonicalNumber(literal string) (string, error) {
	if !strings.ContainsAny(literal, ".eE") {
		integer, ok := new(big.Int).SetString(literal, 10)
		if !ok {
			return "", fmt.Errorf("canonical json: invalid integer literal %q", literal)
		}
		return integer.String(), nil
	}
	value, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		// Python's json parser keeps an overflowed literal as +/-Inf and
		// json.dumps then renders it as the bare token Infinity/-Infinity.
		// strconv reports the same value together with ErrRange, so the range
		// error is not a refusal here - only a genuinely malformed literal is.
		var numberError *strconv.NumError
		if !errors.As(err, &numberError) || !errors.Is(numberError.Err, strconv.ErrRange) {
			return "", fmt.Errorf("canonical json: invalid number literal %q: %w", literal, err)
		}
	}
	return pythonFloatRepr(value), nil
}

// pythonFloatRepr reproduces CPython's repr(float), which json.dumps uses.
//
// The rule is: if the decimal exponent is in [-4, 16) use fixed notation with
// at least one fractional digit, otherwise scientific notation with a two-digit
// signed exponent. strconv's own thresholds and exponent widths differ, so the
// shortest digits are taken from strconv and the shape is rebuilt here.
func pythonFloatRepr(value float64) string {
	switch {
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	case math.IsNaN(value):
		return "NaN"
	}
	shortest := strconv.FormatFloat(value, 'e', -1, 64)
	negative := strings.HasPrefix(shortest, "-")
	if negative {
		shortest = shortest[1:]
	}
	split := strings.IndexByte(shortest, 'e')
	if split < 0 {
		return shortest
	}
	digits := strings.Replace(shortest[:split], ".", "", 1)
	exponent, err := strconv.Atoi(shortest[split+1:])
	if err != nil {
		return shortest
	}
	var rendered string
	if exponent >= -4 && exponent < 16 {
		switch {
		case exponent >= 0 && len(digits) <= exponent+1:
			rendered = digits + strings.Repeat("0", exponent+1-len(digits)) + ".0"
		case exponent >= 0:
			rendered = digits[:exponent+1] + "." + digits[exponent+1:]
		default:
			rendered = "0." + strings.Repeat("0", -exponent-1) + digits
		}
	} else {
		mantissa := digits[:1]
		if len(digits) > 1 {
			mantissa += "." + digits[1:]
		}
		sign := "+"
		if exponent < 0 {
			sign = "-"
			exponent = -exponent
		}
		rendered = fmt.Sprintf("%se%s%02d", mantissa, sign, exponent)
	}
	if negative {
		return "-" + rendered
	}
	return rendered
}

// writeCanonicalString escapes exactly the code points Python's json.dumps
// escapes with ensure_ascii=False: the two structural characters, the five
// short control escapes, and every other control character as \u00xx.
func writeCanonicalString(text string, out *strings.Builder) {
	out.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			switch {
			case r < 0x20:
				fmt.Fprintf(out, `\u%04x`, r)
			default:
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

// canonicalBody applies JSON tags before canonical encoding.
func canonicalBody(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	plain, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	return CanonicalJSON(plain)
}
