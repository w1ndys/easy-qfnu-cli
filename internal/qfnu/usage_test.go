package qfnu

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// TestMain 把默认上报替换为空操作，防止任何测试把匿名事件发到生产 hub；
// 需要断言事件的测试自行替换 reportUsage。
func TestMain(m *testing.M) {
	old := reportUsage
	reportUsage = func(string, string) {}
	code := m.Run()
	reportUsage = old
	os.Exit(code)
}

func captureUsage(t *testing.T) *[]string {
	t.Helper()
	old := reportUsage
	events := []string{}
	reportUsage = func(feature, status string) {
		events = append(events, feature+":"+status)
	}
	t.Cleanup(func() { reportUsage = old })
	return &events
}

func TestJWXTAcademicActionsReportSuccessAndFailure(t *testing.T) {
	for _, action := range []string{"grades", "schedule", "evaluations", "evaluate"} {
		events := captureUsage(t)
		writeJWXTCommandResult(action, nil, errors.New("boom"), io.Discard)
		if len(*events) != 1 || (*events)[0] != "jwxt."+action+":failure" {
			t.Fatalf("%s failure events = %v", action, *events)
		}
		*events = nil
		writeJWXTCommandResult(action, payload{}, nil, io.Discard)
		if len(*events) != 1 || (*events)[0] != "jwxt."+action+":success" {
			t.Fatalf("%s success events = %v", action, *events)
		}
	}
}

func TestLoginFailureAndLocalActionsReportNothing(t *testing.T) {
	events := captureUsage(t)
	writeJWXTCommandResult("login", nil, errors.New("wrong password"), io.Discard)
	writeJWXTCommandResult("status", nil, errors.New("boom"), io.Discard)
	writeJWXTCommandResult("captcha", nil, errors.New("boom"), io.Discard)
	writeJWXTCommandResult("logout", nil, errors.New("boom"), io.Discard)
	if len(*events) != 0 {
		t.Fatalf("unexpected events = %v", *events)
	}
}

func TestLoginSuccessStillReportsOnlyViaLoginPath(t *testing.T) {
	events := captureUsage(t)
	oldReporter := reportAnonymousEvent
	t.Cleanup(func() { reportAnonymousEvent = oldReporter })
	var anonymous []string
	reportAnonymousEvent = func(feature, status string) error {
		anonymous = append(anonymous, feature+":"+status)
		return nil
	}

	writeJWXTCommandResult("login", payload{}, nil, io.Discard)
	if len(*events) != 0 {
		t.Fatalf("usage events = %v", *events)
	}
	if len(anonymous) != 1 || anonymous[0] != "jwxt.login:success" {
		t.Fatalf("anonymous events = %v", anonymous)
	}
}

func TestRunJWCReportsOnlyNetworkActions(t *testing.T) {
	events := captureUsage(t)

	var out strings.Builder
	runJWC([]string{"list", "--page", "later"}, &out)
	if !strings.Contains(out.String(), `"ok": false`) {
		t.Fatalf("list output = %s", out.String())
	}
	if len(*events) != 1 || (*events)[0] != "jwc.list:failure" {
		t.Fatalf("list events = %v", *events)
	}

	*events = nil
	runJWC([]string{"channels"}, &out)
	if len(*events) != 0 {
		t.Fatalf("channels events = %v", *events)
	}
	*events = nil
	runJWC([]string{"bogus"}, &out)
	if len(*events) != 0 {
		t.Fatalf("unknown jwc action events = %v", *events)
	}
}

func TestRunFreshmanReportsSearchAttemptsOnly(t *testing.T) {
	events := captureUsage(t)

	var out strings.Builder
	runFreshman([]string{"search", "校规", "--page", "later"}, &out)
	if len(*events) != 1 || (*events)[0] != "freshman.search:failure" {
		t.Fatalf("search events = %v", *events)
	}
	*events = nil
	runFreshman([]string{"bogus"}, &out)
	if len(*events) != 0 {
		t.Fatalf("unknown freshman action events = %v", *events)
	}
}

func TestRelayDoesNotReportBeforeRequestAttempt(t *testing.T) {
	events := captureUsage(t)

	var out strings.Builder
	runJWXTRelay("custom", testRelayClient(t), strings.NewReader(`{}`), &out)
	if len(*events) != 0 {
		t.Fatalf("pre-attempt events = %v", *events)
	}
}
