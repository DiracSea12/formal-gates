// Package dogfood owns framework-independent dogfood-v1 business contracts.
// It never schedules work or writes a second run state.
package dogfood

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

const Version = 1
const Profile = "dogfood-v1"
const Baseline = "517de91f1b480ec969bd91385c29ce38d8bc2841"

type Check struct {
	ID           string   `json:"id"`
	Argv         []string `json:"argv"`
	Dependencies []string `json:"dependencies"`
}
type Spec struct {
	Version           int      `json:"version"`
	Project           string   `json:"project"`
	Base              string   `json:"base"`
	RequirementDigest string   `json:"requirement_digest"`
	Checks            []Check  `json:"checks"`
	Gates             []string `json:"gates"`
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	ReasoningEffort   string   `json:"reasoning_effort"`
	RepairLimit       int      `json:"repair_limit"`
}
type Finding struct {
	ID       string   `json:"id"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Paths    []string `json:"paths"`
}
type WorkerResult struct {
	Status   string    `json:"status"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
	CheckIDs []string  `json:"check_ids"`
}
type CheckResult struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Candidate     string `json:"candidate"`
	Binding       string `json:"binding"`
	ExitCode      int    `json:"exit_code"`
	Evidence      string `json:"evidence"`
	InheritedFrom string `json:"inherited_from,omitempty"`
}
type GateResult struct {
	ID        string       `json:"id"`
	Candidate string       `json:"candidate"`
	Revision  string       `json:"revision"`
	Evidence  string       `json:"evidence"`
	Result    WorkerResult `json:"result"`
}

func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func validID(s string) bool {
	if s == "" || len(s) > 96 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func safeDependency(s string) bool {
	return s != "" && s != "." && !strings.Contains(s, "\\") && !strings.HasPrefix(s, "/") && path.Clean(s) == s && s != ".." && !strings.HasPrefix(s, "../") && !strings.ContainsAny(s, "*?[]\x00\n\r")
}
func ValidateSpec(s Spec) error {
	if s.Version != Version {
		return fmt.Errorf("unsupported dogfood contract version %d", s.Version)
	}
	if s.Project == "" || s.Base == "" || len(s.RequirementDigest) != 64 {
		return fmt.Errorf("project, base and exact requirement digest are required")
	}
	if _, e := hex.DecodeString(s.RequirementDigest); e != nil {
		return fmt.Errorf("invalid requirement digest")
	}
	if s.Provider != "dsh" && s.Provider != "fixture" {
		return fmt.Errorf("unsupported provider %q", s.Provider)
	}
	if s.RepairLimit < 1 || s.RepairLimit > 100 {
		return fmt.Errorf("repair_limit must be 1..100; exhaustion asks the user, never grants PASS")
	}
	if len(s.Checks) == 0 {
		return fmt.Errorf("at least one explicitly authorized check is required")
	}
	seen := map[string]bool{}
	for _, c := range s.Checks {
		if !validID(c.ID) || seen[c.ID] {
			return fmt.Errorf("invalid or duplicate check id %q", c.ID)
		}
		seen[c.ID] = true
		if len(c.Argv) == 0 || strings.TrimSpace(c.Argv[0]) == "" {
			return fmt.Errorf("check %s needs argv", c.ID)
		}
		for _, a := range c.Argv {
			if strings.ContainsRune(a, '\x00') {
				return fmt.Errorf("NUL in command")
			}
		}
		ds := map[string]bool{}
		for _, d := range c.Dependencies {
			if !safeDependency(d) || ds[d] {
				return fmt.Errorf("invalid or duplicate dependency %q", d)
			}
			ds[d] = true
		}
	}
	seen = map[string]bool{}
	for _, g := range s.Gates {
		if (g != "complexity-gate" && g != "implementation-quality-gate") || seen[g] {
			return fmt.Errorf("unknown or duplicate gate %q", g)
		}
		seen[g] = true
	}
	if len(s.Gates) == 0 {
		return fmt.Errorf("dogfood-v1 requires an independent gate")
	}
	return nil
}
func ValidateWorker(role string, r WorkerResult, checks []Check) error {
	if r.Findings == nil || r.CheckIDs == nil {
		return fmt.Errorf("findings and check_ids must be explicit arrays")
	}
	roles := map[string]bool{"product-review": true, "technical-review": true, "development": true, "repair": true, "qa-design": true, "qa-review": true, "complexity-gate": true, "implementation-quality-gate": true}
	if !roles[role] {
		return fmt.Errorf("unknown role %q", role)
	}
	switch r.Status {
	case "PASS", "FAIL", "BLOCKED":
	case "CONTINUE":
		if role != "development" && role != "repair" {
			return fmt.Errorf("CONTINUE is only a development continuation")
		}
	default:
		return fmt.Errorf("invalid worker status %q", r.Status)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return fmt.Errorf("worker result needs a summary")
	}
	seen := map[string]bool{}
	blocking := false
	for _, f := range r.Findings {
		if !validID(f.ID) || seen[f.ID] || strings.TrimSpace(f.Message) == "" {
			return fmt.Errorf("invalid or duplicate finding")
		}
		seen[f.ID] = true
		switch f.Severity {
		case "P0", "P1":
			blocking = true
		case "P2", "P3":
		default:
			return fmt.Errorf("unknown severity %q", f.Severity)
		}
	}
	if r.Status == "PASS" && blocking {
		return fmt.Errorf("PASS cannot contain P0/P1")
	}
	if r.Status == "FAIL" && len(r.Findings) == 0 {
		return fmt.Errorf("FAIL needs concrete findings; runtime errors are BLOCKED")
	}
	known := map[string]bool{}
	for _, c := range checks {
		known[c.ID] = true
	}
	seen = map[string]bool{}
	for _, id := range r.CheckIDs {
		if !known[id] || seen[id] {
			return fmt.Errorf("unknown or duplicate check %q", id)
		}
		seen[id] = true
	}
	if (role == "qa-design" || role == "qa-review") && r.Status == "PASS" && len(seen) != len(known) {
		return fmt.Errorf("approved design must account for every authorized check")
	}
	return nil
}
func NeedsRepair(r WorkerResult) bool {
	if r.Status != "PASS" {
		return true
	}
	for _, f := range r.Findings {
		if f.Severity != "P3" {
			return true
		}
	}
	return false
}
func StrictDecode(b []byte, dst any) error {
	scan := json.NewDecoder(bytes.NewReader(b))
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
func WorkerSchema() map[string]any {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	arr := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	finding := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "severity", "message", "paths"}, "properties": map[string]any{"id": str(), "severity": map[string]any{"type": "string", "enum": []string{"P0", "P1", "P2", "P3"}}, "message": str(), "paths": arr(str())}}
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"status", "summary", "findings", "check_ids"}, "properties": map[string]any{"status": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL", "BLOCKED", "CONTINUE"}}, "summary": str(), "findings": arr(finding), "check_ids": arr(str())}}
}
