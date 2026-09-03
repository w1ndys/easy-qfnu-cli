package qfnu

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeCredentialsMatchesQFNUProtocol(t *testing.T) {
	got := encodeCredentials("abc", "pw", "XYZ123", "10120")
	want := "aXbcY%Z1%%pw"
	if got != want {
		t.Fatalf("encodeCredentials() = %q, want %q", got, want)
	}
}

func TestEncodeCredentialsKeepsCharactersAfterProtocolLimit(t *testing.T) {
	username := "12345678901234567890"
	got := encodeCredentials(username, "pw", "", "")
	if got != username[:20]+"%%%pw" {
		t.Fatalf("unexpected encoded value: %q", got)
	}
}

func TestParseLoginMessageReturnsExactShowMsgText(t *testing.T) {
	raw := `<li class="input_li" id="showMsg" style="color: red; margin-bottom: 0;">
		&nbsp;验证码错误!!
	</li>`
	got := parseLoginMessage(raw)
	want := "验证码错误!!"
	if got != want {
		t.Fatalf("parseLoginMessage() = %q, want %q", got, want)
	}
}

func TestParseLoginMessageSupportsNestedMarkupAndSingleQuotedID(t *testing.T) {
	raw := `<div id='showMsg'><font color="red">用户名或密码错误</font></div>`
	got := parseLoginMessage(raw)
	want := "用户名或密码错误"
	if got != want {
		t.Fatalf("parseLoginMessage() = %q, want %q", got, want)
	}
}

func TestParseLoginMessageIgnoresPageWithoutShowMsg(t *testing.T) {
	if got := parseLoginMessage(`<html><body>登录失败</body></html>`); got != "" {
		t.Fatalf("parseLoginMessage() = %q, want empty", got)
	}
}

func TestLoginFailureHintMatchesCaptchaMessage(t *testing.T) {
	got := loginFailureHint("验证码错误!!")
	want := "重新运行 easy-qfnu jwxt captcha 获取新验证码"
	if got != want {
		t.Fatalf("loginFailureHint() = %q, want %q", got, want)
	}
}

func TestParseListItems(t *testing.T) {
	raw := `<ul class="n_listxx1"><li><h2><a href="info/1103/7719.htm" title="通知标题">通知标题</a><span class="time">2026-07-13</span></h2><p>摘要内容</p></li></ul>`
	items := parseListItems(raw, jwcBase+"/tz_j_.htm")
	if len(items) != 1 || items[0].ID != "7719" || items[0].CategoryID != "1103" {
		t.Fatalf("unexpected list item: %#v", items)
	}
}

func TestRequestFollowsSameOriginRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			if r.Method != http.MethodPost {
				t.Errorf("initial method = %s, want POST", r.Method)
			}
			http.Redirect(w, r, "/finish", http.StatusFound)
		case "/finish":
			if r.Method != http.MethodGet {
				t.Errorf("redirected method = %s, want GET", r.Method)
			}
			_, _ = w.Write([]byte("authenticated"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := &jwxtClient{http: &http.Client{CheckRedirect: sameOriginRedirect(server.URL)}}
	status, finalURL, body, err := client.request(http.MethodPost, server.URL+"/start", strings.NewReader("payload"), nil)
	if err != nil {
		t.Fatalf("request returned error: %v", err)
	}
	if status != http.StatusOK || finalURL != server.URL+"/finish" || string(body) != "authenticated" {
		t.Fatalf("request = status %d, final %q, body %q", status, finalURL, body)
	}

}

func TestRequestStopsCrossOriginRedirect(t *testing.T) {
	var destinationHits int
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationHits++
		_, _ = w.Write([]byte("must not follow"))
	}))
	t.Cleanup(destination.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/finish", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	client := &jwxtClient{http: &http.Client{CheckRedirect: sameOriginRedirect(source.URL)}}
	status, finalURL, body, err := client.request(http.MethodGet, source.URL+"/start", nil, nil)
	if err != nil {
		t.Fatalf("request returned error: %v", err)
	}
	if status != http.StatusFound || finalURL != source.URL+"/start" {
		t.Fatalf("cross-origin redirect = status %d, final %q", status, finalURL)
	}
	if destinationHits != 0 || !strings.Contains(string(body), destination.URL) {
		t.Fatalf("cross-origin redirect was followed: hits=%d body=%q", destinationHits, body)
	}
}

func TestRunJWXTRejectsNonNumericScore(t *testing.T) {
	var output strings.Builder

	runJWXT([]string{"evaluate", "--score", "high"}, &output)

	if !strings.Contains(output.String(), "--score must be an integer") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestRunJWXTReportsCorruptSession(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(sessionPath, []byte("{"), 0600); err != nil {
		t.Fatalf("write corrupt session: %v", err)
	}
	var output strings.Builder

	runJWXT([]string{"status", "--session-path", sessionPath}, &output)

	if !strings.Contains(output.String(), "parse JWXT session") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestRunJWXTLogoutRemovesCorruptSession(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(sessionPath, []byte("{"), 0600); err != nil {
		t.Fatalf("write corrupt session: %v", err)
	}
	var output strings.Builder

	runJWXT([]string{"logout", "--session-path", sessionPath}, &output)

	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session still exists or stat failed: %v", err)
	}
	if !strings.Contains(output.String(), `"logged_in": false`) {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestRunJWXTLogoutReportsRemovalFailure(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session")
	if err := os.Mkdir(sessionPath, 0700); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionPath, "child"), []byte("x"), 0600); err != nil {
		t.Fatalf("make session directory non-empty: %v", err)
	}
	var output strings.Builder

	runJWXT([]string{"logout", "--session-path", sessionPath}, &output)

	if !strings.Contains(output.String(), "clear JWXT session") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestEvaluationPresetRejectsInvalidOptionScore(t *testing.T) {
	detail := &evaluationDetail{
		IDs:     []string{"indicator"},
		Options: map[string][]payload{"indicator": {{"option_id": "good", "score": "invalid"}}},
	}

	_, _, err := evaluationPreset(detail, 89)

	if err == nil || !strings.Contains(err.Error(), "invalid score") {
		t.Fatalf("evaluationPreset() error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestStatusKeepsLoginWhenProfileEnrichmentFails(t *testing.T) {
	client, err := newJWXTClient(filepath.Join(t.TempDir(), "session.json"), "")
	if err != nil {
		t.Fatalf("create JWXT client: %v", err)
	}
	client.meta.Username = "student"
	client.jar.SetCookies(jwxtOriginURL(), []*http.Cookie{{Name: "JSESSIONID", Value: "active"}})
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/jsxsd/framework/xsMain.jsp" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("教学一体化服务平台")),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		}
		return nil, errors.New("profile unavailable")
	})

	result, err := client.status()

	if err != nil || result["logged_in"] != true {
		t.Fatalf("status result = %#v, error = %v", result, err)
	}
	if warning, ok := result["profile_warning"].(string); !ok || !strings.Contains(warning, "资料补全") {
		t.Fatalf("profile warning = %#v", result["profile_warning"])
	}
}
