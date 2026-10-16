package validate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/testkit"
)

func TestDshBootstrapChecksInstalledHookPayload(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	for _, tc := range []struct {
		name      string
		scope     string
		skipHooks bool
	}{
		{name: "global", scope: "global"},
		{name: "global_skip_hooks", scope: "global", skipHooks: true},
		{name: "project", scope: "project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := copyPackageFixture(t)
			home, project := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("DSH_HOME", filepath.Join(home, "dsh"))
			patch := filepath.Join(home, "dsh", dshPatchFileName)
			mustWriteValidateTest(t, patch, "# user patch\n- modify: user-plugin\n  config:\n    value: preserved\n")
			options := InstallOptions{
				Source: source, Host: "dsh", Scope: tc.scope, Project: project,
				RegistryPath: filepath.Join(home, "registry.json"),
				BinaryTarget: filepath.Join(home, "stable", nativeBinaryName()),
				Force:        true, SkipHooks: tc.skipHooks,
			}
			report, err := Install(options)
			if err != nil {
				t.Fatal(err)
			}
			target := report.Targets[0].TargetPath
			plugin := filepath.Join(target, filepath.FromSlash(dshPluginFileRel))
			hasPlugin := tc.scope == "global" && !tc.skipHooks
			if data, err := os.ReadFile(plugin); hasPlugin {
				if err != nil || string(data) != dshPluginSource {
					t.Fatalf("installed plugin bytes differ: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("hook-free installation contains a plugin: %v", err)
			}
			before, err := PackageReceipt(target)
			if err != nil {
				t.Fatal(err)
			}
			patchBefore, err := os.ReadFile(patch)
			if err != nil {
				t.Fatal(err)
			}
			registryBefore, err := os.ReadFile(options.RegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			options.Bootstrap = true
			for attempt := 0; attempt < 2; attempt++ {
				bootstrap, err := Install(options)
				if err != nil {
					t.Fatalf("bootstrap rejected installed DSH package: %v", err)
				}
				data, err := os.ReadFile(bootstrap.BootstrapReceiptPath)
				if err != nil {
					t.Fatal(err)
				}
				var receipt BootstrapReceipt
				if err := json.Unmarshal(data, &receipt); err != nil {
					t.Fatal(err)
				}
				if !receipt.Accepted || receipt.StateCreated || receipt.PackageDigest != report.PackageDigest || len(receipt.Records) != 1 || receipt.Records[0].InstalledDigest != before.Digest {
					t.Fatalf("bootstrap did not bind original package and installed bytes: %+v", receipt)
				}
			}
			if after, err := PackageReceipt(target); err != nil || after.Digest != before.Digest {
				t.Fatalf("bootstrap changed installed bytes: %v", err)
			}
			for path, want := range map[string][]byte{patch: patchBefore, options.RegistryPath: registryBefore} {
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("bootstrap changed %s: %v", path, err)
				}
			}
			if !hasPlugin {
				return
			}
			selfOptions := options
			selfOptions.Source = target
			selfOptions.RegistryPath = filepath.Join(home, "self-bootstrap-registry.json")
			if _, err := Install(selfOptions); err != nil {
				t.Fatalf("bootstrap rejected its own installed DSH package: %v", err)
			}
			if err := os.WriteFile(plugin, []byte(dshPluginSource+"\nchanged\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Install(options); err == nil || !strings.Contains(err.Error(), "bootstrap target differs from source package") {
				t.Fatalf("bootstrap admitted altered plugin bytes: %v", err)
			}
			selfOptions.RegistryPath = filepath.Join(home, "altered-self-bootstrap-registry.json")
			if _, err := Install(selfOptions); err == nil || !strings.Contains(err.Error(), "bootstrap target differs from source package") {
				t.Fatalf("self bootstrap admitted altered plugin bytes: %v", err)
			}
			if got, err := os.ReadFile(plugin); err != nil || string(got) != dshPluginSource+"\nchanged\n" {
				t.Fatalf("rejected bootstrap changed plugin bytes: %v", err)
			}
			for path, want := range map[string][]byte{patch: patchBefore, options.RegistryPath: registryBefore} {
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("rejected bootstrap changed %s: %v", path, err)
				}
			}
		})
	}
}

func TestDshPluginSourceOwnsRequiredInterceptionSurface(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("dsh_plugin.mjs")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"export const name = 'formal-gates-dsh'",
		"ctx.on('tools/pre-execute'",
		"['hook', 'decide', '--provider', config.provider]",
		"normalizeArguments",
		"process.platform",
		"permissionDecision",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("dsh_plugin.mjs missing required behavior %q", required)
		}
	}
	for _, retired := range []string{"subagent/start", "subagent/end", "lifecyclePayload", "'capture'", "subagentChildren"} {
		if strings.Contains(source, retired) {
			t.Fatalf("retired lifecycle callback remains: %s", retired)
		}
	}

}

// DSH anchors inserted plugin names beside the patch, not beside the profile.
func TestDshPluginResolvesBesideHomePatch(t *testing.T) {
	t.Parallel()
	home := filepath.Join(t.TempDir(), "custom dsh home")
	target := installTarget{
		targetPath: filepath.Join(home, "skills", "formal-gates"),
		hookConfig: filepath.Join(home, dshPatchFileName),
	}
	if _, err := configureDshHook(target); err != nil {
		t.Fatal(err)
	}
	doc, err := readDshPatch(target.hookConfig)
	if err != nil {
		t.Fatal(err)
	}
	row := dshMappingNode(doc.Content[0].Content[0], "insert").Content[0]
	name := dshMappingValue(row, "name")
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target.hookConfig), filepath.FromSlash(name)))
	if resolved != dshPluginPath(target) {
		t.Fatalf("DSH resolves plugin to %q, installed at %q", resolved, dshPluginPath(target))
	}
	if _, err := os.Stat(resolved); err != nil {
		t.Fatal(err)
	}
}

func TestDshHookReinstallPreservesUnchangedPatch(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	target := installTarget{host: "dsh", targetPath: filepath.Join(home, "skills", "formal-gates"), hookConfig: filepath.Join(home, dshPatchFileName)}
	if changed, err := configureInstallHook(target); err != nil || !changed {
		t.Fatalf("initial hook installation changed=%t err=%v", changed, err)
	}
	before, err := os.ReadFile(target.hookConfig)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1234567890, 0)
	if err := os.Chtimes(target.hookConfig, when, when); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target.hookConfig)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := configureInstallHook(target); err != nil || changed {
		t.Fatalf("unchanged hook installation changed=%t err=%v", changed, err)
	}
	after, err := os.ReadFile(target.hookConfig)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("unchanged patch bytes changed: %v", err)
	}
	current, err := os.Stat(target.hookConfig)
	if err != nil || !current.ModTime().Equal(info.ModTime()) {
		t.Fatalf("unchanged patch modification time changed: %v", err)
	}
	if err := removeInstallHooks(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target.hookConfig); !os.IsNotExist(err) {
		t.Fatalf("installer-only patch remains after uninstall: %v", err)
	}
}
