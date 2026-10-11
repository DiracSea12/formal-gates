package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func StrictDecode(b []byte, dst any) error {
	scan := json.NewDecoder(bytes.NewReader(b))
	// The scanning pass only inspects object keys and delimiters, but it still
	// has to consume every token. Without UseNumber a numeric token is decoded
	// into a float64, so an integer literal outside float64's range fails here
	// before the target type is ever considered - which would reject values the
	// canonical encoder handles by design (arbitrary-precision integers).
	scan.UseNumber()
	var visit func() error
	visit = func() error {
		t, e := scan.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for scan.More() {
				k, e := scan.Token()
				if e != nil {
					return e
				}
				name, ok := k.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate/invalid JSON key %v", k)
				}
				seen[name] = true
				if e = visit(); e != nil {
					return e
				}
			}
		case '[':
			for scan.More() {
				if e = visit(); e != nil {
					return e
				}
			}
		}
		_, e = scan.Token()
		return e
	}
	if e := visit(); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return fmt.Errorf("exactly one JSON document is required")
	}
	return nil
}
