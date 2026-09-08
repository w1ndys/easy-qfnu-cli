package qfnu

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultRecommendationEndpoint = "https://recommend.easy-qfnu.top/v1/recommendation"

var (
	recommendationEndpoint   = defaultRecommendationEndpoint
	recommendationHTTPClient = &http.Client{Timeout: 30 * time.Second}
)

type recommendationResponse struct {
	status int
	url    string
	body   map[string]any
}

type recommendationClientError struct {
	message     string
	hint        string
	reportUsage bool
}

// queryRecommendations 只做公开 GET：不读取本地 JWXT 会话，也不附加 Cookie。
func queryRecommendations(values url.Values) (recommendationResponse, *recommendationClientError) {
	target, err := url.Parse(strings.TrimRight(recommendationEndpoint, "/"))
	if err != nil {
		return recommendationResponse{}, &recommendationClientError{message: "无法构造推荐服务地址", hint: "请稍后重试"}
	}
	if values != nil {
		target.RawQuery = values.Encode()
	}
	request, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return recommendationResponse{}, &recommendationClientError{message: "无法构造推荐查询请求", hint: "请稍后重试"}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "easy-qfnu/"+version)
	response, err := recommendationHTTPClient.Do(request)
	if err != nil {
		return recommendationResponse{}, &recommendationClientError{message: "推荐查询请求失败", hint: "请检查网络和远程服务后重试", reportUsage: true}
	}
	var body map[string]any
	if err := decodeResponseJSON(response, &body); err != nil {
		return recommendationResponse{}, &recommendationClientError{message: "推荐服务返回了无效 JSON", hint: "请稍后重试", reportUsage: true}
	}
	return recommendationResponse{status: response.StatusCode, url: target.String(), body: body}, nil
}
