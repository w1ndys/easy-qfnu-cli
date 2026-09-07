package qfnu

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

var reportRecommendationUsage = func(operation, status string) {
	// Telemetry is a side effect; its failure must not change a query result.
	reportUsage("recommendation."+operation, status)
}

var errRecommendationHelp = errors.New("recommendation help")

func runRecommendation(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" {
		return printRecommendationUsage(out)
	}

	switch args[0] {
	case "search":
		return runRecommendationSearch(args[1:], out)
	default:
		return writeRecommendationFailure(out, "unknown action: "+args[0], "支持 search")
	}
}

func printRecommendationUsage(out io.Writer) int {
	usage := "Usage: easy-qfnu recommendation search [--course value] [--teacher value] [--top 20]\n"
	if _, err := fmt.Fprint(out, usage); err != nil {
		return 1
	}
	return 2
}

func runRecommendationSearch(args []string, out io.Writer) int {
	values, err := parseRecommendationSearch(args)
	if err != nil {
		if errors.Is(err, errRecommendationHelp) {
			return printRecommendationUsage(out)
		}
		return writeRecommendationFailure(out, err.Error(), "")
	}
	return requestRecommendation(values, out)
}

func parseRecommendationSearch(args []string) (url.Values, error) {
	course := ""
	teacher := ""
	top := 20
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--help" {
			return nil, errRecommendationHelp
		}
		if arg != "--course" && arg != "--teacher" && arg != "--top" {
			if strings.HasPrefix(arg, "-") {
				return nil, fmt.Errorf("unknown option: %s", arg)
			}
			return nil, fmt.Errorf("search 不接受位置参数")
		}
		if index+1 >= len(args) {
			return nil, fmt.Errorf("%s requires a value", arg)
		}
		value := strings.TrimSpace(args[index+1])
		index++
		switch arg {
		case "--course":
			course = value
		case "--teacher":
			teacher = value
		case "--top":
			parsed, convErr := strconv.Atoi(value)
			if convErr != nil {
				return nil, fmt.Errorf("--top must be an integer")
			}
			top = parsed
		}
	}
	if course == "" && teacher == "" {
		return nil, fmt.Errorf("至少提供一个非空 --course 或 --teacher")
	}
	if top < 1 || top > 100 {
		return nil, fmt.Errorf("top 必须是 1 到 100 的整数")
	}
	values := url.Values{}
	if course != "" {
		values.Set("course", course)
	}
	if teacher != "" {
		values.Set("teacher", teacher)
	}
	values.Set("top", strconv.Itoa(top))
	return values, nil
}

func requestRecommendation(values url.Values, out io.Writer) int {
	response, clientErr := queryRecommendations(values)
	if clientErr != nil {
		if clientErr.reportUsage {
			reportRecommendationUsage("search", "failure")
		}
		return writeRecommendationFailure(out, clientErr.message, clientErr.hint)
	}
	if response.status < 200 || response.status >= 300 {
		message := recommendationResponseMessage(response.body, fmt.Sprintf("推荐服务返回 HTTP %d", response.status))
		reportRecommendationUsage("search", "failure")
		return writeRecommendationFailure(out, message, "请稍后重试")
	}
	if !isRecommendationSuccessCode(response.body["code"]) {
		message := recommendationResponseMessage(response.body, "推荐服务拒绝了查询请求")
		reportRecommendationUsage("search", "failure")
		return writeRecommendationFailure(out, message, "请检查查询条件后重试")
	}
	result := recommendationResult(response)
	reportRecommendationUsage("search", "success")
	return writeJSON(out, success("recommendation", result))
}

func recommendationResponseMessage(body map[string]any, fallback string) string {
	message, ok := body["message"].(string)
	if !ok || strings.TrimSpace(message) == "" {
		return fallback
	}
	return strings.TrimSpace(message)
}

func recommendationResult(response recommendationResponse) payload {
	result := payload{}
	if data, ok := response.body["data"].(map[string]any); ok {
		for key, value := range data {
			result[key] = value
		}
	} else {
		result["data"] = response.body["data"]
	}
	result["operation"] = "search"
	result["url"] = response.url
	return result
}

func isRecommendationSuccessCode(code any) bool {
	text, ok := code.(string)
	return ok && text == "OK"
}

func writeRecommendationFailure(out io.Writer, message, hint string) int {
	return writeJSON(out, failure("recommendation", message, hint)) + 1
}
