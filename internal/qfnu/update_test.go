package qfnu

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{left: "v0.1.10", right: "v0.1.2", want: 1},
		{left: "v2026.08.30.14", right: "v2026.08.30.14", want: 0},
		{left: "v2026.08.30.13", right: "v2026.08.30.14", want: -1},
	}
	for _, test := range tests {
		if got := compareVersions(test.left, test.right); got != test.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestCheckUpdatesUsesReleaseVersion(t *testing.T) {
	oldVersion := version
	oldManifestURL := updateManifestURL
	t.Cleanup(func() {
		version = oldVersion
		updateManifestURL = oldManifestURL
	})
	version = "v2026.08.30.14"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"release_version":"v2026.08.30.15","cli_version":"v2026.08.30.15"}`)); err != nil {
			t.Errorf("write newer manifest: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	updateManifestURL = server.URL

	var out bytes.Buffer
	if code := checkUpdates(&out); code == 0 {
		t.Fatal("checkUpdates() returned success for a newer release")
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	updates, ok := result["updates"].([]any)
	if !ok || len(updates) != 1 {
		t.Fatalf("updates = %#v", result["updates"])
	}
	issue, ok := updates[0].(map[string]any)
	if !ok || issue["kind"] != "cli" || issue["latest_version"] != "v2026.08.30.15" {
		t.Fatalf("release update = %#v", updates[0])
	}
}

func TestCheckUpdatesAcceptsMatchingReleaseWithoutSkillDirectory(t *testing.T) {
	oldVersion := version
	oldManifestURL := updateManifestURL
	t.Cleanup(func() {
		version = oldVersion
		updateManifestURL = oldManifestURL
	})
	version = "v2026.08.30.14"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"release_version":"v2026.08.30.14","cli_version":"v2026.08.30.14","skill_version":"v0.0.0"}`)); err != nil {
			t.Errorf("write matching manifest: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	updateManifestURL = server.URL

	var out bytes.Buffer
	if code := checkUpdates(&out); code != 0 {
		t.Fatalf("checkUpdates() = %d, output = %s", code, out.String())
	}
}
