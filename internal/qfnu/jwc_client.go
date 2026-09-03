package qfnu

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func requestJWC(method, target string, body io.Reader, headers map[string]string) (string, string, error) {
	request, err := http.NewRequest(method, target, body)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("User-Agent", "easy-qfnu-skill/easy-qfnu")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return "", "", err
	}
	bodyBytes, err := readResponseBody(response)
	if err != nil {
		return "", response.Request.URL.String(), err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", response.Request.URL.String(), fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return string(bodyBytes), response.Request.URL.String(), nil
}

func listJWCPage(item channel, page int) (string, string, error) {
	if page < 1 {
		return "", "", &jwcError{message: "page must be at least 1"}
	}
	path := "/" + item.Slug + ".htm"
	if page > 1 {
		path = "/" + item.Slug + "/" + strconv.Itoa(page) + ".htm"
	}
	return requestJWC(http.MethodGet, jwcBase+path, nil, nil)
}

func searchJWCPage(keyword string, page int) (string, string, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(keyword)))
	if page <= 1 {
		form := url.Values{"lucenenewssearchkey": {encoded}, "_lucenesearchtype": {"1"}, "searchScope": {"1"}}
		headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Referer": jwcBase + "/"}
		return requestJWC(http.MethodPost, jwcBase+"/ssjg.jsp?wbtreeid=1001", strings.NewReader(form.Encode()), headers)
	}
	target := jwcBase + "/ssjg.jsp?wbtreeid=1001&searchScope=1&currentnum=" + strconv.Itoa(page) + "&newskeycode2=" + url.QueryEscape(encoded)
	return requestJWC(http.MethodGet, target, nil, map[string]string{"Referer": jwcBase + "/"})
}
