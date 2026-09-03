package qfnu

import (
	"fmt"
	"io"
	"net/url"
	"strings"
)

var reportPrecourseUsage = func(operation, status string) {
	// Telemetry is a side effect; its failure must not change a query result.
	if err := reportAnonymousEvent("precourse."+operation, status); err != nil {
		return
	}
}

var precourseSearchOptions = map[string]string{
	"--q":             "q",
	"-q":              "q",
	"--course-code":   "courseCode",
	"--course-name":   "courseName",
	"--teacher-name":  "teacherName",
	"--course-nature": "courseNature",
	"--course-attr":   "courseAttr",
	"--college":       "college",
	"--schedule-time": "scheduleTime",
	"--location":      "location",
	"--campus":        "campus",
}

func runPrecourse(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" {
		return printPrecourseUsage(out)
	}

	switch args[0] {
	case "search":
		return runPrecourseSearch(args[1:], out)
	case "meta":
		if len(args) > 1 {
			return writePrecourseFailure(out, "meta 不接受额外参数", "")
		}
		return requestPrecourse("meta", nil, out)
	case "popular":
		return runPrecoursePopular(args[1:], out)
	default:
		return writePrecourseFailure(out, "unknown action: "+args[0], "支持 search、meta、popular")
	}
}

func printPrecourseUsage(out io.Writer) int {
	usage := "Usage: easy-qfnu precourse <search|meta|popular>\n" +
		"  easy-qfnu precourse search [keyword] [--course-code value] [--course-name value] [--teacher-name value]\n" +
		"    [--course-nature value] [--course-attr value] [--college value] [--schedule-time value]\n" +
		"    [--location value] [--campus value]\n" +
		"  easy-qfnu precourse meta\n" +
		"  easy-qfnu precourse popular --field <teacherName|courseName|college>\n"
	if _, err := fmt.Fprint(out, usage); err != nil {
		return 1
	}
	return 2
}

func runPrecourseSearch(args []string, out io.Writer) int {
	values := url.Values{}
	keyword := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--help" {
			return printPrecourseUsage(out)
		}
		if field, ok := precourseSearchOptions[arg]; ok {
			if index+1 >= len(args) {
				return writePrecourseFailure(out, arg+" requires a value", "")
			}
			value := strings.TrimSpace(args[index+1])
			if field == "q" && keyword != "" {
				return writePrecourseFailure(out, "搜索关键词只能指定一次", "")
			}
			values.Set(field, value)
			index++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return writePrecourseFailure(out, "unknown option: "+arg, "")
		}
		if keyword != "" {
			return writePrecourseFailure(out, "search 只接受一个位置关键词", "")
		}
		keyword = strings.TrimSpace(arg)
	}

	if keyword != "" {
		if values.Get("q") != "" {
			return writePrecourseFailure(out, "搜索关键词只能指定一次", "")
		}
		values.Set("q", keyword)
	}
	values = nonEmptyValues(values)
	if len(values) == 0 {
		return writePrecourseFailure(out, "至少提供一个非空查询条件", "")
	}
	return requestPrecourse("search", values, out)
}

func runPrecoursePopular(args []string, out io.Writer) int {
	field := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--help" {
			return printPrecourseUsage(out)
		}
		if arg != "--field" {
			return writePrecourseFailure(out, "unknown option: "+arg, "使用 --field 指定统计字段")
		}
		if field != "" {
			return writePrecourseFailure(out, "--field 只能指定一次", "")
		}
		if index+1 >= len(args) {
			return writePrecourseFailure(out, "--field requires a value", "")
		}
		field = strings.TrimSpace(args[index+1])
		index++
	}
	if field != "teacherName" && field != "courseName" && field != "college" {
		return writePrecourseFailure(out, "field 必须是 teacherName、courseName 或 college", "")
	}
	return requestPrecourse("popular", url.Values{"field": {field}}, out)
}

func requestPrecourse(operation string, values url.Values, out io.Writer) int {
	response, clientErr := queryPrecourse(operation, values)
	if clientErr != nil {
		if clientErr.reportUsage {
			reportPrecourseUsage(operation, "failure")
		}
		return writePrecourseFailure(out, clientErr.message, clientErr.hint)
	}
	if response.status < 200 || response.status >= 300 {
		message := precourseResponseMessage(response.body, fmt.Sprintf("预选课服务返回 HTTP %d", response.status))
		reportPrecourseUsage(operation, "failure")
		return writePrecourseFailure(out, message, "请稍后重试")
	}
	if !isPrecourseSuccessCode(response.body["code"]) {
		message := precourseResponseMessage(response.body, "预选课服务拒绝了查询请求")
		reportPrecourseUsage(operation, "failure")
		return writePrecourseFailure(out, message, "请检查查询条件后重试")
	}
	result := precourseResult(operation, response)
	reportPrecourseUsage(operation, "success")
	return writeJSON(out, success("precourse", result))
}

func precourseResponseMessage(body map[string]any, fallback string) string {
	message, ok := body["message"].(string)
	if !ok || strings.TrimSpace(message) == "" {
		return fallback
	}
	return strings.TrimSpace(message)
}

func precourseResult(operation string, response precourseResponse) payload {
	result := payload{}
	if data, ok := response.body["data"].(map[string]any); ok {
		for key, value := range data {
			result[key] = value
		}
	} else {
		result["data"] = response.body["data"]
	}
	result["operation"] = operation
	result["url"] = response.url
	return result
}

func nonEmptyValues(values url.Values) url.Values {
	result := url.Values{}
	for key, entries := range values {
		for _, value := range entries {
			if strings.TrimSpace(value) != "" {
				result.Add(key, strings.TrimSpace(value))
			}
		}
	}
	return result
}

func isPrecourseSuccessCode(code any) bool {
	switch value := code.(type) {
	case float64:
		return value == 0
	case string:
		return value == "0" || value == "OK"
	default:
		return false
	}
}

func writePrecourseFailure(out io.Writer, message, hint string) int {
	return writeJSON(out, failure("precourse", message, hint)) + 1
}
