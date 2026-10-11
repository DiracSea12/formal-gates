package harness

// ZCode adapter tests that need no host process: the selection mechanism's
// refusal branches, the account overlay construction, the respond table, the
// error classification, the notification filter, materialize's event
// contract, reconcile eligibility and the routing dispatch. The app-server
// execute sequence over a scripted peer lives in native_zcode_unix_test.go.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// zcodePathHash is the sha256 of a path string, the revision's path hash.
func zcodePathHash(t *testing.T, path string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

// TestZcodeReference pins reference's branches (合同选型机制拒绝分支):
// empty is nil, malformed JSON, a non-object, a wrong key set, and the exact
// two-key object.
func TestZcodeReference(t *testing.T) {
	if ref, err := zcodeReference(""); err != nil || ref != nil {
		t.Fatalf("empty: %v %v", ref, err)
	}
	cases := []struct {
		value string
		want  string
	}{
		{"not json", "ZCODE_CONFIG: a bound selection must name a provider and a model"},
		{"[1]", "ZCODE_CONFIG: a bound selection must name exactly one provider and model"},
		{`{"providerId": "p"}`, "ZCODE_CONFIG: a bound selection must name exactly one provider and model"},
		{`{"providerId": "p", "modelId": "m", "extra": 1}`, "ZCODE_CONFIG: a bound selection must name exactly one provider and model"},
	}
	for _, tc := range cases {
		_, err := zcodeReference(tc.value)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("%s: err = %v, want %q", tc.value, err, tc.want)
		}
	}
	ref, err := zcodeReference(`{"modelId":"m","providerId":"p"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, ref); got != `{"modelId":"m","providerId":"p"}` {
		t.Fatalf("ref = %s", got)
	}
}

// TestZcodeSchemaForPrompt pins the prompt rendering's one presentation-order
// rule: canonical encoding with each object's additionalProperties key moved
// last (recursively; array order untouched), and the value unchanged. A value
// without the directive key is byte-identical to the canonical encoding.

// A directive inside an array element is reordered without touching
// the array's own order.

// The directive's own value is rendered as-is, whatever it is.

// zcodeTestSnapshot is one host snapshot with a current selection, an
// advertised list carrying reasoning levels, a thought level and a mode.
const zcodeTestSnapshot = `{
	"protocol": {"name": "ZCode Protocol", "version": 1},
	"session": {"sessionId": "s-1"},
	"settings": {
		"model": {
			"current": {"providerId": "p1", "modelId": "m1"},
			"available": [
				{"ref": {"providerId": "p1", "modelId": "m1"},
				 "reasoning": {"levels": [{"value": "high"}, {"value": "low"}], "defaultLevel": "low"}},
				{"ref": {"providerId": "p1", "modelId": "m2"}},
				{"ref": "not-a-ref"},
				"not-an-entry"
			]
		},
		"thoughtLevel": {"current": "low"},
		"mode": {"current": "default"}
	}
}`

// TestZcodeSelectionMechanism pins selected_model, selection,
// advertised_models, advertised_reasoning and bound_model (合同选型机制).
func TestZcodeSelectionMechanism(t *testing.T) {
	snapshot := decodeTestJSON(t, zcodeTestSnapshot)

	if got := mustCanonical(t, zcodeSelection(snapshot)); got !=
		`{"model":"{\"modelId\":\"m1\",\"providerId\":\"p1\"}","reasoning_effort":"low"}` {
		t.Fatalf("selection = %s", got)
	}
	// A host that reports no current selection stays absent, not filled in.
	empty := decodeTestJSON(t, `{"settings": {"model": {"current": null}, "thoughtLevel": {}}}`)
	if got := mustCanonical(t, zcodeSelection(empty)); got != `{"model":"","reasoning_effort":""}` {
		t.Fatalf("empty selection = %s", got)
	}
	// The reasoning effort is a level name or nothing: a current value that is
	// not a level name (here a number) expresses no preference, exactly like
	// an absent one.
	numeric := decodeTestJSON(t, `{"settings": {"thoughtLevel": {"current": 5}}}`)
	if got := mustCanonical(t, zcodeSelection(numeric)); got != `{"model":"","reasoning_effort":""}` {
		t.Fatalf("non-string level = %s", got)
	}
	// An incomplete current selection (no modelId) is no selection either.
	incomplete := decodeTestJSON(t, `{"settings": {"model": {"current": {"providerId": "p1"}}}}`)
	if ref := zcodeSelectedModel(incomplete); ref != nil {
		t.Fatalf("incomplete current = %v", ref)
	}

	advertised := zcodeAdvertisedModels(snapshot)
	want := []string{`{"modelId":"m1","providerId":"p1"}`, `{"modelId":"m2","providerId":"p1"}`}
	if len(advertised) != len(want) {
		t.Fatalf("advertised = %v", advertised)
	}
	for index, value := range want {
		if advertised[index] != value {
			t.Fatalf("advertised[%d] = %q, want %q", index, advertised[index], value)
		}
	}

	ref, err := zcodeReference(`{"providerId": "p1", "modelId": "m1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, zcodeAdvertisedReasoning(snapshot, ref)); got !=
		`{"default":"low","levels":["high","low"]}` {
		t.Fatalf("reasoning = %s", got)
	}
	// A default the levels do not contain falls back to the first level.
	staleDefault := decodeTestJSON(t, `{"settings": {"model": {"available": [
		{"ref": {"providerId": "p1", "modelId": "m1"},
		 "reasoning": {"levels": [{"value": "high"}, {"value": 5}, "not-a-level"], "defaultLevel": "gone"}}]}}}`)
	if got := mustCanonical(t, zcodeAdvertisedReasoning(staleDefault, ref)); got !=
		`{"default":"high","levels":["high"]}` {
		t.Fatalf("stale default = %s", got)
	}
	unknown, err := zcodeReference(`{"providerId": "p9", "modelId": "m9"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, zcodeAdvertisedReasoning(snapshot, unknown)); got != `{"default":"","levels":[]}` {
		t.Fatalf("unknown ref reasoning = %s", got)
	}

	// bound_model: the effort wins, else the advertised default; a model with
	// no levels is left exactly as bound.
	if got := mustCanonical(t, zcodeBoundModel(snapshot, ref, "")); got !=
		`{"modelId":"m1","options":{"reasoningLevel":"low"},"providerId":"p1"}` {
		t.Fatalf("bound default = %s", got)
	}
	if got := mustCanonical(t, zcodeBoundModel(snapshot, ref, "high")); got !=
		`{"modelId":"m1","options":{"reasoningLevel":"high"},"providerId":"p1"}` {
		t.Fatalf("bound effort = %s", got)
	}
	plain, err := zcodeReference(`{"providerId": "p1", "modelId": "m2"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, zcodeBoundModel(snapshot, plain, "")); got !=
		`{"modelId":"m2","providerId":"p1"}` {
		t.Fatalf("level-less bound = %s", got)
	}
	if zcodeBoundModel(snapshot, nil, "high") != nil {
		t.Fatal("no wanted model must stay nil")
	}
}

// TestZcodeBundledCatalogueAndEnvironment pins the catalogue candidates and
// the environment pair: a host that resolves its own catalogue keeps its
// behaviour, and a host that cannot gets both variables named together.
func TestZcodeBundledCatalogueAndEnvironment(t *testing.T) {
	t.Setenv("ZCODE_DATA_BASE_DIR", t.TempDir())
	// The script sits deep enough that the source-tree-relative candidate (five
	// directories up) still lands inside this test's own temp tree, so nothing
	// leaks into the shared temp area.
	base := t.TempDir()
	root := filepath.Join(base, "a", "b", "c", "app")
	script := filepath.Join(root, "glm", "zcode.cjs")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("// app-server\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No candidate anywhere and no Resources file: the host keeps its own
	// (failing) behaviour, so nothing is named.
	if got := zcodeBundledProviderCatalogue(script); got != "" {
		t.Fatalf("no candidate = %q", got)
	}
	if env := zcodeProviderEnvironment(script); env != nil {
		t.Fatalf("environment must be nil, got %v", env)
	}

	// The Resources layout the 3.12.3 bundle ships: named, and both provider
	// variables are set together.
	resources := filepath.Join(root, "config", "provider", "zcode-builtin.json")
	if err := os.MkdirAll(filepath.Dir(resources), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resources, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := zcodeBundledProviderCatalogue(script); got != resources {
		t.Fatalf("resources candidate = %q, want %q", got, resources)
	}
	env := zcodeProviderEnvironment(script)
	if env == nil || env[zcodeBuiltinCatalogueEnv] != resources || env[zcodePersonalConfigEnv] == "" {
		t.Fatalf("environment pair = %v", env)
	}

	// The host's own first candidate exists: keep the host's behaviour.
	hostCandidate := filepath.Join(root, "glm", "provider", "zcode-builtin.json")
	if err := os.MkdirAll(filepath.Dir(hostCandidate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostCandidate, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := zcodeBundledProviderCatalogue(script); got != "" {
		t.Fatalf("host candidate must win, got %q", got)
	}
	if env := zcodeProviderEnvironment(script); env != nil {
		t.Fatalf("environment must be nil, got %v", env)
	}

	// The source-tree-relative candidate wins as well.
	if err := os.Remove(hostCandidate); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resources); err != nil {
		t.Fatal(err)
	}
	sourceCandidate := filepath.Join(base, "config", "provider", "zcode-builtin.json")
	if err := os.MkdirAll(filepath.Dir(sourceCandidate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceCandidate, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := zcodeBundledProviderCatalogue(script); got != "" {
		t.Fatalf("source-tree candidate must win, got %q", got)
	}

	// The pinned catalogue is read back from the binding environment.
	if got := zcodePinnedCatalogue(map[string]any{"environment": map[string]any{
		zcodeBuiltinCatalogueEnv: "/tmp/elsewhere.json"}}); got != "/tmp/elsewhere.json" {
		t.Fatalf("pinned = %q", got)
	}
	if got := zcodePinnedCatalogue(map[string]any{}); got != "" {
		t.Fatalf("unpinned = %q", got)
	}
}

// zcodeTestCatalogue writes one catalogue fixture and returns its path.
func zcodeTestCatalogue(t *testing.T, revision string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "zcode-builtin.json")
	body := `{"revision": ` + revision + `, "config": {"providerConfigRules": {"providerRules": [
		{"providerId": "p1", "config": {"access": {"type": "zhipu-account", "accountType": "zhipu", "mode": "coding-plan"}}},
		{"providerId": "p2", "config": {"access": {"type": "zhipu-account", "accountType": "zhipu", "mode": "solo"}}},
		{"providerId": "p3", "config": {"access": {"type": "api-key"}}},
		"not-a-rule"
	]}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// zcodeTestStore writes the host credential store with the given key order.
// The values are opaque here and are never read by the adapter.
func zcodeTestStore(t *testing.T, keys []string) string {
	t.Helper()
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, ".zcode", "v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := &strings.Builder{}
	body.WriteString("{")
	for index, key := range keys {
		if index > 0 {
			body.WriteString(",")
		}
		body.WriteString(`"` + key + `":"cipher-text"`)
	}
	body.WriteString("}")
	if err := os.WriteFile(filepath.Join(data, ".zcode", "v2", "credentials.json"),
		[]byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

// TestZcodeBuiltinProviderRevision pins the revision format: the catalogue's
// own revision plus the sha256 of the resolved path, never of the contents.
func TestZcodeBuiltinProviderRevision(t *testing.T) {
	catalogue := zcodeTestCatalogue(t, "7")
	resolved, err := filepath.Abs(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := zcodeBuiltinProviderRevision(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	want := "zcode-builtin:7:" + zcodePathHash(t, resolved)
	if revision != want {
		t.Fatalf("revision = %q, want %q", revision, want)
	}
	// A string revision is kept verbatim.
	textCatalogue := zcodeTestCatalogue(t, `"r-nine"`)
	if revision, err = zcodeBuiltinProviderRevision(textCatalogue); err != nil ||
		!strings.HasPrefix(revision, "zcode-builtin:r-nine:") {
		t.Fatalf("string revision = %q %v", revision, err)
	}
	// The same contents at another path are another revision.
	other := filepath.Join(t.TempDir(), "copy.json")
	if err := os.WriteFile(other, readTestFileBytes(t, catalogue), 0o644); err != nil {
		t.Fatal(err)
	}
	otherRevision, err := zcodeBuiltinProviderRevision(other)
	if err != nil {
		t.Fatal(err)
	}
	if otherRevision == want {
		t.Fatal("the path hash must distinguish equal contents")
	}

	if _, err := zcodeBuiltinProviderRevision(filepath.Join(t.TempDir(), "absent.json")); err == nil ||
		!strings.HasPrefix(err.Error(), "ZCODE_ACCOUNT_CONFIG: cannot read the named provider catalogue ") {
		t.Fatalf("absent catalogue: %v", err)
	}
	noRevision := filepath.Join(t.TempDir(), "no-revision.json")
	if err := os.WriteFile(noRevision, []byte(`{"config": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := zcodeBuiltinProviderRevision(noRevision); err == nil ||
		!strings.HasPrefix(err.Error(), "ZCODE_ACCOUNT_CONFIG: the named provider catalogue has no revision: ") {
		t.Fatalf("no revision: %v", err)
	}
}

// TestZcodeAccountRulesAndProvisioning pins the catalogue rules, the store's
// key order (the overlay's provider order and its revision join both depend
// on it), the identity separator, and the refusal texts.
func TestZcodeAccountRulesAndProvisioning(t *testing.T) {
	catalogue := zcodeTestCatalogue(t, "7")
	rules, err := zcodeCatalogueAccountRules(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules = %v", rules)
	}
	if _, ok := rules["p1"]; !ok {
		t.Fatalf("p1 must be declared: %v", rules)
	}

	data := zcodeTestStore(t, []string{
		"account-provider:coding-plan:p2:account:u2:api-key",
		"unrelated-key",
		"account-provider:coding-plan:p1:account:u1:api-key",
		"account-provider:coding-plan:p1:account:u1:refresh-token",
		"account-provider:coding-plan:p9:account:u9:api-key",
	})
	t.Setenv("ZCODE_DATA_BASE_DIR", data)
	provisioned, err := zcodeProvisionedAccountProviders(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	// Document order, api-key shape only, catalogue-declared only, deduped.
	if len(provisioned) != 2 || provisioned[0] != "p2" || provisioned[1] != "p1" {
		t.Fatalf("provisioned = %v", provisioned)
	}

	// No store at all: empty, never a refusal.
	t.Setenv("ZCODE_DATA_BASE_DIR", t.TempDir())
	if provisioned, err = zcodeProvisionedAccountProviders(catalogue); err != nil || len(provisioned) != 0 {
		t.Fatalf("absent store = %v %v", provisioned, err)
	}

	// An unreadable store is a typed refusal.
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, ".zcode", "v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, ".zcode", "v2", "credentials.json"),
		[]byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZCODE_DATA_BASE_DIR", broken)
	_, err = zcodeProvisionedAccountProviders(catalogue)
	if err == nil || err.Error() != "ZCODE_ACCOUNT_CONFIG: cannot read the host credential store "+
		filepath.Join(broken, ".zcode", "v2", "credentials.json") {
		t.Fatalf("broken store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(broken, ".zcode", "v2", "credentials.json"),
		[]byte("[1]"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = zcodeProvisionedAccountProviders(catalogue)
	if err == nil || err.Error() != "ZCODE_ACCOUNT_CONFIG: the host credential store is not an index: "+
		filepath.Join(broken, ".zcode", "v2", "credentials.json") {
		t.Fatalf("non-index store: %v", err)
	}
}

// TestZcodeAccountProviderConfiguration pins the overlay: the three ways a
// current provider is chosen and the exact configuration shape.
func TestZcodeAccountProviderConfiguration(t *testing.T) {
	catalogue := zcodeTestCatalogue(t, "7")
	data := zcodeTestStore(t, []string{
		"account-provider:coding-plan:p2:account:u2:api-key",
		"account-provider:coding-plan:p1:account:u1:api-key",
	})
	t.Setenv("ZCODE_DATA_BASE_DIR", data)
	resolved, err := filepath.Abs(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	base := "zcode-builtin:7:" + zcodePathHash(t, resolved)

	// No request, no host plan: the store's own order.
	overlay, err := zcodeAccountProviderConfiguration(catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if getOf(overlay, "current") != "p2" || getOf(overlay, "current_source") != "provisioning-order" {
		t.Fatalf("provisioning-order = %v", overlay)
	}
	want := `{"based_on":"` + base + `","configuration":{"basedOnZCodeBuiltinRevision":"` + base +
		`","providers":{"p1":{"access":{"entitled":true,"type":"zhipu-account"}},"p2":{"access":{"entitled":true,"type":"zhipu-account"}}},` +
		`"revision":"account-config:` + base + `:p2,p1","states":{"p1":{"availability":"available","current":false,"entitled":true},` +
		`"p2":{"availability":"available","current":true,"entitled":true}}},"current":"p2","current_source":"provisioning-order",` +
		`"providers":["p2","p1"]}`
	if got := mustCanonical(t, overlay); got != want {
		t.Fatalf("overlay:\n got %s\nwant %s", got, want)
	}

	// The requested provider wins when it is provisioned.
	wanted, err := zcodeReference(`{"providerId": "p1", "modelId": "m1"}`)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err = zcodeAccountProviderConfiguration(catalogue, wanted)
	if err != nil {
		t.Fatal(err)
	}
	if getOf(overlay, "current") != "p1" || getOf(overlay, "current_source") != "requested" {
		t.Fatalf("requested = %v", overlay)
	}
	// A provider the host has not provisioned is not chosen.
	other, err := zcodeReference(`{"providerId": "p3", "modelId": "m1"}`)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err = zcodeAccountProviderConfiguration(catalogue, other)
	if err != nil {
		t.Fatal(err)
	}
	if getOf(overlay, "current") != "p2" {
		t.Fatalf("unprovisioned request = %v", overlay)
	}

	// The host's own plan selection: family and kind, never the id spelling.
	if err := os.WriteFile(filepath.Join(data, ".zcode", "v2", "setting.json"),
		[]byte(`{"providerFamilyDomain": "zhipu", "providerFamilyConnectionSelections": {"zhipu": {"kind": "coding-plan"}}}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	overlay, err = zcodeAccountProviderConfiguration(catalogue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if getOf(overlay, "current") != "p1" || getOf(overlay, "current_source") != "host-selected-plan" {
		t.Fatalf("host-selected-plan = %v", overlay)
	}

	// A host with nothing provisioned stays empty.
	t.Setenv("ZCODE_DATA_BASE_DIR", t.TempDir())
	overlay, err = zcodeAccountProviderConfiguration(catalogue, nil)
	if err != nil || overlay != nil {
		t.Fatalf("empty host = %v %v", overlay, err)
	}
}

// zcodeTestResponse is the native answer text of a completed turn.
const zcodeTestResponse = `{"status": "PASS", "summary": "observed"}`

// zcodeTestResponseJSON is zcodeTestResponse as a JSON string value.
const zcodeTestResponseJSON = `"{\"status\": \"PASS\", \"summary\": \"observed\"}"`

// zcodeSuccessEvents is one completed turn's evidence for inputId in-1.
func zcodeSuccessEvents() []string {
	return []string{
		`{"sessionId": "s-1", "seq": 1, "type": "turn.started", "turnId": "t-1", "payload": {"inputId": "in-1"}}`,
		`{"sessionId": "s-1", "seq": 2, "type": "assistant/message", "payload": {"inputId": "other"}}`,
		`{"sessionId": "s-1", "seq": 3, "type": "turn.completed", "turnId": "t-1", "payload": {"inputId": "in-1", "resultType": "success", "response": ` + zcodeTestResponseJSON + `, "usage": {"inputTokens": 9, "outputTokens": 2}}}`,
		// A replayed sequence number is skipped, not an error.
		`{"sessionId": "s-1", "seq": 3, "type": "turn.failed", "turnId": "t-1", "payload": {"inputId": "in-1"}}`,
	}
}

// zcodeWriteEvidence lands the source binding and the event lines.
func zcodeWriteEvidence(t *testing.T, directory, source string, events []string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "native-source.json"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	body := ""
	if len(events) > 0 {
		body = strings.Join(events, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(directory, "native-events.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestZcodeMaterializeSource pins the event contract (合同 materialize 事件
// 合同): inputId/turnId association, monotonic seq, success-only completion,
// the original semantic submission and the receipt shape.
func TestZcodeMaterializeSource(t *testing.T) {
	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
	peerSubmitSemantics(directory, map[string]any{"status": "PASS", "summary": "observed"})
	zcodeWriteEvidence(t, directory,
		`{"sessionId": "s-1", "inputId": "in-1", "selection": {"model": "ref", "reasoning_effort": "low"}}`,
		zcodeSuccessEvents())
	receipt, err := zcodeMaterializeSource(directory)
	if err != nil {
		t.Fatal(err)
	}
	if textOf(receipt["provider"]) != "zcode" || textOf(receipt["identity"]) != "s-1" ||
		textOf(receipt["native_turn"]) != "t-1" || textOf(receipt["input_id"]) != "in-1" {
		t.Fatalf("receipt = %v", receipt)
	}
	if !valuesEqual(receipt["selection"], map[string]any{"model": "ref", "reasoning_effort": "low"}) {
		t.Fatalf("selection = %v", receipt["selection"])
	}
	if !valuesEqual(receipt["usage_evidence"], map[string]any{"inputTokens": jsonNumber("9"), "outputTokens": jsonNumber("2")}) {
		t.Fatalf("usage = %v", receipt["usage_evidence"])
	}
	if !valuesEqual(receipt["files"], proofsOf(t, directory, append(append([]string{}, nativeSourceFiles...), nativeToolCallsFile, nativeToolContextFile))) {
		t.Fatalf("files = %v", receipt["files"])
	}
	if _, err := os.Stat(filepath.Join(directory, "answer.raw.txt")); !os.IsNotExist(err) {
		t.Fatal("terminal prose became semantic evidence")
	}
	if body := readTestFileBytes(t, filepath.Join(directory, "answer.json")); string(body) != `{"status":"PASS","summary":"observed"}` {
		t.Fatalf("answer.json = %s", body)
	}
}

// TestZcodeMaterializeSourceRefusals pins the refusal texts of the evidence
// scan (成功/失败/缺完成/重复 start/terminal mismatch/序列).
func TestZcodeMaterializeSourceRefusals(t *testing.T) {
	const source = `{"sessionId": "s-1", "inputId": "in-1", "selection": {}}`
	start := `{"sessionId": "s-1", "seq": 1, "type": "turn.started", "turnId": "t-1", "payload": {"inputId": "in-1"}}`
	failed := `{"sessionId": "s-1", "seq": 2, "type": "turn.failed", "turnId": "t-1", "payload": {"inputId": "in-1"}}`
	completed := `{"sessionId": "s-1", "seq": 2, "type": "turn.completed", "turnId": "t-1", "payload": {"inputId": "in-1", "resultType": "success", "response": " {} "}}`
	errored := `{"sessionId": "s-1", "seq": 2, "type": "turn.completed", "turnId": "t-1", "payload": {"inputId": "in-1", "resultType": "error"}}`
	cases := []struct {
		name   string
		events []string
		want   string
	}{
		{"turn failed", []string{start, failed}, "ZCODE_EXECUTION: turn failed"},
		{"result not success", []string{start, errored}, "ZCODE_EXECUTION: turn failed"},
		{"completion missing", []string{start}, "ZCODE_RESULT: matching native completion is missing"},
		{"no events at all", nil, "ZCODE_RESULT: matching native completion is missing"},
		{"duplicate start", []string{start,
			`{"sessionId": "s-1", "seq": 2, "type": "turn.started", "turnId": "t-2", "payload": {"inputId": "in-1"}}`},
			"ZCODE_BINDING: duplicate/missing start"},
		{"start without turnId",
			[]string{`{"sessionId": "s-1", "seq": 1, "type": "turn.started", "payload": {"inputId": "in-1"}}`},
			"ZCODE_BINDING: duplicate/missing start"},
		{"terminal without start", []string{completed}, "ZCODE_BINDING: terminal mismatch"},
		{"terminal turn differs",
			[]string{start, `{"sessionId": "s-1", "seq": 2, "type": "turn.completed", "turnId": "t-9", "payload": {"inputId": "in-1", "resultType": "success"}}`},
			"ZCODE_BINDING: terminal mismatch"},
		{"second terminal", []string{start, completed,
			`{"sessionId": "s-1", "seq": 3, "type": "turn.completed", "turnId": "t-1", "payload": {"inputId": "in-1", "resultType": "success"}}`},
			"ZCODE_BINDING: terminal mismatch"},
		{"invalid sequence text",
			[]string{`{"sessionId": "s-1", "seq": "1", "type": "turn.started", "turnId": "t-1", "payload": {"inputId": "in-1"}}`},
			"ZCODE_BINDING: invalid sequence"},
		{"negative sequence",
			[]string{`{"sessionId": "s-1", "seq": -1, "type": "turn.started", "turnId": "t-1", "payload": {"inputId": "in-1"}}`},
			"ZCODE_BINDING: invalid sequence"},
		{"float sequence",
			[]string{`{"sessionId": "s-1", "seq": 1.5, "type": "turn.started", "turnId": "t-1", "payload": {"inputId": "in-1"}}`},
			"ZCODE_BINDING: invalid sequence"},
		{"mixed session evidence",
			[]string{start, `{"sessionId": "s-2", "seq": 2, "type": "turn.completed", "turnId": "t-1", "payload": {"inputId": "in-1", "resultType": "success"}}`},
			"BINDING: mixed session evidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			zcodeWriteEvidence(t, directory, source, tc.events)
			_, err := zcodeMaterializeSource(directory)
			if blocked, ok := err.(*blockedError); !ok || blocked.message != tc.want {
				t.Fatalf("err = %v (%T), want %q", err, err, tc.want)
			}
		})
	}
}

// TestZcodeMaterializeSourceRejectsChangedSavedSemantics verifies the materialized
// payload against the original successful semantic submission.
func TestZcodeMaterializeSourceRejectsChangedSavedSemantics(t *testing.T) {
	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
	peerSubmitSemantics(directory, map[string]any{"status": "PASS", "summary": "observed"})
	zcodeWriteEvidence(t, directory,
		`{"sessionId": "s-1", "inputId": "in-1", "selection": {}}`, zcodeSuccessEvents())
	if err := os.WriteFile(filepath.Join(directory, "answer.json"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := zcodeMaterializeSource(directory)
	if err == nil || err.Error() != "NATIVE_RESULT: saved semantics differ from original tool submission" {
		t.Fatalf("err = %v", err)
	}
}

// TestZcodeReconcileEligibility recovers only original successful process and
// source evidence, without submitting another semantic result.
func TestZcodeReconcileEligibility(t *testing.T) {
	fixture := func() string {
		directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
		peerSubmitSemantics(directory, map[string]any{"status": "PASS", "summary": "observed"})
		zcodeWriteEvidence(t, directory,
			`{"sessionId": "s-1", "inputId": "in-1", "selection": {"model": "ref", "reasoning_effort": "low"}}`,
			zcodeSuccessEvents())
		return directory
	}
	directory := fixture()
	names := []string{"native-source.json", "native-events.jsonl", nativeToolCallsFile, nativeToolContextFile}
	originalProofs := proofsOf(t, directory, names)
	source, err := zcodeReconcile(directory, map[string]any{"exit_code": 0, "error": ""})
	if err != nil || source["identity"] != "s-1" || source["native_turn"] != "t-1" || source["input_id"] != "in-1" {
		t.Fatalf("recovered source = %v %v", source, err)
	}
	if !valuesEqual(source["selection"], map[string]any{"model": "ref", "reasoning_effort": "low"}) || !valuesEqual(originalProofs, proofsOf(t, directory, names)) {
		t.Fatal("recovery changed original selection or semantic evidence")
	}
	if string(readTestFileBytes(t, filepath.Join(directory, "answer.json"))) != `{"status":"PASS","summary":"observed"}` {
		t.Fatal("recovery changed canonical semantic bytes")
	}
	for _, process := range []map[string]any{{"exit_code": 1, "error": ""}, {"exit_code": 0, "error": "interrupted"}} {
		other := fixture()
		_, err := zcodeReconcile(other, process)
		if err == nil || err.Error() != "ZCODE_RECONCILE: original native process did not complete successfully" {
			t.Fatalf("failed original process: %v", err)
		}
		if _, err := os.Stat(filepath.Join(other, "source.json")); !os.IsNotExist(err) {
			t.Fatal("failed original process became a recovered success")
		}
	}
}

// TestZcodeRespondTable pins the four client-directed replies (合同 respond
// 四路): the preferences fallback, the unavailable official auth, the runtime
// headers answer from the host's own credential helper, and the never-answered
// user decisions.
func TestZcodeRespondTable(t *testing.T) {
	z := &zcodeClient{directory: t.TempDir()}

	reply, err := z.respond("session/requestRuntimePreferences", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, reply.payload); got != `{"error":{"code":-32601,"message":"Use native runtime preferences fallback"}}` ||
		reply.status != "native-default-fallback" {
		t.Fatalf("preferences = %s %s", got, reply.status)
	}

	reply, err = z.respond("interaction/requestOfficialMcpAuthHeaders", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, reply.payload); got != `{"result":{"ok":false,"reason":"official_auth_unavailable"}}` ||
		reply.status != "official_auth_unavailable" {
		t.Fatalf("official auth = %s %s", got, reply.status)
	}

	// Without a package-owned helper, the credential remains unavailable.
	reply, err = z.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, reply.payload); got != `{"result":{"errorMessage":"no provisioned request credential for p1","headersApplied":false}}` ||
		reply.status != "provider-runtime-headers-unavailable" {
		t.Fatalf("missing credential = %s %s", got, reply.status)
	}
	reply, err = z.respond(zcodeRuntimeHeadersMethod, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if reply.status != "provider-runtime-headers-unavailable" {
		t.Fatalf("no provider = %s", reply.status)
	}

	// The two user decisions never reach respond any more: the transport's
	// interaction specs hold them, so respond has no branch for them
	// and answers nothing.
	for _, method := range []string{"interaction/requestPermission", "interaction/requestUserInput"} {
		if reply, err := z.respond(method, map[string]any{}); err != nil || reply != nil {
			t.Fatalf("%s: held interactions must not be answered inline: %v %v", method, reply, err)
		}
	}

	if reply, err := z.respond("session/unknown", map[string]any{}); err != nil || reply != nil {
		t.Fatalf("unknown method = %v %v", reply, err)
	}
}

// TestZcodeRespondRuntimeHeadersWithCredential pins the credential answer
// through the host-side helper: the apiKey reaches the protocol reply only.
// Skipped when node is absent (the zcode host itself requires node).
func TestZcodeRespondRuntimeHeadersWithCredential(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	if runNativePackageTest(t) {
		return
	}
	root, err := nativePackageRoot()
	if err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(root, "internal", "harness")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(packageDir, zcodeCredentialHelper)
	if err := os.WriteFile(helper, []byte("process.stdout.write('key-for-' + process.argv[2]);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(store, []byte(`{"anything": "ignored"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(zcodeCredentialHelperEnv, store)

	directory := t.TempDir()
	for _, name := range []string{"protocol.jsonl", "host.stderr", "interaction-status.json", "native-events.jsonl"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	z := &zcodeClient{directory: directory}
	reply, err := z.respond(zcodeRuntimeHeadersMethod, map[string]any{"providerId": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, reply.payload); got != `{"result":{"headersApplied":true,"requestAuth":{"apiKey":"key-for-p1"}}}` ||
		reply.status != "provider-runtime-headers-applied" {
		t.Fatalf("credential reply = %s %s", got, reply.status)
	}
	// The secret appears nowhere in the evidence directory.
	for _, name := range []string{"protocol.jsonl", "host.stderr", "interaction-status.json", "native-events.jsonl"} {
		if body := readTestFileBytes(t, filepath.Join(directory, name)); strings.Contains(string(body), "key-for-p1") {
			t.Fatalf("the credential leaked into %s", name)
		}
	}
}

// TestZcodeClassify pins the resume classification: -32004 plus the host's
// "Session not found:" prefix, and nothing else.
func TestZcodeClassify(t *testing.T) {
	err := zcodeClassify("session/resume",
		decodeTestJSON(t, `{"code": -32004, "message": "Session not found: s-1"}`),
		map[string]any{"sessionId": "s-1"})
	unavailable, ok := err.(*sessionUnavailableError)
	if !ok {
		t.Fatalf("err = %T %v", err, err)
	}
	if unavailable.message != "ZCODE_SESSION_UNAVAILABLE: Session not found: s-1" ||
		textOf(unavailable.identity) != "s-1" || unavailable.method != "session/resume" ||
		textOf(unavailable.code) != "-32004" {
		t.Fatalf("unavailable = %+v", unavailable)
	}

	for _, tc := range []struct {
		name     string
		method   string
		errValue string
	}{
		{"wrong method", "session/create", `{"code": -32004, "message": "Session not found: s-1"}`},
		{"wrong code", "session/resume", `{"code": -32602, "message": "Session not found: s-1"}`},
		{"wrong prefix", "session/resume", `{"code": -32004, "message": "gone"}`},
	} {
		if err := zcodeClassify(tc.method, decodeTestJSON(t, tc.errValue), map[string]any{}); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

// TestZcodeNotificationFilter pins the notification hook: only session/event
// params of the bound identity are recorded, child sessions are not, and the
// evidence line is canonical.
func TestZcodeNotificationFilter(t *testing.T) {
	directory := t.TempDir()
	z := &zcodeClient{directory: directory}

	messages := []string{
		`{"id": 1, "result": {}}`,
		`{"method": "session/event", "params": {"sessionId": "s-1", "seq": 1, "type": "turn.started", "turnId": "t-1", "payload": {"inputId": "in-1"}}}`,
		`{"method": "session/event", "params": {"sessionId": "child-1", "seq": 1, "type": "turn.started"}}`,
		`{"method": "session/event", "params": "not-a-dict"}`,
		`{"method": "other/notification", "params": {"sessionId": "s-1"}}`,
	}
	for _, message := range messages {
		z.notification(decodeTestJSON(t, message))
	}
	if len(z.events) != 0 {
		t.Fatalf("unbound identity must record nothing: %v", z.events)
	}
	if _, statErr := os.Stat(filepath.Join(directory, "native-events.jsonl")); !os.IsNotExist(statErr) {
		t.Fatal("no evidence may exist before the identity is bound")
	}

	z.identity = "s-1"
	for _, message := range messages {
		z.notification(decodeTestJSON(t, message))
	}
	if len(z.events) != 1 || textOf(z.events[0]["turnId"]) != "t-1" {
		t.Fatalf("events = %v", z.events)
	}
	want := `{"payload":{"inputId":"in-1"},"seq":1,"sessionId":"s-1","turnId":"t-1","type":"turn.started"}` + "\n"
	if got := readTestFileBytes(t, filepath.Join(directory, "native-events.jsonl")); string(got) != want {
		t.Fatalf("native-events.jsonl:\n got %s\nwant %s", got, want)
	}
}

// TestZcodeInteractionObserved pins the attribution rule's truth table: only
// the host's permission.resolved receipt naming the held requestId, a
// tool.updated progress event naming the held toolCallId, or a turn terminal
// event naming the held turnId confirm a delivered hold; same-session
// metadata (titleUpdated, session.updated) and telemetry never do.
func TestZcodeInteractionObserved(t *testing.T) {
	hold := &nativePendingHold{params: map[string]any{
		"requestId": "req-1", "sessionId": "s-1", "turnId": "t-1", "toolCallId": "tc-1"}}
	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{"receipt names the held request", `{"method": "session/event", "params": {"sessionId": "s-1", "seq": 57, "type": "permission.resolved", "turnId": "t-1", "payload": {"decision": "allow", "reason": "Approved once", "requestId": "req-1", "toolCallId": "tc-1"}}}`, true},
		{"receipt names another request", `{"method": "session/event", "params": {"sessionId": "s-1", "type": "permission.resolved", "payload": {"requestId": "req-2", "toolCallId": "tc-1"}}}`, false},
		{"tool progress names the held call", `{"method": "session/event", "params": {"sessionId": "s-1", "seq": 58, "type": "tool.updated", "turnId": "t-1", "payload": {"kind": "started", "toolCallId": "tc-1", "toolName": "Bash"}}}`, true},
		{"tool progress names another call", `{"method": "session/event", "params": {"sessionId": "s-1", "turnId": "t-1", "type": "tool.updated", "payload": {"toolCallId": "tc-2"}}}`, false},
		{"turn terminal names the held turn", `{"method": "session/event", "params": {"sessionId": "s-1", "seq": 71, "type": "turn.completed", "turnId": "t-1", "payload": {"inputId": "in-1", "resultType": "success"}}}`, true},
		{"turn terminal names another turn", `{"method": "session/event", "params": {"sessionId": "s-1", "type": "turn.completed", "turnId": "t-2", "payload": {"inputId": "in-1"}}}`, false},
		{"turn start is not terminal", `{"method": "session/event", "params": {"sessionId": "s-1", "type": "turn.started", "turnId": "t-1", "payload": {"inputId": "in-1"}}}`, false},
		{"title update names session and turn", `{"method": "session/event", "params": {"sessionId": "s-1", "seq": 4, "type": "session.titleUpdated", "turnId": "t-1", "payload": {"title": "t", "source": "first_input"}}}`, false},
		{"settings update of the same session", `{"method": "session/event", "params": {"sessionId": "s-1", "type": "session.updated", "payload": {"mode": "build"}}}`, false},
		{"telemetry is not a session event", `{"method": "process/mcpTelemetry", "params": {"event": "heartbeat"}}`, false},
	}
	for _, test := range cases {
		if got := zcodeInteractionObserved(hold, decodeTestJSON(t, test.message).(map[string]any)); got != test.want {
			t.Errorf("%s: got %v", test.name, got)
		}
	}
	// A user-input hold carries no toolCallId, so tool progress cannot be
	// attributed to it even when it names some call.
	userInput := &nativePendingHold{params: map[string]any{
		"requestId": "ui-1", "sessionId": "s-1", "turnId": "t-1"}}
	message := decodeTestJSON(t, `{"method": "session/event", "params": {"sessionId": "s-1", "type": "tool.updated", "payload": {"toolCallId": "tc-1"}}}`).(map[string]any)
	if zcodeInteractionObserved(userInput, message) {
		t.Error("tool progress must not confirm a hold without a toolCallId")
	}
}

// zcodeStreamEvent renders one streamed assistant/message notification whose
// payload is unique per sequence (the retention measurements count payloads).
func zcodeStreamEvent(seq int) map[string]any {
	return map[string]any{"method": "session/event", "params": map[string]any{
		"sessionId": "s-1",
		"seq":       jsonNumber(strconv.Itoa(seq)),
		"type":      "assistant/message",
		"payload": map[string]any{
			"inputId": "other-input",
			"text":    strconv.Itoa(seq) + "-" + strings.Repeat("x", 16<<10),
		},
	}}
}

// zcodeDrainEvents is the execution loop's consume pattern: each event's
// retained slot is cleared as the cursor advances, and the fully drained
// buffer is reset together with the cursor -- the same inline reset execute
// performs before waiting for more events.
func zcodeDrainEvents(z *zcodeClient, position int) int {
	for position < len(z.events) {
		z.events[position] = nil
		position++
	}
	z.events = z.events[:0]
	return 0
}

// TestZcodeEventsReleasedUnderLongStream pins the receive buffer's retention
// bound: a stream of 1536 synthetic events (16KB payloads, ~24MB in total,
// the shape the review's synthetic probe measured), delivered in batches the
// way the reader goroutine lands them, never lets the retained slice grow
// past one batch; every event still reaches native-events.jsonl; and the
// coarse heap contrast against the pre-fix pattern (append-only, cursor
// advanced, nothing released) shows the consumed events are collectable, not
// merely hidden behind the cursor.
func TestZcodeEventsReleasedUnderLongStream(t *testing.T) {
	const batches = 48
	const batch = 32
	const events = batches * batch // 1536
	directory := t.TempDir()
	z := &zcodeClient{directory: directory, identity: "s-1"}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	position := 0
	peak := 0
	for seq := 1; seq <= events; seq++ {
		z.notification(zcodeStreamEvent(seq))
		if len(z.events) > peak {
			peak = len(z.events)
		}
		if len(z.events) == batch {
			position = zcodeDrainEvents(z, position)
			if len(z.events) != 0 {
				t.Fatalf("a drained buffer must compact to empty, got %d", len(z.events))
			}
		}
	}
	position = zcodeDrainEvents(z, position)
	if position != 0 || len(z.events) != 0 {
		t.Fatalf("final drain: position %d, len %d", position, len(z.events))
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	released := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	// z stays live through the measurement above (its emptiness is asserted
	// here), so the small growth measures the released stream, not a dead
	// client.
	if live := len(z.events); live != 0 {
		t.Fatalf("the released buffer must end empty, got %d", live)
	}
	if peak > batch {
		t.Fatalf("retained events peaked at %d, want <= %d", peak, batch)
	}
	if body := readTestFileBytes(t, filepath.Join(directory, "native-events.jsonl")); strings.Count(string(body), "\n") != events {
		t.Fatalf("native-events.jsonl lines = %d, want %d", strings.Count(string(body), "\n"), events)
	}

	// The pre-fix retention pattern over the same stream: append-only, the
	// cursor advanced, nothing released. The whole transcript stays reachable.
	retained := &zcodeClient{directory: t.TempDir(), identity: "s-1"}
	runtime.GC()
	runtime.ReadMemStats(&before)
	cursor := 0
	for seq := 1; seq <= events; seq++ {
		retained.notification(zcodeStreamEvent(events + seq))
		if cursor < len(retained.events) {
			_ = textOf(getOf(retained.events[cursor], "sessionId"))
			cursor++
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	kept := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	// The liveness twin of the released side: retained is read after the
	// measurement, so the growth really is the retained transcript.
	if got := len(retained.events); got != events {
		t.Fatalf("the retained pattern must keep every event, got %d", got)
	}
	if kept < 16<<20 {
		t.Fatalf("the retained pattern must hold the ~24MB transcript, heap grew %d bytes", kept)
	}
	if released > kept/2 {
		t.Fatalf("released growth %d bytes is not clearly below the retained %d bytes", released, kept)
	}
	t.Logf("1536 events x 16KB payloads: released pattern net heap change %d bytes, retained pattern %d",
		released, kept)
}

// TestZcodeValidateBindingAndVerifySource pins zcode's registry operations:
// the provider check and the complete original native and tool proof set.
func TestZcodeValidateBindingAndVerifySource(t *testing.T) {
	root := t.TempDir()
	node := filepath.Join(root, "node")
	script := filepath.Join(root, "zcode.cjs")
	for _, path := range []string{node, script} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	nodeProof, err := fileProofOf(node)
	if err != nil {
		t.Fatal(err)
	}
	scriptProof, err := fileProofOf(script)
	if err != nil {
		t.Fatal(err)
	}

	if err := zcodeValidateBinding(decodeTestJSON(t, `{"provider": "dsh", "files": {}}`)); err == nil ||
		err.Error() != "HOST_BINDING: provider differs" {
		t.Fatalf("foreign provider: %v", err)
	}
	changed := map[string]any{"provider": "zcode", "files": map[string]any{
		node: map[string]any{"sha256": strings.Repeat("0", 64), "size": 1}}}
	if err := zcodeValidateBinding(changed); err == nil ||
		err.Error() != "HOST_CHANGED: bound native executable/script changed: "+node {
		t.Fatalf("changed proof: %v", err)
	}
	valid := map[string]any{"provider": "zcode", "files": map[string]any{
		node: nodeProof, script: scriptProof}}
	if err := zcodeValidateBinding(valid); err != nil {
		t.Fatalf("valid binding: %v", err)
	}

	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
	peerSubmitSemantics(directory, map[string]any{"status": "PASS", "summary": "observed"})
	zcodeWriteEvidence(t, directory, `{"sessionId":"s-1","inputId":"in-1","selection":{}}`, zcodeSuccessEvents())
	receipt, err := zcodeMaterializeSource(directory)
	if err != nil {
		t.Fatal(err)
	}
	proofs := objectOrEmpty(receipt["files"])
	if err := zcodeVerifySource(directory, map[string]any{"provider": "zcode", "identity": "s-1", "files": proofs}); err != nil {
		t.Fatalf("valid source: %v", err)
	}
	if err := zcodeVerifySource(directory, map[string]any{"provider": "dsh", "identity": "s-1", "files": proofs}); err == nil ||
		err.Error() != "NATIVE_BINDING: missing or different provider/identity" {
		t.Fatalf("foreign source: %v", err)
	}
	if err := zcodeVerifySource(directory, map[string]any{"provider": "zcode", "identity": "s-1",
		"files": map[string]any{nativeToolCallsFile: proofs[nativeToolCallsFile]}}); err == nil ||
		err.Error() != "EVIDENCE_MISSING: native source proofs absent" {
		t.Fatalf("short proof set: %v", err)
	}
}

// TestZcodeImplementationRouting pins the dispatch: zcode routes to its own
// adapter, an unknown provider reports the registered adapters.
func TestZcodeImplementationRouting(t *testing.T) {
	impl, err := nativeImplementationFor("zcode")
	if err != nil {
		t.Fatal(err)
	}
	if impl.capture == nil || impl.execute == nil {
		t.Fatalf("zcode implementation = %+v", impl)
	}
	if _, err := nativeImplementationFor("ghost"); err == nil ||
		err.Error() != "UNSUPPORTED_NATIVE_HOST: ghost; execution adapters: dsh, zcode, claude, codex, workbuddy, cursor" {
		t.Fatalf("unknown provider: %v", err)
	}
}
