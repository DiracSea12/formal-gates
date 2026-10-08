package coverage

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestManifestOperationsValidateVariants(t *testing.T) {
	for name, operation := range map[string]Operation{
		"argv":          {Kind: OperationArgv, Argv: []string{"tool", "", "a b", `"quoted"`, "$value"}, Stdin: "a\n\x00b"},
		"shell":         {Kind: OperationShell, Command: "printf '%s' \"$value\"\n", Stdin: "input\n"},
		"missing kind":  {Argv: []string{"tool"}},
		"unknown kind":  {Kind: "EXEC", Argv: []string{"tool"}},
		"missing argv":  {Kind: OperationArgv},
		"empty program": {Kind: OperationArgv, Argv: []string{"", "arg"}},
		"missing shell": {Kind: OperationShell},
		"argv mixed":    {Kind: OperationArgv, Argv: []string{"tool"}, Command: "tool"},
		"shell mixed":   {Kind: OperationShell, Argv: []string{""}, Command: "tool"},
	} {
		t.Run(name, func(t *testing.T) {
			wantValid := name == "argv" || name == "shell"
			for _, shared := range []bool{false, true} {
				c, _ := fixtureContract()
				if shared {
					c.Manifests[0].SharedSteps = []SharedStep{{ID: "prepare", Procedure: "prepare data", Operations: []Operation{operation}}}
					c.Manifests[0].Cases[0].SharedSteps = []string{"prepare"}
				} else {
					c.Manifests[0].Cases[0].Operations = []Operation{operation}
				}
				if err := c.Validate(); (err == nil) != wantValid {
					t.Fatalf("shared=%v validity=%v: %v", shared, wantValid, err)
				}
			}
		})
	}
}

func TestOperationJSONPreservesApprovedValues(t *testing.T) {
	want := []Operation{{Kind: OperationArgv, Argv: []string{"tool", "", "two words", `"quoted"`, "$value"}, Stdin: "input\n"}, {Kind: OperationShell, Command: "printf '%s' \"$value\"\n"}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got []Operation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("approved values changed: %#v != %#v", got, want)
	}
}

func TestManifestOperationsHaveOneInvocationSource(t *testing.T) {
	c, _ := fixtureContract()
	c.Manifests[1].Cases[0].Operations = []Operation{{Kind: OperationArgv, Argv: []string{"go", "test"}}}
	if err := c.Validate(); err == nil {
		t.Fatal("Go reference and operations accepted as two invocation sources")
	}
	c.Manifests[1].Cases[0].TestRef = "behavior.spec.ts::runs"
	if err := c.Validate(); err != nil {
		t.Fatalf("opaque reference with approved operations rejected: %v", err)
	}
	c.Manifests[1].Cases[0].Operations = nil
	if err := c.Validate(); err != nil {
		t.Fatalf("partial design without an execution recipe rejected: %v", err)
	}
}

func TestManifestDigestBindsCaseAndSharedOperations(t *testing.T) {
	for _, shared := range []bool{false, true} {
		c, _ := fixtureContract()
		operations := []Operation{{Kind: OperationArgv, Argv: []string{"tool", "first"}}}
		if shared {
			c.Manifests[0].SharedSteps = []SharedStep{{ID: "prepare", Procedure: "prepare data", Operations: operations}}
			c.Manifests[0].Cases[0].SharedSteps = []string{"prepare"}
		} else {
			c.Manifests[0].Cases[0].Operations = operations
		}
		before, err := c.ManifestDigest()
		if err != nil {
			t.Fatal(err)
		}
		operations[0].Argv[1] = "second"
		after, err := c.ManifestDigest()
		if err != nil {
			t.Fatal(err)
		}
		if before == after {
			t.Fatalf("shared=%v operation change absent from manifest digest", shared)
		}
	}
}
