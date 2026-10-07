package validate

import (
	"fmt"

	"os"
	"path/filepath"

	"strings"
)

type FindingInput struct {
	Severity          string
	SeverityRationale string
	Category          string
	Requirement       string
	NormalPath        string
	ActualError       string
	DeferralImpact    string
	Verification      FindingVerification
	Message           string
	Locations         []string
}

// verifyRegistryBindingRecord 校验 record 与 workflow root/package root 及启
// 动器身份的绑定关系。不含 registry 加载与安装目标摘要核验：二者由调用方
// 在上游完成，避免同一验证链内重复读盘与重复全树哈希。
func verifyRegistryBindingRecord(record RegistryRecord, root, packageRoot string) error {
	canonicalRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	canonicalPackage, err := filepath.Abs(packageRoot)
	if err != nil {
		return err
	}
	// A project-scope record is bound to one repository root.  A global
	// installation is intentionally reusable across projects, so its
	// projectRoot records the host-level installation namespace rather than
	// the arbitrary repository that is invoking the stable driver.
	if record.Scope == "project" {
		if expected := canonicalRegistryPath(record.CanonicalPaths["projectRoot"]); expected != "." && expected != canonicalRegistryPath(canonicalRoot) {
			return fmt.Errorf("UNREGISTERED_INSTALL: registry project root does not match workflow root")
		}
	}
	if canonicalRegistryPath(record.Target) != canonicalRegistryPath(canonicalPackage) || canonicalRegistryPath(record.CanonicalPaths["target"]) != canonicalRegistryPath(canonicalPackage) {
		return fmt.Errorf("UNREGISTERED_INSTALL: registry target does not match package root")
	}
	// Installed packages are only allowed to drive workflow writes through
	// the fixed launcher recorded by admission.  A direct invocation of the
	// candidate binary under the host target has the same package binding but
	// a different executable identity and is therefore rejected.  Repository
	// checkouts keep the legacy source-tree path and intentionally do not use
	// this installed-launcher check.
	executable, executableErr := os.Executable()
	if executableErr != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: cannot resolve invoking launcher: %w", executableErr)
	}
	base := filepath.Base(filepath.Clean(executable))
	if !strings.HasSuffix(base, ".test") && !strings.HasSuffix(base, ".test.exe") && !launcherInvocationMatches(record.LauncherPath) {
		return fmt.Errorf("UNREGISTERED_INSTALL: workflow must be driven by stable launcher %s", record.LauncherPath)
	}
	return nil
}

func verifyRegisteredTargetIdentity(record RegistryRecord) error {
	if err := assertInstallSource(record.Target); err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: registered target is not an installed artifact: %w", err)
	}
	if strings.TrimSpace(record.InstalledDigest) == "" {
		return nil
	}
	receipt, err := PackageReceipt(record.Target)
	if err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: registered target identity cannot be read: %w", err)
	}
	if receipt.Digest != record.InstalledDigest {
		return fmt.Errorf("UNREGISTERED_INSTALL: registered target digest is stale")
	}
	return nil
}

func validateSemanticResult(status, message string, findings []FindingInput) (string, []Finding, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "PASS" && status != "FAIL" && status != "RUNTIME_ERROR" {
		return "", nil, fmt.Errorf("status must be PASS, FAIL, or RUNTIME_ERROR")
	}
	if status == "FAIL" && len(findings) == 0 {
		return "", nil, fmt.Errorf("FAIL requires at least one finding")
	}
	if status == "RUNTIME_ERROR" {
		if len(findings) != 0 {
			return "", nil, fmt.Errorf("RUNTIME_ERROR cannot include reviewer findings")
		}
		if strings.TrimSpace(message) == "" {
			return "", nil, fmt.Errorf("RUNTIME_ERROR requires a message")
		}
	}
	converted := make([]Finding, 0, len(findings))
	hasBlocking := false
	for _, input := range findings {
		if strings.TrimSpace(input.Message) == "" {
			return "", nil, fmt.Errorf("finding message is required")
		}
		locations := make([]string, 0, len(input.Locations))
		for _, location := range input.Locations {
			if err := validateFindingLocation(location); err != nil {
				return "", nil, err
			}
			locations = append(locations, strings.TrimSpace(location))
		}
		severity := strings.ToUpper(strings.TrimSpace(input.Severity))
		if severity != "P0" && severity != "P1" && severity != "P2" && severity != "P3" {
			return "", nil, fmt.Errorf("gate finding severity must be P0, P1, P2, or P3")
		}
		if severity == "P0" || severity == "P1" {
			hasBlocking = true
		}
		finding := Finding{Severity: severity, SeverityRationale: strings.TrimSpace(input.SeverityRationale), Category: strings.ToUpper(strings.TrimSpace(input.Category)), Requirement: strings.TrimSpace(input.Requirement), NormalPath: strings.TrimSpace(input.NormalPath), ActualError: strings.TrimSpace(input.ActualError), DeferralImpact: strings.TrimSpace(input.DeferralImpact), Verification: input.Verification, Message: strings.TrimSpace(input.Message), Locations: locations}
		finding.ID = findingStableID(finding)
		converted = append(converted, finding)
	}
	if status == "PASS" && hasBlocking {
		return "", nil, fmt.Errorf("PASS can include only P2/P3 findings")
	}
	if status == "FAIL" && !hasBlocking {
		return "", nil, fmt.Errorf("FAIL requires at least one P0 or P1 finding")
	}
	return status, converted, nil
}

func validateFindingLocation(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("finding location is empty")
	}
	if strings.Contains(value, `\`) || strings.Contains(value, "://") || strings.HasPrefix(value, "/") || (len(value) > 1 && value[1] == ':') {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	path := value
	for count := 0; count < 2; count++ {
		index := strings.LastIndex(path, ":")
		if index <= 0 {
			break
		}
		if !suffixIsDigits(path[index+1:]) {
			break
		}
		path = path[:index]
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	return nil
}

func suffixIsDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func absPath(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(full)
}
