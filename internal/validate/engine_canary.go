package validate

import (
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

// The portable installed check proves launcher admission, current intake and
// lightweight terminal persistence. Regular's model/QA acceptance is run by the
// separately documented native integration; this check does not fabricate it.
func runInstalledEngineCanary(packageRoot, root, registry, recordID string) error {
	requirement := []byte("# Canary\n\n## 需求点\n\n### REQ-001：记录\n\n#### 要求\n保留确认过的需求。\n\n#### 验收条件\n- AC-001：轻量路线保存需求与未验证标记。\n\n#### 来源\n安装回归。\n")
	if err := os.WriteFile(filepath.Join(root, "requirement.md"), requirement, 0600); err != nil {
		return err
	}
	runner := execNativeCommandRunner{}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "canary@example.invalid"}, {"config", "user.name", "Installed engine canary"}, {"add", "requirement.md"}, {"-c", "core.hooksPath=/dev/null", "commit", "-qm", "Canary input"}} {
		if _, err := runner.Run(root, "git", args...); err != nil {
			return err
		}
	}
	admitted, err := ResolveEngineAdmissionByRecord(recordID)
	if err != nil {
		return err
	}
	if admitted.RegistryPath != registry {
		return fmt.Errorf("canary registry binding differs")
	}
	f, _, err := facade.Start(facade.StartOptions{Root: root, PackageRoot: packageRoot, Request: facade.StartRequest{RunID: "canary", RequirementPath: "requirement.md"}, Admission: &facade.Admission{RegistryPath: registry, PackageDigest: admitted.PackageDigest, InstalledTargetIdentity: recordID, Generation: admitted.Generation, Lease: admitted.Lease, Token: admitted.Token}})
	if err != nil {
		return err
	}
	if _, err = f.Drive(); err != nil {
		return err
	}
	for _, q := range []protocol.RegularRequest{{Action: "confirm-requirement"}, {Action: "mode", Mode: "lightweight"}} {
		r, err := f.Show()
		if err != nil {
			return err
		}
		event, _ := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("canary-request-%d", r.Revision)), protocol.RegularEvent{Operation: "REQUEST", ExpectedRevision: r.Revision, Request: &q})
		_, r, err = f.Submit(event)
		if err != nil {
			return err
		}
		ask := r.Regular.Ask
		event, _ = protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("canary-decision-%d", r.Revision)), protocol.RegularEvent{Operation: "DECIDE", ExpectedRevision: r.Revision, Decision: &protocol.RegularDecision{RequestID: ask.ID, Token: ask.Token, Digest: ask.Digest, Choice: "confirm"}})
		if _, _, err = f.Submit(event); err != nil {
			return err
		}
	}
	r, err := f.Drive()
	if err != nil {
		return err
	}
	if r.Status != "COMPLETE" || !r.Unverified {
		return fmt.Errorf("current lightweight terminal is missing")
	}
	return nil
}
