package cli

import (
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/worker"
	"formal-gates/internal/validate"
)

func TestStandaloneResultArgumentsConstructSemanticFindings(t *testing.T) {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	a := registerStandaloneResultArguments(fs)
	message := "中文引号 \"证据\"\n第二行"
	if err := fs.Parse([]string{
		"--review-complete", "--message", message,
		"--finding-severity", "P1", "--finding-message", message,
		"--finding-severity-rationale", "核心边界", "--finding-category", "correctness",
		"--finding-requirement", "公开行为的语义要求", "--finding-normal-path", "正常操作",
		"--finding-actual-error", "实际错误", "--finding-deferral-impact", "延期影响",
		"--finding-verify-scope", "范围事实", "--finding-verify-severity", "定级事实",
		"--finding-verify-causality", "因果事实", "--finding-verify-reproduction", "复现事实",
		"--finding-verify-classification", "分类事实", "--finding-read", "1", "--finding-line", "2",
		"--finding-listing", "2", "--finding-entry", "1",
		"--finding-severity", "P3", "--finding-message", "独立建议", "--finding-read", "1",
	}); err != nil {
		t.Fatal(err)
	}
	body, provided, err := a.input()
	if err != nil || !provided {
		t.Fatal(provided, err)
	}
	context := validate.StandaloneGateContext{LocationFacts: []worker.LocationFact{
		{Kind: "read", Paths: []string{"original.go"}, Content: "first\nsecond\n"},
		{Kind: "listing", Paths: []string{"removed.go"}},
	}}
	result, err := validate.ValidateStandaloneGateResult(body, context)
	if err != nil || result.Status != "FAIL" || result.Message != message || len(result.Findings) != 2 {
		t.Fatal(result, err)
	}
	f := result.Findings[0]
	if f.Message != message || f.SeverityRationale != "核心边界" || f.Category != "correctness" || f.Requirement != "公开行为的语义要求" || f.NormalPath != "正常操作" || f.ActualError != "实际错误" || f.DeferralImpact != "延期影响" {
		t.Fatal(f)
	}
	if !reflect.DeepEqual(f.Locations, []string{"original.go:2", "removed.go"}) || f.Verification.Scope != "范围事实" || f.Verification.Severity != "定级事实" || f.Verification.Causality != "因果事实" || f.Verification.Reproduction != "复现事实" || f.Verification.Classification != "分类事实" {
		t.Fatal(f)
	}
	if !reflect.DeepEqual(result.Findings[1].Locations, []string{"original.go"}) {
		t.Fatal("finding locations crossed findings", result.Findings)
	}
}

func TestStandaloneResultArgumentsUseExistingReviewValidation(t *testing.T) {
	for _, item := range []struct {
		name, status, errorText string
		args                    []string
	}{
		{name: "no semantic arguments"},
		{name: "completed without findings", status: "PASS", args: []string{"--review-complete"}},
		{name: "actual interruption", status: "RUNTIME_ERROR", args: []string{"--interruption-reason", "无法完成\n\"原原因\""}},
		{name: "missing judgment", errorText: "requires findings", args: []string{"--message", "summary"}},
		{name: "mixed interruption and finding", errorText: "cannot include", args: []string{"--interruption-reason", "reason", "--finding-severity", "P3", "--finding-message", "finding"}},
		{name: "unpublished source", errorText: "original published", args: []string{"--finding-severity", "P3", "--finding-message", "finding", "--finding-read", "9"}},
	} {
		t.Run(item.name, func(t *testing.T) {
			fs := flag.NewFlagSet("review", flag.ContinueOnError)
			a := registerStandaloneResultArguments(fs)
			if err := fs.Parse(item.args); err != nil {
				t.Fatal(err)
			}
			body, provided, err := a.input()
			if err != nil || provided != (len(item.args) != 0) {
				t.Fatal(provided, err)
			}
			if !provided {
				return
			}
			result, err := validate.ValidateStandaloneGateResult(body, validate.StandaloneGateContext{})
			if item.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), item.errorText) {
					t.Fatal(result, err)
				}
			} else if err != nil || result.Status != item.status {
				t.Fatal(result, err)
			}
		})
	}
	for _, args := range [][]string{{"--finding-message", "unbound"}, {"--finding-read", "1"}, {"--finding-severity", "P3", "--finding-line", "1"}, {"--finding-severity", "P3", "--finding-read", "0"}} {
		fs := flag.NewFlagSet("review", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerStandaloneResultArguments(fs)
		if err := fs.Parse(args); err == nil {
			t.Fatal("unbound or invalid position accepted", args)
		}
	}
}
