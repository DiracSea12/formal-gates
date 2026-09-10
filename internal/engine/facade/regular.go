package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// prepareRegular reads the one editable requirement before bootstrap. Structural
// precheck is deliberately a later drive step so failure keeps the intake visible.
func prepareRegular(root string, req StartRequest) (protocol.RegularState, error) {
	if req.Provider != "" {
		if _, err := runtimeAdapter(req.Provider); err != nil {
			return protocol.RegularState{}, err
		}
	}
	if req.Route != "" && req.Route != "regular" {
		return protocol.RegularState{}, fmt.Errorf("regular intake cannot select full/custom/lightweight at start; confirm the document before choosing mode")
	}
	p := filepath.Clean(req.RequirementPath)
	if filepath.IsAbs(p) || p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return protocol.RegularState{}, fmt.Errorf("requirement must be one project-relative file")
	}
	data, err := os.ReadFile(filepath.Join(root, p))
	if err != nil {
		return protocol.RegularState{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return protocol.RegularState{}, err
	}
	config, err := settings.Load(root, home)
	if err != nil {
		return protocol.RegularState{}, err
	}
	cmd := exec.Command("git", "-C", root, "rev-parse", "--verify", "HEAD^{commit}")
	out, err := cmd.Output()
	if err != nil {
		return protocol.RegularState{}, fmt.Errorf("regular intake requires a Git repository with a committed baseline: %w", err)
	}
	head := strings.TrimSpace(string(out))
	if req.BaseSnapshot != "" && req.BaseSnapshot != head {
		return protocol.RegularState{}, fmt.Errorf("B1 intake base-snapshot must match the current immutable Git HEAD")
	}
	gates := []string{"complexity-gate", "implementation-quality-gate"}
	initial := protocol.RegularState{CatalogGates: gates, Requirement: protocol.Document(filepath.ToSlash(p), data), Settings: config, BaseSnapshot: head, Candidate: head}
	ownerPath := filepath.Join(root, ".gates", "start-owner.json")
	if data, err := os.ReadFile(ownerPath); err == nil {
		var owner struct {
			Transcript string `json:"transcript"`
			Session    string `json:"session"`
			Provider   string `json:"provider"`
		}
		if json.Unmarshal(data, &owner) == nil {
			initial.OwnerStartedAt = time.Now().UTC()
			initial.OwnerTranscript = owner.Transcript
			initial.OwnerSession = owner.Session
			initial.OwnerProvider = owner.Provider
			initial.OwnerCostBaseline, _ = cost.CaptureBaseline(owner.Provider, owner.Transcript)
		}
		_ = os.Remove(ownerPath)
	}
	return initial, nil
}

