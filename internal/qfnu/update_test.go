package qfnu

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{left: "v0.1.10", right: "v0.1.2", want: 1},
		{left: "v2026.08.30.1608", right: "v2026.08.30.1608", want: 0},
		{left: "v2026.08.30.1607", right: "v2026.08.30.1608", want: -1},
	}
	for _, test := range tests {
		if got := compareVersions(test.left, test.right); got != test.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestReadSkillVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "VERSION")
	if err := os.WriteFile(path, []byte("v2026.08.30.1608\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, ok := readSkillVersion(path); !ok || got != "v2026.08.30.1608" {
		t.Fatalf("readSkillVersion() = %q, %v", got, ok)
	}
}

func TestCheckUpdatesStopsForStaleSkill(t *testing.T) {
	oldVersion := version
	oldManifestURL := updateManifestURL
	t.Cleanup(func() {
		version = oldVersion
		updateManifestURL = oldManifestURL
	})
	version = "v2026.08.30.1608"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"release_version":"v2026.08.30.1608","cli_version":"v2026.08.30.1608","skill_version":"v2026.08.30.1609"}`))
	}))
	t.Cleanup(server.Close)
	updateManifestURL = server.URL

	skillDir := t.TempDir()
	skill := "---\nname: easy-qfnu-skill\ndescription: test\n---\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skill), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "VERSION"), []byte("v2026.08.30.1608\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EASY_QFNU_SKILL_DIR", skillDir)
	var out, errOut bytes.Buffer
	if code := checkUpdates(&out, &errOut); code == 0 {
		t.Fatal("checkUpdates() returned success for a stale skill")
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["update_required"] != true {
		t.Fatalf("update_required = %#v, want true", result["update_required"])
	}
}
