package qfnu

import (
	"strings"
	"testing"
)

func TestRunFreshmanRejectsNonNumericPage(t *testing.T) {
	var output strings.Builder

	runFreshman([]string{"search", "校规", "--page", "later"}, &output)

	if !strings.Contains(output.String(), "--page must be an integer") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}
