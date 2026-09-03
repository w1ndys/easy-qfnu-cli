package qfnu

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const freshmanAPI = "https://freshman-exam.easy-qfnu.top/api/questions"

type freshmanQueryError struct {
	message string
	hint    string
}

func queryFreshman(search freshmanSearch) (payload, *freshmanQueryError) {
	query := url.Values{"keyword": {search.keyword}, "page": {strconv.Itoa(search.page)}, "pageSize": {strconv.Itoa(search.pageSize)}}
	target := freshmanAPI + "?" + query.Encode()
	response, err := (&http.Client{}).Get(target)
	if err != nil {
		return nil, &freshmanQueryError{message: "failed to query question bank: " + err.Error(), hint: "请检查网络后重试"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if err := discardResponseBody(response); err != nil {
			return nil, &freshmanQueryError{message: "failed to read question-bank response", hint: "请稍后重试"}
		}
		return nil, &freshmanQueryError{message: fmt.Sprintf("question bank returned HTTP %d", response.StatusCode), hint: "请稍后重试"}
	}
	var upstream map[string]any
	if err := decodeResponseJSON(response, &upstream); err != nil {
		return nil, &freshmanQueryError{message: "invalid question-bank response", hint: "请联系维护者并提供接口响应状态"}
	}
	if remoteErr := parseFreshmanRemoteError(upstream); remoteErr != nil {
		return nil, remoteErr
	}
	return normalizeFreshmanResponse(upstream, target), nil
}

func parseFreshmanRemoteError(upstream map[string]any) *freshmanQueryError {
	ok, exists := upstream["ok"].(bool)
	if !exists || ok {
		return nil
	}
	message, messageOK := upstream["error"].(string)
	if !messageOK || strings.TrimSpace(message) == "" {
		message = "question-bank request failed"
	}
	hint := ""
	if remoteHint, hintOK := upstream["hint"].(string); hintOK {
		hint = remoteHint
	}
	return &freshmanQueryError{message: message, hint: hint}
}

func normalizeFreshmanResponse(upstream map[string]any, target string) payload {
	upstream["source"] = "freshman"
	if _, exists := upstream["page_size"]; !exists {
		if value, exists := upstream["pageSize"]; exists {
			upstream["page_size"] = value
		}
	}
	if items, exists := upstream["items"].([]any); exists {
		upstream["count"] = len(items)
	}
	upstream["url"] = target
	return payload(upstream)
}
