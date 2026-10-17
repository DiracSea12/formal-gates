package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"formal-gates/internal/settings"
)

func TestConfiguredPlatformPathsStayScopedToTheirOwnBinding(t *testing.T) {
	t.Parallel()
	native, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	native, err = filepath.EvalSymlinks(native)
	if err != nil {
		t.Fatal(err)
	}
	payload := readBytes(t, native)
	for _, provider := range []string{"claude", "codex", "cursor", "dsh", "zcode"} {
		t.Run(provider, func(t *testing.T) {
			root := resolveNativePath(t.TempDir())
			executable := filepath.Join(root, "native-cli")
			if runtime.GOOS == "windows" {
				executable += ".exe"
			}
			if err := os.WriteFile(executable, payload, 0o700); err != nil {
				t.Fatal(err)
			}
			home := filepath.Join(root, "native-home")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			options := map[string]string{provider + "Executable": executable}
			if provider != "dsh" {
				options[provider+"Home"] = home
			}
			if provider == "zcode" {
				script := filepath.Join(root, "server.cjs")
				os.WriteFile(script, []byte("native source"), 0600)
				options["zcodeScript"] = script
			}
			originalHome := os.Getenv("HOME")
			frozen, err := settings.Freeze(provider, settings.Effective{Values: settings.Values{Native: options}})
			if err != nil {
				t.Fatal(err)
			}
			binding, err := NativeBinding(provider, frozen.Values)
			if err != nil {
				t.Fatal(err)
			}
			if getOf(getOf(binding, "launch"), "executable") != executable {
				t.Fatalf("configured executable not bound: %v", binding)
			}
			if provider != "dsh" && getOf(getOf(binding, "environment"), "HOME") != home {
				t.Fatalf("configured native home not bound: %v", binding)
			}
			if provider == "zcode" && getOf(getOf(binding, "launch"), "script") != options["zcodeScript"] {
				t.Fatal("configured script ignored")
			}
			if os.Getenv("HOME") != originalHome {
				t.Fatal("configured native binding changed process-global environment")
			}
		})
	}
}
