package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileProofMatchesContentHash(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body []byte
	}{
		{"empty", nil},
		{"one byte", []byte("x")},
		{"ascii text", []byte("hello\n")},
		{"utf8 non-bmp", []byte("日本語 🚀 café\n")},
		{"nul bytes", []byte{0x00, 0x01, 0xff, 0xfe, 0x00}},
		{"crlf", []byte("a\r\nb\r\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".bin")
			if err := os.WriteFile(path, tc.body, 0o644); err != nil {
				t.Fatal(err)
			}
			proof, refusal := FileProof(path)
			if refusal != "" {
				t.Fatalf("refused: %s", refusal)
			}
			sum := sha256.Sum256(tc.body)
			if proof["sha256"] != hex.EncodeToString(sum[:]) {
				t.Fatalf("sha256 %v", proof["sha256"])
			}
			if proof["size"] != len(tc.body) {
				t.Fatalf("size %v, want %d", proof["size"], len(tc.body))
			}
		})
	}
}

func TestFileProofLargeFile(t *testing.T) {
	dir := t.TempDir()
	body := make([]byte, 1<<20+7)
	for i := range body {
		body[i] = byte(i % 251)
	}
	path := filepath.Join(dir, "large.bin")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	proof, refusal := FileProof(path)
	if refusal != "" {
		t.Fatalf("refused: %s", refusal)
	}
	sum := sha256.Sum256(body)
	if proof["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatal("large file digest differs from the same bytes hashed directly")
	}
	if proof["size"] != len(body) {
		t.Fatalf("size %v, want %d", proof["size"], len(body))
	}
}

func TestFileProofFollowsSymlinkAndUnicodeNames(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("linked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	direct, _ := FileProof(target)
	viaLink, refusal := FileProof(link)
	if refusal != "" {
		t.Fatalf("refused: %s", refusal)
	}
	if viaLink["sha256"] != direct["sha256"] || viaLink["size"] != direct["size"] {
		t.Fatal("a symlink must proof as its target content")
	}

	named := filepath.Join(dir, "日本語 🚀.txt")
	if err := os.WriteFile(named, []byte("named\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, refusal := FileProof(named); refusal != "" {
		t.Fatalf("unicode file name refused: %s", refusal)
	}
}

func TestFileProofRefusals(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.txt")
	proof, refusal := FileProof(missing)
	if proof != nil {
		t.Fatal("refusal must not return a proof")
	}
	if refusal != "EVIDENCE_MISSING: "+missing {
		t.Fatalf("refusal %q", refusal)
	}

	// A directory is not evidence.
	if _, refusal := FileProof(dir); !strings.HasPrefix(refusal, "EVIDENCE_MISSING: ") {
		t.Fatalf("directory refusal %q", refusal)
	}

	// A broken symlink is missing evidence.
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "nowhere"), broken); err == nil {
		if _, refusal := FileProof(broken); !strings.HasPrefix(refusal, "EVIDENCE_MISSING: ") {
			t.Fatalf("broken symlink refusal %q", refusal)
		}
	}
}

func TestFileProofIsDeterministic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "same.txt")
	if err := os.WriteFile(path, []byte("stable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, _ := FileProof(path)
	for i := 0; i < 5; i++ {
		again, refusal := FileProof(path)
		if refusal != "" || again["sha256"] != first["sha256"] || again["size"] != first["size"] {
			t.Fatalf("iteration %d differed", i)
		}
	}
}
