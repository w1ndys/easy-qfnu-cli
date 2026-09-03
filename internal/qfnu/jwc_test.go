package qfnu

import (
	"strings"
	"testing"
)

func TestListJWCRejectsNonNumericPage(t *testing.T) {
	_, err := listJWC([]string{"--page", "later"})

	if err == nil || !strings.Contains(err.Error(), "--page must be an integer") {
		t.Fatalf("listJWC() error = %v", err)
	}
}

func TestSearchJWCRejectsNonNumericLimit(t *testing.T) {
	_, err := searchJWC([]string{"选课", "--limit", "many"})

	if err == nil || !strings.Contains(err.Error(), "--limit must be an integer") {
		t.Fatalf("searchJWC() error = %v", err)
	}
}
