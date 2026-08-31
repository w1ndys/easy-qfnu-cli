package qfnu

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTelemetryClientSendsOnlyAnonymousFields(t *testing.T) {
	oldVersion := version
	t.Cleanup(func() { version = oldVersion })
	version = "v2026.09.01.1200"

	var received telemetryEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-QFNU-JWXT-Cookie") != "" {
			t.Fatal("telemetry request must not contain a Cookie")
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("telemetry request must not contain authorization")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode telemetry: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := telemetryClient{
		endpoint: server.URL,
		http:     server.Client(),
		now:      func() time.Time { return time.Date(2026, 9, 1, 4, 5, 6, 0, time.UTC) },
	}
	if err := client.send("jwxt.login", "success"); err != nil {
		t.Fatalf("send telemetry: %v", err)
	}

	want := telemetryEvent{
		Feature:    "jwxt.login",
		Status:     "success",
		CLIVersion: version,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		OccurredAt: "2026-09-01T04:05:06Z",
	}
	if received != want {
		t.Fatalf("telemetry event = %#v, want %#v", received, want)
	}
}

func TestReportLoginSuccessKeepsFailureSilent(t *testing.T) {
	oldReporter := reportAnonymousEvent
	t.Cleanup(func() { reportAnonymousEvent = oldReporter })
	var feature, status string
	reportAnonymousEvent = func(gotFeature, gotStatus string) error {
		feature, status = gotFeature, gotStatus
		return errors.New("service unavailable")
	}
	result := payload{}

	reportLoginSuccess(result)

	if feature != "jwxt.login" || status != "success" {
		t.Fatalf("reported %q/%q", feature, status)
	}
	notice, _ := result["telemetry_notice"].(string)
	if !strings.Contains(notice, "不含学号、姓名、Cookie") {
		t.Fatalf("unexpected telemetry notice: %q", notice)
	}
	if strings.Contains(notice, "失败") || strings.Contains(notice, "unavailable") {
		t.Fatalf("telemetry failure leaked to output: %q", notice)
	}
}

func TestStatusAndFailedLoginDoNotReportLoginEvent(t *testing.T) {
	oldReporter := reportAnonymousEvent
	t.Cleanup(func() { reportAnonymousEvent = oldReporter })
	calls := 0
	reportAnonymousEvent = func(_, _ string) error {
		calls++
		return nil
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("QFNU_JWXT_USERNAME", "")
	t.Setenv("QFNU_JWXT_PASSWORD", "")

	var statusOut strings.Builder
	if code := runJWXT([]string{"status"}, &statusOut); code != 0 {
		t.Fatalf("status exit code = %d", code)
	}
	var loginOut strings.Builder
	if code := runJWXT([]string{"login"}, &loginOut); code != 0 {
		t.Fatalf("failed login write exit code = %d", code)
	}
	if !strings.Contains(loginOut.String(), `"ok": false`) {
		t.Fatalf("login without credentials output = %s", loginOut.String())
	}
	if calls != 0 {
		t.Fatalf("reported %d events for status or failed login", calls)
	}
}
