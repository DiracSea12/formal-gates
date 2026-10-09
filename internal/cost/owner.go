package cost

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// OwnerCost belongs only to this run. SourceUnavailable means the interval
// could not be measured; its absent measurement is serialized as null.
type OwnerCost struct {
	InputCacheHitTokens  int64  `json:"inputCacheHitTokens"`
	InputCacheMissTokens int64  `json:"inputCacheMissTokens"`
	OutputTokens         int64  `json:"outputTokens"`
	TotalInputTokens     int64  `json:"totalInputTokens"`
	Source               string `json:"source"`
	Reason               string `json:"reason,omitempty"`
}

// TranscriptSnapshot fixes the parsed start baseline and the bytes it covers.
// Checking this prefix at completion distinguishes appends from truncation or
// rewriting, even when a replacement file is longer and reports larger totals.
// The owning RunState stores this small record, never transcript contents.
type TranscriptSnapshot struct {
	Usage  Usage  `json:"usage"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

// CaptureBaseline reads a single snapshot of the owner transcript. A missing
// usage event or incomplete trailing record cannot establish an exact baseline.
func CaptureBaseline(provider, path string) (*TranscriptSnapshot, error) {
	data, usage, err := readOwnerTranscript(provider, path)
	if err != nil {
		return nil, err
	}
	return &TranscriptSnapshot{Usage: usage, Size: int64(len(data)), Digest: transcriptDigest(data)}, nil
}

// MeasureOwner subtracts the persisted start baseline from the same source at
// completion. It never falls back to whole-file usage when the interval is lost.
func MeasureOwner(provider, path string, baseline *TranscriptSnapshot) (Usage, error) {
	if baseline == nil {
		return Usage{}, fmt.Errorf("owner baseline is unavailable")
	}
	data, end, err := readOwnerTranscript(provider, path)
	if err != nil {
		return Usage{}, err
	}
	if baseline.Size < 0 || int64(len(data)) < baseline.Size || transcriptDigest(data[:baseline.Size]) != baseline.Digest {
		return Usage{}, fmt.Errorf("owner transcript was truncated or rewritten")
	}
	delta := Usage{
		InputCacheHitTokens:  end.InputCacheHitTokens - baseline.Usage.InputCacheHitTokens,
		InputCacheMissTokens: end.InputCacheMissTokens - baseline.Usage.InputCacheMissTokens,
		OutputTokens:         end.OutputTokens - baseline.Usage.OutputTokens,
	}
	if delta.InputCacheHitTokens < 0 || delta.InputCacheMissTokens < 0 || delta.OutputTokens < 0 {
		return Usage{}, fmt.Errorf("owner usage interval is inconsistent")
	}
	return delta, nil
}

func readOwnerTranscript(provider, path string) ([]byte, Usage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, Usage{}, err
	}
	// A host may be writing its final JSONL record while start/Seal is called.
	// Keep such an interval unavailable instead of accepting a partial total.
	tail := bytes.TrimSpace(data)
	if i := bytes.LastIndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	if !json.Valid(tail) {
		return nil, Usage{}, fmt.Errorf("owner transcript has no complete trailing record")
	}
	usage, err := parseTranscript(provider, bytes.NewReader(data))
	return data, usage, err
}

func transcriptDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// RecordOwner records this run's terminal interval at most once.
func RecordOwner(run *RunCost, entry OwnerCost) {
	if run == nil || run.Owner != nil {
		return
	}
	run.Owner = &entry
	run.addTokens(entry.TotalInputTokens, entry.InputCacheHitTokens, entry.InputCacheMissTokens, entry.OutputTokens)
}

// AddChildOwner adds a frozen child interval without assigning this run's Owner.
// The receipt owner must admit that child exactly once in the same transaction.
func AddChildOwner(run *RunCost, owner *OwnerCost) {
	if run == nil || owner == nil {
		return
	}
	run.addTokens(owner.TotalInputTokens, owner.InputCacheHitTokens, owner.InputCacheMissTokens, owner.OutputTokens)
}
