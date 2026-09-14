package facade

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

type workBuddyRuntimeAdapter struct{ streamRuntimeAdapter }

func (workBuddyRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	executable := config.NativeString("workbuddyExecutable")
	if executable == "codebuddy" {
		if _, err := exec.LookPath(executable); err != nil {
			candidate := "/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy"
			if _, err := os.Stat(candidate); err == nil {
				executable = candidate
			}
		}
	}
	home := config.NativeString("workbuddyHome")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if err := workBuddyHomeAvailable(home); err != nil {
		return nil, err
	}
	nativeHome := os.Getenv("WORKBUDDY_CONFIG_DIR")
	if nativeHome == "" {
		nativeHome = filepath.Join(home, ".workbuddy")
	}
	nativeHome, err = filepath.Abs(nativeHome)
	if err != nil {
		return nil, err
	}
	product := config.NativeString("workbuddyProductConfig")
	if product == "" {
		product = os.Getenv("ACC_PRODUCT_CONFIG_PATH")
	}
	if product == "" {
		product = filepath.Join(nativeHome, "cache", "acc-product-config-v3.json")
	}
	product, err = filepath.Abs(product)
	if err != nil {
		return nil, err
	}
	if err := workBuddyProductConfigAvailable(product); err != nil {
		return nil, err
	}
	// Native desktop authentication is rooted in os.homedir(), independently of
	// CODEBUDDY_CONFIG_DIR. An isolated engine installation must bind the existing
	// native home rather than copy authentication files into its own HOME.
	environment := map[string]string{
		"HOME":                 home,
		"CODEBUDDY_CONFIG_DIR": nativeHome, "WORKBUDDY_CONFIG_DIR": nativeHome,
		"ACC_PRODUCT_CONFIG_PATH": product,
	}
	if runtime.GOOS == "windows" {
		environment["USERPROFILE"] = home
	}
	return captureStreamContext("workbuddy", executable, environment)
}
func (workBuddyRuntimeAdapter) validate(binding *protocol.HostRuntimeContext) error {
	if err := validateStreamContext(binding, "workbuddy"); err != nil {
		return err
	}
	if home := binding.Environment["HOME"]; home != "" {
		if err := workBuddyHomeAvailable(home); err != nil {
			return err
		}
	}
	if path := binding.Environment["ACC_PRODUCT_CONFIG_PATH"]; path != "" {
		return workBuddyProductConfigAvailable(path)
	}
	return nil
}

func workBuddyHomeAvailable(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("WORKBUDDY_CONFIG: native home directory unavailable at %s; select the existing WorkBuddy user's home with workbuddyHome", path)
	}
	return nil
}

func workBuddyProductConfigAvailable(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("WORKBUDDY_CONFIG: native desktop product configuration unavailable at %s; start/sign in to WorkBuddy or set workbuddyProductConfig to its native file", path)
	}
	return nil
}

func (workBuddyRuntimeAdapter) probe(q nativeProbe) (string, json.RawMessage, error) {
	present, err := workBuddySessionRecordAvailable(q.Context, q.Identity)
	raw, _ := json.Marshal(map[string]any{"sessionId": q.Identity, "present": present})
	if err != nil {
		return "session/store", raw, err
	}
	if !present {
		return "session/store", raw, fmt.Errorf("WORKBUDDY_RECOVERY: native session record is unavailable")
	}
	return "session/store", raw, nil
}

// WorkBuddy's print-mode sessions and ACP sessions use different control
// surfaces. Recovery therefore checks the native print-session store and lets
// the subsequent --resume invocation prove actual resumability. The adapter
// never reads or copies transcript contents during this capability check.
func workBuddySessionRecordAvailable(binding *protocol.HostRuntimeContext, identity string) (bool, error) {
	if binding == nil || identity == "" || filepath.Base(identity) != identity {
		return false, fmt.Errorf("WORKBUDDY_RECOVERY: exact native identity is required")
	}
	configRoot := binding.Environment["CODEBUDDY_CONFIG_DIR"]
	if !filepath.IsAbs(configRoot) {
		return false, fmt.Errorf("WORKBUDDY_RECOVERY: native config root is unavailable")
	}
	projects := filepath.Join(configRoot, "projects")
	entries, err := os.ReadDir(projects)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	matches := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, statErr := os.Stat(filepath.Join(projects, entry.Name(), identity+".jsonl"))
		if statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
			matches++
		}
	}
	if matches > 1 {
		return false, fmt.Errorf("WORKBUDDY_RECOVERY: native identity is ambiguous across project stores")
	}
	return matches == 1, nil
}
func requestedNativeID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 15) | 64
	value[8] = (value[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func init() { registerBuiltinRuntime("workbuddy", workBuddyRuntimeAdapter{}) }

func (workBuddyRuntimeAdapter) readIdentity(job, action string) (string, error) {
	file, err := os.Open(filepath.Join(job, "stdout.jsonl"))
	if err != nil {
		return "", err
	}
	defer file.Close()
	observed, err := observeWorkBuddyResult(file, "")
	if observed.Identity != "" {
		return observed.Identity, nil
	}
	return "", err
}
