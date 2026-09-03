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

func capturePrecourseTelemetry(t *testing.T) *[]string {
	t.Helper()
	events := []string{}
	oldReporter := reportPrecourseUsage
	reportPrecourseUsage = func(operation, status string) {
		events = append(events, operation+":"+status)
	}
	t.Cleanup(func() { reportPrecourseUsage = oldReporter })
	return &events
}

func TestPrecourseSearchBuildsQueryAndReturnsCourses(t *testing.T) {
	events := capturePrecourseTelemetry(t)
	var requestURL *url.URL
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURL = r.URL
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"code":"OK","data":{"count":1,"courses":[{"courseCode":"590014","courseName":"音乐鉴赏"}]}}`); err != nil {
			t.Errorf("write search response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	oldEndpoint := precourseEndpoint
	precourseEndpoint = server.URL + "/v1/precourses"
	t.Cleanup(func() { precourseEndpoint = oldEndpoint })

	var output strings.Builder
	code := runPrecourseSearch([]string{"音乐鉴赏", "--campus", "日照", "--teacher-name", "王老师"}, &output)
	if code != 0 {
		t.Fatalf("runPrecourseSearch() = %d, output = %s", code, output.String())
	}
	if requestURL == nil {
		t.Fatal("precourse request was not sent")
	}
	if requestURL.Path != "/v1/precourses/search" {
		t.Fatalf("path = %q", requestURL.Path)
	}
	if requestURL.Query().Get("q") != "音乐鉴赏" || requestURL.Query().Get("campus") != "日照" || requestURL.Query().Get("teacherName") != "王老师" {
		t.Fatalf("query = %v", requestURL.Query())
	}
	if authorization != "" {
		t.Fatalf("unexpected client authorization header: %q", authorization)
	}

	var result payload
	if err := json.Unmarshal([]byte(output.String()), &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true || result["source"] != "precourse" || result["count"] != float64(1) {
		t.Fatalf("result = %#v", result)
	}
	courses, ok := result["courses"].([]any)
	if !ok || len(courses) != 1 {
		t.Fatalf("courses = %#v", result["courses"])
	}
	if len(*events) != 1 || (*events)[0] != "search:success" {
		t.Fatalf("telemetry events = %#v", *events)
	}
}

func TestPrecourseMetaPopularAndPluralDispatch(t *testing.T) {
	events := capturePrecourseTelemetry(t)
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"code":0,"data":{"items":[]}}`); err != nil {
			t.Errorf("write metadata response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	oldEndpoint := precourseEndpoint
	precourseEndpoint = server.URL + "/v1/precourses"
	t.Cleanup(func() { precourseEndpoint = oldEndpoint })

	var output strings.Builder
	if code := runPrecourse([]string{"meta"}, &output); code != 0 {
		t.Fatalf("meta exit code = %d, output = %s", code, output.String())
	}
	output.Reset()
	if code := runPrecourse([]string{"popular", "--field", "college"}, &output); code != 0 {
		t.Fatalf("popular exit code = %d, output = %s", code, output.String())
	}
	output.Reset()
	if code := Run([]string{"precourses", "search", "音乐"}, &output, io.Discard); code != 0 {
		t.Fatalf("plural dispatch exit code = %d, output = %s", code, output.String())
	}

	if len(paths) != 3 || paths[0] != "/v1/precourses/meta?" || paths[1] != "/v1/precourses/popular?field=college" || paths[2] != "/v1/precourses/search?q=%E9%9F%B3%E4%B9%90" {
		t.Fatalf("paths = %#v", paths)
	}
	if len(*events) != 3 || (*events)[0] != "meta:success" || (*events)[1] != "popular:success" || (*events)[2] != "search:success" {
		t.Fatalf("telemetry events = %#v", *events)
	}
}

func TestPrecourseRejectsMissingConditionsAndRemoteFailure(t *testing.T) {
	events := capturePrecourseTelemetry(t)
	var output strings.Builder
	if code := runPrecourseSearch([]string{"--campus", " "}, &output); code == 0 {
		t.Fatal("empty conditions unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "非空") {
		t.Fatalf("empty condition output = %s", output.String())
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		if _, err := io.WriteString(w, `{"code":"UPSTREAM_UNAVAILABLE","data":null}`); err != nil {
			t.Errorf("write failure response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	oldEndpoint := precourseEndpoint
	precourseEndpoint = server.URL
	t.Cleanup(func() { precourseEndpoint = oldEndpoint })
	output.Reset()
	if code := runPrecourseSearch([]string{"音乐"}, &output); code == 0 {
		t.Fatal("remote failure unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "HTTP 502") {
		t.Fatalf("remote failure output = %s", output.String())
	}
	if len(*events) != 1 || (*events)[0] != "search:failure" {
		t.Fatalf("telemetry events = %#v", *events)
	}
}
