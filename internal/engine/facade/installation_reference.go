package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Global installation resources carry request locations, never workflow state.
// Callers hold the original admission window before publishing or removing one.
func (admission Admission) runReferencePath(stateDir string) (string, error) {
	data, err := os.ReadFile(admission.RegistryPath)
	if err != nil {
		return "", err
	}
	var doc struct {
		Records []struct {
			ID             string            `json:"id"`
			Scope          string            `json:"scope"`
			ResourceRoot   string            `json:"resourceRoot"`
			CanonicalPaths map[string]string `json:"canonicalPaths"`
		} `json:"records"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	for _, record := range doc.Records {
		if record.ID != admission.InstalledTargetIdentity {
			continue
		}
		if record.Scope != "global" {
			return "", nil
		}
		if !filepath.IsAbs(record.ResourceRoot) {
			return "", fmt.Errorf("UNREGISTERED_INSTALL: global resource root is unavailable")
		}
		root, err := filepath.EvalSymlinks(admission.Root)
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(admission.Root, stateDir)
		if err != nil {
			return "", err
		}
		request := filepath.Join(root, relative, "request.json")
		sum := sha256.Sum256([]byte(request))
		return filepath.Join(record.CanonicalPaths["resourceRoot"], "engine-runs", hex.EncodeToString(sum[:])+".json"), nil
	}
	return "", fmt.Errorf("UNREGISTERED_INSTALL: owning installation record is unavailable")
}

func (admission Admission) removeRunReference(stateDir string) error {
	path, err := admission.runReferencePath(stateDir)
	if err != nil || path == "" {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
