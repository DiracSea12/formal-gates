package validate

// whiteboxDeliveredTestCode is the whitebox designer's delivered structural test
// code used by fixtures and the portable canary. Test-reference assembly parses
// candidate Go files and binds the selected test's exact signature. QA review
// checks correspondence, and QA execution runs the delivered tests.
const whiteboxDeliveredTestCode = `package whiteboxfixture

import "testing"

func TestWhiteboxDirectRules(t *testing.T) {}

func TestWhiteboxStructure(t *testing.T) {}

func TestWhiteboxStructureRevised(t *testing.T) {}

func TestWhiteboxDirectBehavior(t *testing.T) {}

func TestWhiteboxFailurePaths(t *testing.T) {}

func TestWhiteboxDuplicate(t *testing.T) {}
`
