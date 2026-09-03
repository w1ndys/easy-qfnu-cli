package qfnu

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRelayFeedbackSendsJSONAndSessionCookie(t *testing.T) {
	var receivedBody []byte
	var receivedCookie string
	var receivedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedCookie = r.Header.Get("X-QFNU-JWXT-Cookie")
		receivedKey = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"OK","data":{"submitted":true}}`))
	}))
	t.Cleanup(server.Close)
	oldTarget := relayTargets["feedback"]
	relayTargets["feedback"] = relayTarget{endpoint: server.URL, sendsCookie: true, sendsIdempotency: true}
	t.Cleanup(func() { relayTargets["feedback"] = oldTarget })

	client := testRelayClient(t)
	var output strings.Builder
	input := `{"category_id":"bug","text":"页面打不开"}`
	if code := runJWXTRelay("feedback", client, strings.NewReader(input), &output); code != 0 {
		t.Fatalf("relay exit code = %d, output = %s", code, output.String())
	}
	if string(receivedBody) != input {
		t.Fatalf("body = %q, want %q", receivedBody, input)
	}
	if receivedCookie != "JSESSIONID=abc; route=blue" {
		t.Fatalf("cookie = %q", receivedCookie)
	}
	if receivedKey != relayIdempotencyKey([]byte(input)) {
		t.Fatalf("idempotency key = %q", receivedKey)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(output.String()), &response); err != nil {
		t.Fatalf("relay output is not JSON: %v", err)
	}
	if response["code"] != "OK" {
		t.Fatalf("relay response = %#v", response)
	}
}
func TestRelayRankUsesGETAndQueryParameters(t *testing.T) {
	var method, path, rawQuery string
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, rawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"OK","data":{"class_rank":1}}`))
	}))
	t.Cleanup(server.Close)
	oldTarget := relayTargets["rank"]
	relayTargets["rank"] = relayTarget{endpoint: server.URL + "/v1/rankings/me", method: http.MethodGet, sendsCookie: true}
	t.Cleanup(func() { relayTargets["rank"] = oldTarget })

	var output strings.Builder
	input := `{"scope":"both","course_codes":["CS101","CS102"]}`
	if code := runJWXTRelay("rank", testRelayClient(t), strings.NewReader(input), &output); code != 0 {
		t.Fatalf("relay exit code = %d, output = %s", code, output.String())
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet || path != "/v1/rankings/me" || string(body) != "" {
		t.Fatalf("request = %s %s?%s body=%q", method, path, rawQuery, body)
	}
	if values.Get("scope") != "both" || strings.Join(values["course_code"], ",") != "CS101,CS102" {
		t.Fatalf("query = %v", values)
	}
}

func TestRelayRejectsMissingSessionAndCustomAction(t *testing.T) {
	client, err := newJWXTClient(t.TempDir()+"/session.json", "")
	if err != nil {
		t.Fatalf("create JWXT client: %v", err)
	}
	var output strings.Builder
	if code := runJWXTRelay("feedback", client, strings.NewReader(`{}`), &output); code == 0 {
		t.Fatal("relay without session unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "no active JWXT session") {
		t.Fatalf("missing session output = %s", output.String())
	}

	output.Reset()
	if code := runJWXTRelay("custom", client, strings.NewReader(`{}`), &output); code == 0 {
		t.Fatal("custom relay action unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "unknown relay action") {
		t.Fatalf("custom action output = %s", output.String())
	}
}

func TestRelayPropagatesRemoteHTTPFailureAsNonZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"INVALID_REQUEST","data":null}`))
	}))
	t.Cleanup(server.Close)
	oldTarget := relayTargets["rank"]
	relayTargets["rank"] = relayTarget{endpoint: server.URL, sendsCookie: true}
	t.Cleanup(func() { relayTargets["rank"] = oldTarget })

	client := testRelayClient(t)
	var output strings.Builder
	if code := runJWXTRelay("rank", client, strings.NewReader(`{"scope":"both"}`), &output); code == 0 {
		t.Fatal("failed relay unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), `"INVALID_REQUEST"`) {
		t.Fatalf("remote failure output = %s", output.String())
	}
}

func TestRelayRejectsInvalidJSON(t *testing.T) {
	client := testRelayClient(t)
	var output strings.Builder
	if code := runJWXTRelay("feedback", client, strings.NewReader("{"), &output); code == 0 {
		t.Fatal("invalid JSON unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "valid JSON") {
		t.Fatalf("invalid JSON output = %s", output.String())
	}
}

func testRelayClient(t *testing.T) *jwxtClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	base, err := url.Parse(jwxtBase)
	if err != nil {
		t.Fatalf("parse JWXT URL: %v", err)
	}
	jar.SetCookies(base, []*http.Cookie{
		{Name: "JSESSIONID", Value: "abc"},
		{Name: "route", Value: "blue"},
	})
	return &jwxtClient{jar: jar}
}
