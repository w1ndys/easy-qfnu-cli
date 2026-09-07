package qfnu

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func captureRecommendationTelemetry(t *testing.T) *[]string {
	t.Helper()
	events := []string{}
	oldReporter := reportRecommendationUsage
	reportRecommendationUsage = func(operation, status string) {
		events = append(events, operation+":"+status)
	}
	t.Cleanup(func() { reportRecommendationUsage = oldReporter })
	return &events
}

func TestRecommendationSearchBuildsQueryAndReturnsItems(t *testing.T) {
	events := captureRecommendationTelemetry(t)
	var requestURL *url.URL
	var authorization string
	var cookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURL = r.URL
		authorization = r.Header.Get("Authorization")
		cookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		body := `{"code":"OK","data":{"count":1,"updated_at":"2026-09-08T00:00:00Z","version":"abc","items":[{"course_name":"高等数学","teacher_name":"张老师","year":"2025-2026","reason":"讲解清楚","nickname":null}]}}`
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("write search response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	oldEndpoint := recommendationEndpoint
	recommendationEndpoint = server.URL + "/v1/recommendations"
	t.Cleanup(func() { recommendationEndpoint = oldEndpoint })

	var output strings.Builder
	code := runRecommendationSearch([]string{"--course", "高等数学", "--teacher", "张", "--top", "5"}, &output)
	if code != 0 {
		t.Fatalf("runRecommendationSearch() = %d, output = %s", code, output.String())
	}
	if requestURL == nil {
		t.Fatal("recommendation request was not sent")
	}
	if requestURL.Path != "/v1/recommendations" {
		t.Fatalf("path = %q", requestURL.Path)
	}
	query := requestURL.Query()
	if query.Get("course") != "高等数学" || query.Get("teacher") != "张" || query.Get("top") != "5" {
		t.Fatalf("query = %v", query)
	}
	if authorization != "" {
		t.Fatalf("unexpected client authorization header: %q", authorization)
	}
	if cookie != "" {
		t.Fatalf("unexpected client cookie header: %q", cookie)
	}

	var result payload
	if err := json.Unmarshal([]byte(output.String()), &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true || result["source"] != "recommendation" || result["count"] != float64(1) {
		t.Fatalf("result = %#v", result)
	}
	items, ok := result["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v", result["items"])
	}
	if len(*events) != 1 || (*events)[0] != "search:success" {
		t.Fatalf("telemetry events = %#v", *events)
	}
}

func TestRecommendationSearchDefaultsTopAndPluralDispatch(t *testing.T) {
	events := captureRecommendationTelemetry(t)
	var requestURL *url.URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURL = r.URL
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"code":"OK","data":{"count":0,"items":[],"updated_at":"2026-09-08T00:00:00Z","version":"local"}}`); err != nil {
			t.Errorf("write empty response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	oldEndpoint := recommendationEndpoint
	recommendationEndpoint = server.URL + "/v1/recommendations"
	t.Cleanup(func() { recommendationEndpoint = oldEndpoint })

	var output strings.Builder
	if code := Run([]string{"recommendations", "search", "--teacher", "王"}, &output, io.Discard); code != 0 {
		t.Fatalf("plural dispatch exit code = %d, output = %s", code, output.String())
	}
	if requestURL == nil {
		t.Fatal("recommendation request was not sent")
	}
	if requestURL.Query().Get("teacher") != "王" || requestURL.Query().Get("top") != "20" || requestURL.Query().Get("course") != "" {
		t.Fatalf("query = %v", requestURL.Query())
	}
	if len(*events) != 1 || (*events)[0] != "search:success" {
		t.Fatalf("telemetry events = %#v", *events)
	}
}

func TestRecommendationRejectsMissingConditionsAndRemoteFailure(t *testing.T) {
	events := captureRecommendationTelemetry(t)
	var output strings.Builder
	if code := runRecommendationSearch([]string{"--course", " "}, &output); code == 0 {
		t.Fatal("empty conditions unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "至少提供一个非空") {
		t.Fatalf("empty condition output = %s", output.String())
	}
	if len(*events) != 0 {
		t.Fatalf("local validation should not report telemetry, events = %#v", *events)
	}

	output.Reset()
	if code := runRecommendationSearch([]string{"--course", "高数", "--top", "101"}, &output); code == 0 {
		t.Fatal("oversize top unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "1 到 100") {
		t.Fatalf("top output = %s", output.String())
	}
	if len(*events) != 0 {
		t.Fatalf("invalid top should not report telemetry, events = %#v", *events)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		if _, err := io.WriteString(w, `{"code":"INVALID_REQUEST","data":null,"message":"查询至少需要 course 或 teacher"}`); err != nil {
			t.Errorf("write failure response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	oldEndpoint := recommendationEndpoint
	recommendationEndpoint = server.URL
	t.Cleanup(func() { recommendationEndpoint = oldEndpoint })
	output.Reset()
	if code := runRecommendationSearch([]string{"--course", "高数"}, &output); code == 0 {
		t.Fatal("remote failure unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "查询至少需要 course 或 teacher") {
		t.Fatalf("remote failure output = %s", output.String())
	}
	if len(*events) != 1 || (*events)[0] != "search:failure" {
		t.Fatalf("telemetry events = %#v", *events)
	}
}
