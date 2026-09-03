package qfnu

import (
	"net/http"
	"net/http/httptest"
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
