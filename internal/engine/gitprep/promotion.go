package gitprep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PromotionReceipt records the full native validation view on both sides. A new
// commit identity may carry the validated evidence only when every path, blob,
// mode and provider setting remains equivalent; tree similarity is insufficient.
type PromotionReceipt struct {
	OperationID        string `json:"operationId"`
	ExpectedTargetBase string `json:"expectedTargetBase"`
	Source             View   `json:"source"`
	Target             View   `json:"target"`
	Ref                string `json:"ref"`
	Status             string `json:"status"`
	ConflictFree       bool   `json:"conflictFree"`
}

func ValidatePromotion(r PromotionReceipt, source View, base, operation string) error {
	if r.OperationID != operation || r.ExpectedTargetBase != base || r.Source.Identity != source.Identity || r.Source.Digest != source.Digest || r.Ref == "" || r.Status != "COMPLETE" || !r.ConflictFree {
		return fmt.Errorf("promotion receipt has incomplete, stale, conflicting or UNKNOWN native binding")
	}
	if r.Source.Tree == "" || r.Target.Tree == "" || r.Target.Identity == "" || r.Source.Digest == "" || r.Source.Digest != r.Target.Digest || Digest(r.Source.Entries) != Digest(r.Target.Entries) || Digest(r.Source.ProviderMetadata) != Digest(r.Target.ProviderMetadata) {
		return fmt.Errorf("promotion validation views are not exactly equivalent")
	}
	return nil
}

// Promote moves the entire already validated candidate, never a test patch. The
// supported Git path uses fast-forward and keeps identity. A crash after merge
// can be reconciled from the exact source identity without invoking merge again.
func Promote(root, source, expectedBase, operation string) (receipt PromotionReceipt, err error) {
	receipt = PromotionReceipt{OperationID: operation, ExpectedTargetBase: expectedBase, Status: "UNKNOWN"}
	receipt.Source, err = Snapshot(root, source)
	if err != nil {
		return receipt, err
	}
	ref, err := Git(root, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		ref = "HEAD"
	}
	receipt.Ref = ref
	head, err := Head(root)
	if err != nil {
		return receipt, err
	}
	if head != source {
		if head != expectedBase {
			return receipt, fmt.Errorf("EXTERNAL_CHANGED: promotion target drifted from %s to %s", expectedBase, head)
		}
		// Runtime resources and unrelated untracked files are not delivery content.
		// Git itself refuses an untracked path that would be overwritten by merge.
		status, err := Git(root, "status", "--porcelain", "--untracked-files=no")
		if err != nil {
			return receipt, err
		}
		var matching []string
		if status != "" {
			changed, err := Git(root, "diff", "--name-only", "HEAD", "--")
			if err != nil {
				return receipt, err
			}
			for _, path := range strings.Split(changed, "\n") {
				if path == "" {
					continue
				}
				matching = append(matching, path)
				delta, err := Git(root, "diff", source, "--", path)
				if err != nil || delta != "" {
					return receipt, fmt.Errorf("promotion target modification %s differs from verified candidate", path)
				}
			}
		}
		// A target already at the exact committed validation view needs no merge.
		// Its distinct native identity is retained in the equivalence receipt;
		// the source evidence is never rewritten to claim it ran at this target.
		target, err := Snapshot(root, head)
		if err != nil {
			return receipt, err
		}
		if status == "" && target.Digest == receipt.Source.Digest {
			receipt.Target = target
			receipt.Status = "COMPLETE"
			receipt.ConflictFree = true
			return receipt, ValidatePromotion(receipt, receipt.Source, expectedBase, operation)
		}
		// Confirmed document edits already equal the validated candidate, but Git
		// will still refuse to overwrite them. Restore only those proven identical
		// paths before fast-forward; the persisted promotion intent can recover this
		// boundary. On an ordinary merge error, restore their desired candidate bytes.
		if len(matching) > 0 {
			restoreBytes, captureErr := captureMatchingFiles(root, matching)
			if captureErr != nil {
				return receipt, captureErr
			}
			// Git checkout filters can change confirmed document bytes even
			// though the canonical blob is identical to the approved candidate.
			defer func() {
				if restoreErr := restoreBytes(); restoreErr != nil {
					err = fmt.Errorf("UNKNOWN: promotion=%v, restore confirmed working bytes: %w", err, restoreErr)
				}
			}()
			if _, err = Git(root, append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, matching...)...); err != nil {
				return receipt, err
			}
		}
		if _, err = Git(root, "merge", "--ff-only", source); err != nil {
			if len(matching) > 0 {
				if _, restoreErr := Git(root, append([]string{"restore", "--source=" + source, "--worktree", "--"}, matching...)...); restoreErr != nil {
					return receipt, fmt.Errorf("UNKNOWN: merge failed (%v), document restore failed (%v)", err, restoreErr)
				}
			}
			return receipt, err
		}
	}
	receipt.Target, err = Snapshot(root, "HEAD")
	if err != nil {
		return receipt, err
	}
	receipt.Status = "COMPLETE"
	receipt.ConflictFree = true
	return receipt, ValidatePromotion(receipt, receipt.Source, expectedBase, operation)
}

func captureMatchingFiles(root string, paths []string) (func() error, error) {
	type file struct {
		path string
		data []byte
		mode os.FileMode
	}
	var files []file
	for _, path := range paths {
		path = filepath.Join(root, path)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue // A matching deletion has no working bytes to preserve.
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue // Symlink targets are materialized directly by Git.
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, file{path, data, info.Mode().Perm()})
	}
	return func() error {
		for _, file := range files {
			if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
				return err
			}
		}
		return nil
	}, nil
}