func (f *Facade) verifyRegularDocument(event *protocol.Event) error {
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	r := snap.State.Regular
	if r != nil && event != nil && event.Regular != nil && event.Regular.Request != nil && event.Regular.Request.Action == "mode" && event.Regular.Request.Mode == "regular" {
		if _, err := captureRuntimeContext(snap.State.RunProvider, r.Settings.Values); err != nil {
			return fmt.Errorf("BRIDGE_UNAVAILABLE: native runtime configuration cannot be used: %w", err)
		}
	}
	if r == nil || r.Terminal != "" {
		return nil
	}
	if event != nil && event.Regular != nil && event.Regular.Operation == "DECIDE" && r.Ask != nil && r.Ask.Kind == "abort" {
		return nil
	}

	if event != nil && event.Regular != nil && event.Regular.Operation == "REQUEST" && event.Regular.Request != nil && event.Regular.Request.Action == "adopt-external" {
		return f.verifyExternal(r, event.Regular.Request.External)
	}
	if event != nil && event.Regular != nil && event.Regular.Operation == "DECIDE" && r.Ask != nil && r.Ask.Kind == "adopt-external" {
		return f.verifyExternal(r, r.Ask.Request.External)
	}

	if event != nil && event.Regular != nil && event.Regular.Operation == "REQUEST" && event.Regular.Request != nil {
		q := event.Regular.Request
		if q.Action == "requirement-change" || q.Action == "abort" {
			return nil
		}
		if q.Action == "apply-requirement" && q.Document != nil {
			data, err := os.ReadFile(filepath.Join(f.root, r.Requirement.Path))
			if err != nil {
				return err
			}
			if protocol.Document(r.Requirement.Path, data).Revision != q.Document.Revision {
				return fmt.Errorf("requirement change must bind the actual complete file revision")
			}
			return nil
		}
	}
	// Phase A isolates all business progression while allowing source and result
	// collection. Applying the confirmed candidate is validated against its bytes.
	if r.ChangePending {
		if event != nil && event.Regular != nil && event.Regular.Operation == "DECIDE" && r.Ask != nil && r.Ask.Kind == "apply-requirement" && r.Ask.Request.Document != nil {
			data, err := os.ReadFile(filepath.Join(f.root, r.Requirement.Path))
			if err != nil {
				return err
			}
			if protocol.Document(r.Requirement.Path, data).Revision != r.Ask.Request.Document.Revision {
				return fmt.Errorf("REQUIREMENT_CHANGED: candidate changed after requirement preview")
			}
		}
		return nil
	}
	data, err := os.ReadFile(filepath.Join(f.root, r.Requirement.Path))
	if err != nil {
		return err
	}
	// Check the original bytes: JSON persistence replaces invalid UTF-8 in
	// Document.Text, which otherwise turns an encoding error into a syntax error.
	if !utf8.Valid(data) {
		return fmt.Errorf("requirement encoding precheck failed: %s must contain valid UTF-8", r.Requirement.Path)
	}
	if protocol.Document(r.Requirement.Path, data).Revision != r.Requirement.Revision {
		recovering := false
		if r.Preparation != nil && r.Preparation.Operation != nil {
			op := r.Preparation.Operation
			if op.Kind == "promote" && op.Claimed {
				old, readErr := exec.Command("git", "-C", f.root, "show", op.Target+":"+r.Requirement.Path).Output()
				recovering = readErr == nil && string(old) == string(data)
			}
		}
		if !recovering {
			return fmt.Errorf("REQUIREMENT_CHANGED: request requirement-change before continuing")
		}
	}
	out, err := exec.Command("git", "-C", f.root, "rev-parse", "--verify", "HEAD^{commit}").Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != r.Candidate {
		if r.Preparation != nil && r.Preparation.Operation != nil && r.Preparation.Operation.Kind == "promote" && r.Preparation.Operation.Claimed && r.Preparation.Candidate != nil && strings.TrimSpace(string(out)) == r.Preparation.Candidate.Candidate.Identity {
			return nil
		}
		return fmt.Errorf("EXTERNAL_CHANGED: inspect Git HEAD and request adopt-external with exact identity, lineage and diffDigest")
	}
	return nil
}
func (f *Facade) verifyExternal(r *protocol.RegularState, observation *protocol.ExternalObservation) error {
	if observation == nil {
		return fmt.Errorf("adopt-external observation is required")
	}
	data, err := os.ReadFile(filepath.Join(f.root, r.Requirement.Path))
	if err != nil {
		return err
	}
	if protocol.Document(r.Requirement.Path, data).Revision != r.Requirement.Revision {
		return fmt.Errorf("REQUIREMENT_CHANGED: requirement body changes must use requirement-change")
	}
	out, err := exec.Command("git", "-C", f.root, "rev-parse", "--verify", "HEAD^{commit}").Output()
	if err != nil {
		return err
	}
	head := strings.TrimSpace(string(out))
	if observation.Identity != head || observation.Lineage != r.BaseSnapshot {
		return fmt.Errorf("STALE_FRESHNESS: observed Git identity/lineage changed")
	}
	if err = exec.Command("git", "-C", f.root, "merge-base", "--is-ancestor", r.BaseSnapshot, head).Run(); err != nil {
		return fmt.Errorf("adopt-external candidate must descend from original base")
	}
	diff, err := exec.Command("git", "-C", f.root, "diff", "--binary", r.Candidate, head, "--").Output()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(diff)
	if observation.DiffDigest != "sha256:"+hex.EncodeToString(sum[:]) {
		return fmt.Errorf("STALE_FRESHNESS: observed Git diff does not match")
	}
	return nil
}

// Settings exposes the same validation and overlay used by bootstrap, without a
// state write. It is the public diagnostic for user/AI edits to config.json.
func Settings(root string) (settings.Effective, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return settings.Effective{}, err
	}
	return settings.Load(root, home)
}

// Owner usage is terminal reporting in the existing cost projection. Intervals
// that overlap another run of the same conversation stay unavailable rather than
// charging the same conversation interval to two runs. Current engine summaries
// retain interval identity after active state cleanup.
func (f *Facade) settleOwnerReport(run Run) error {
	if run.Regular == nil || run.Cost == nil || run.Cost.Owner != nil {
		return nil
	}
	r := run.Regular
	entry := cost.OwnerCost{Source: cost.SourceUnavailable}
	overlapped := false
	if r.OwnerTranscript != "" && !r.OwnerStartedAt.IsZero() {
		peers, err := os.ReadDir(filepath.Join(f.root, ".gates", "engine"))
		if err != nil {
			return err
		}
		for _, dir := range peers {
			if !dir.IsDir() || dir.Name() == f.runID {
				continue
			}
			other, err := Open(f.root, dir.Name())
			if err != nil {
				overlapped = true
				continue
			}
			peer, err := other.Show()
			if err != nil {
				overlapped = true
				continue
			}
			if peer.Regular == nil || filepath.Clean(peer.Regular.OwnerTranscript) != filepath.Clean(r.OwnerTranscript) {
				continue
			}
			if peer.Status == "COMPLETE" {
				summary, err := other.readSummary()
				if err != nil {
					overlapped = true
					continue
				}
				end, err := time.Parse(time.RFC3339Nano, summary.CompletedAt)
				if err == nil && !end.After(r.OwnerStartedAt) {
					continue
				}
			}
			overlapped = true
		}
	}
	if !overlapped {
		if usage, err := cost.MeasureOwner(r.OwnerProvider, r.OwnerTranscript, r.OwnerCostBaseline); err == nil {
			entry = cost.OwnerCost{Source: cost.SourceTranscript, InputCacheHitTokens: usage.InputCacheHitTokens, InputCacheMissTokens: usage.InputCacheMissTokens, TotalInputTokens: usage.InputCacheHitTokens + usage.InputCacheMissTokens, OutputTokens: usage.OutputTokens}
		}
	}
	cost.RecordOwner(run.Cost, entry)
	return nil
}
