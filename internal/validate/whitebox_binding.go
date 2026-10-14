package validate

// whiteboxDeliveredTestCode checks the portable fixture's delivery bytes and
// missing artifact through the two tests bound by the whitebox design result.
const whiteboxDeliveredTestCode = `package whiteboxfixture

import (
	"os"
	"testing"
)

func TestWhiteboxDirectBehavior(t *testing.T) {
	actual, err := os.ReadFile("delivery.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "delivery\n" {
		t.Fatalf("delivery bytes = %q", actual)
	}
}

func TestWhiteboxMissingArtifact(t *testing.T) {
	if _, err := os.Stat("absent-marker.txt"); !os.IsNotExist(err) {
		t.Fatalf("missing artifact accepted: %v", err)
	}
}
`
