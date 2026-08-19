package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPackageDigestIgnoresSelfReferentialManifestField(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "formal-gates.manifest.json")
	initial := []byte(`{"name":"formal-gates","package_parts":["definitions/"]}` + "\n")
	if err := os.WriteFile(manifestPath, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := PackageDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(initial, &document); err != nil {
		t.Fatal(err)
	}
	document["package_digest"] = before
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(updated, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := PackageDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("manifest package digest field changed package identity: before=%s after=%s", before, after)
	}
	document["name"] = "modified"
	modified, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(modified, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := PackageDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed == before {
		t.Fatal("substantive manifest modification did not change package identity")
	}
}
