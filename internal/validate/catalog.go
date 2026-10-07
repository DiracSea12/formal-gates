package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"formal-gates/internal/prompts"
	"hash"
	"sort"
)

func hashPart(h hash.Hash, name, content string) {
	fmt.Fprintf(h, "%d:%s\n%d:%s\n", len(name), name, len(content), content)
}

func promptContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// composedGatePromptHash is the content hash of the catalog-dependent portion
// of a composed gate prompt: the shared reviewer base plus the gate's own
// content. A base-only change therefore moves every gate's hash and enables
// per-gate re-dispatch.
func composedGatePromptHash(catalog prompts.PromptCatalog, content string) string {
	h := sha256.New()
	hashPart(h, "base", catalog.Base)
	hashPart(h, "gate", content)
	return hex.EncodeToString(h.Sum(nil))
}

// composedActionPromptHash is the content hash of the catalog-dependent portion
// of a composed action prompt. Injected reviewer actions carry the
// shared reviewer base, so their hash includes the base — a base-only change
// moves every injected reviewer action's hash and enables the inheritance
// judgment, symmetric with the gate path; non-reviewer actions keep the plain
// content hash (their composed prompt is their own content only).
func composedActionPromptHash(catalog prompts.PromptCatalog, action prompts.PromptDefinition) string {
	if !isReviewerAction(action.ID) {
		return promptContentHash(action.Content)
	}
	h := sha256.New()
	hashPart(h, "base", catalog.Base)
	hashPart(h, "action", action.Content)
	return hex.EncodeToString(h.Sum(nil))
}

// catalogPromptHashes returns one content hash per catalog entry, keyed by
// qualified id ("base", "gate:<id>", "action:<id>") so that deltas can be
// computed per gate and per action. Gate entries hash the composed gate prompt
// (reviewer base plus gate content) so a base-only change is reported per gate.
func catalogPromptHashes(catalog prompts.PromptCatalog) map[string]string {
	hashes := map[string]string{"base": promptContentHash(catalog.Base)}
	for _, gate := range catalog.Gates {
		hashes["gate:"+gate.ID] = composedGatePromptHash(catalog, gate.Content)
	}
	for _, action := range catalog.Actions {
		hashes["action:"+action.ID] = composedActionPromptHash(catalog, action)
	}
	return hashes
}

// catalogDelta reports the qualified catalog ids whose recorded prompt content
// hash no longer matches the current catalog. Runs started before per-entry
// hashing have no recorded hashes; when their catalog revision moved, every
// current entry is reported as changed so the main agent can classify it.
func catalogDelta(state RunState, catalog prompts.PromptCatalog) []string {
	if state.PromptHashes == nil {
		if state.CatalogRevision == catalog.CatalogRevision && state.BasePromptRevision == catalog.BaseRevision {
			return nil
		}
		changed := make([]string, 0, len(catalog.Gates)+len(catalog.Actions)+1)
		for id := range catalogPromptHashes(catalog) {
			changed = append(changed, id)
		}
		sort.Strings(changed)
		return changed
	}
	current := catalogPromptHashes(catalog)
	changed := []string{}
	seen := map[string]bool{}
	for id, hash := range current {
		seen[id] = true
		if recorded, ok := state.PromptHashes[id]; !ok || recorded != hash {
			changed = append(changed, id)
		}
	}
	// Report recorded entries removed from the current catalog so a removed
	// selected gate is a recoverable delta rather than an invisible dead-end.
	for id := range state.PromptHashes {
		if !seen[id] {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed
}
