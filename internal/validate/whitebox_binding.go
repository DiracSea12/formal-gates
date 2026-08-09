package validate

// whiteboxDeliveredTestCode is the whitebox designer's delivered structural test
// code used by fixtures and the portable canary (RQ-013 binding): the tests bound
// by whitebox cases are declared here. It is delivery data, not a validation
// source: the CLI records a whitebox case's test reference (Test = "<file>::<function>",
// an opaque file-locating reference) and does not parse or scan any code to check
// existence. Existence and correspondence are verified by qa-review (reading the
// delivered code) and qa-execution (actually running the tests).
const whiteboxDeliveredTestCode = `package whiteboxfixture

import "testing"

func TestWhiteboxDirectRules(t *testing.T) {}

func TestWhiteboxStructure(t *testing.T) {}

func TestWhiteboxStructureRevised(t *testing.T) {}

func TestWhiteboxDirectBehavior(t *testing.T) {}

func TestWhiteboxFailurePaths(t *testing.T) {}

func TestWhiteboxDuplicate(t *testing.T) {}
`
