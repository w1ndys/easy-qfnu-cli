package qfnu

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultPrecourseEndpoint = "https://precourse.easy-qfnu.top/v1/precourses"

var (
	precourseEndpoint   = defaultPrecourseEndpoint
	precourseHTTPClient = &http.Client{Timeout: 30 * time.Second}
)

type precourseResponse struct {
	status int
	url    string
	body   map[string]any
}

type precourseClientError struct {
	message     string
	hint        string
	reportUsage bool
}

func queryPrecourse(operation string, values url.Values) (precourseResponse, *precourseClientError) {
	target, err := url.Parse(strings.TrimRight(precourseEndpoint, "/") + "/" + operation)
	if err != nil {
		return precourseResponse{}, &precourseClientError{message: "无法构造预选课服务地址", hint: "请稍后重试"}
	}
	if values != nil {
		target.RawQuery = values.Encode()
	}
	request, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return precourseResponse{}, &precourseClientError{message: "无法构造预选课请求", hint: "请稍后重试"}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "easy-qfnu/"+version)
	response, err := precourseHTTPClient.Do(request)
	if err != nil {
		return precourseResponse{}, &precourseClientError{message: "预选课查询请求失败", hint: "请检查网络和远程服务后重试", reportUsage: true}
	}
	var body map[string]any
	if err := decodeResponseJSON(response, &body); err != nil {
		return precourseResponse{}, &precourseClientError{message: "预选课服务返回了无效 JSON", hint: "请稍后重试", reportUsage: true}
	}
	return precourseResponse{status: response.StatusCode, url: target.String(), body: body}, nil
}
