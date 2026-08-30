package qfnu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const freshmanAPI = "https://fq.easy-qfnu.top/api/questions"

func runFreshman(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Fprintln(out, "Usage: qfnu freshman search <keyword> [--page 1] [--page-size 20]")
		return 2
	}
	if args[0] != "search" {
		return writeJSON(out, failure("freshman", "unknown action: "+args[0], ""))
	}
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return writeJSON(out, failure("freshman", "search keyword is empty", ""))
	}
	keyword, page, pageSize := strings.TrimSpace(args[1]), 1, 20
	for i := 2; i < len(args); i++ {
		if i+1 >= len(args) {
			return writeJSON(out, failure("freshman", args[i]+" requires a value", ""))
		}
		switch args[i] {
		case "--page":
			page, _ = strconv.Atoi(args[i+1])
		case "--page-size":
			pageSize, _ = strconv.Atoi(args[i+1])
		default:
			return writeJSON(out, failure("freshman", "unknown option: "+args[i], ""))
		}
		i++
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return writeJSON(out, failure("freshman", "page must be >= 1 and page-size must be between 1 and 100", ""))
	}
	query := url.Values{"keyword": {keyword}, "page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}
	target := freshmanAPI + "?" + query.Encode()
	resp, err := (&http.Client{}).Get(target)
	if err != nil {
		return writeJSON(out, failure("freshman", "failed to query question bank: "+err.Error(), "请检查网络后重试"))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return writeJSON(out, failure("freshman", fmt.Sprintf("question bank returned HTTP %d", resp.StatusCode), "请稍后重试"))
	}
	var upstream map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
		return writeJSON(out, failure("freshman", "invalid question-bank response", "请联系维护者并提供接口响应状态"))
	}
	if ok, exists := upstream["ok"].(bool); exists && !ok {
		message, _ := upstream["error"].(string)
		if message == "" {
			message = "question-bank request failed"
		}
		hint, _ := upstream["hint"].(string)
		return writeJSON(out, failure("freshman", message, hint))
	}
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
	return writeJSON(out, upstream)
}
