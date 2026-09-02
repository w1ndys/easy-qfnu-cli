package qfnu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultPrecourseEndpoint = "https://precourse.easy-qfnu.top/v1/precourses"

var (
	precourseEndpoint    = defaultPrecourseEndpoint
	precourseHTTPClient  = &http.Client{Timeout: 30 * time.Second}
	reportPrecourseUsage = func(operation, status string) {
		_ = reportAnonymousEvent("precourse."+operation, status)
	}
)

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
		printPrecourseUsage(out)
		return 2
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

func printPrecourseUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: easy-qfnu precourse <search|meta|popular>")
	fmt.Fprintln(out, "  easy-qfnu precourse search [keyword] [--course-code value] [--course-name value] [--teacher-name value]")
	fmt.Fprintln(out, "    [--course-nature value] [--course-attr value] [--college value] [--schedule-time value]")
	fmt.Fprintln(out, "    [--location value] [--campus value]")
	fmt.Fprintln(out, "  easy-qfnu precourse meta")
	fmt.Fprintln(out, "  easy-qfnu precourse popular --field <teacherName|courseName|college>")
}

func runPrecourseSearch(args []string, out io.Writer) int {
	values := url.Values{}
	keyword := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--help" {
			printPrecourseUsage(out)
			return 2
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
			printPrecourseUsage(out)
			return 2
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
	target, err := url.Parse(strings.TrimRight(precourseEndpoint, "/") + "/" + operation)
	if err != nil {
		return writePrecourseFailure(out, "无法构造预选课服务地址", "请稍后重试")
	}
	if values != nil {
		target.RawQuery = values.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return writePrecourseFailure(out, "无法构造预选课请求", "请稍后重试")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "easy-qfnu/"+version)
	response, err := precourseHTTPClient.Do(req)
	if err != nil {
		reportPrecourseUsage(operation, "failure")
		return writePrecourseFailure(out, "预选课查询请求失败", "请检查网络和远程服务后重试")
	}
	defer response.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		reportPrecourseUsage(operation, "failure")
		return writePrecourseFailure(out, "预选课服务返回了无效 JSON", "请稍后重试")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := fmt.Sprintf("预选课服务返回 HTTP %d", response.StatusCode)
		if remoteMessage, ok := body["message"].(string); ok && strings.TrimSpace(remoteMessage) != "" {
			message = strings.TrimSpace(remoteMessage)
		}
		reportPrecourseUsage(operation, "failure")
		return writePrecourseFailure(out, message, "请稍后重试")
	}
	if !isPrecourseSuccessCode(body["code"]) {
		message := "预选课服务拒绝了查询请求"
		if remoteMessage, ok := body["message"].(string); ok && strings.TrimSpace(remoteMessage) != "" {
			message = strings.TrimSpace(remoteMessage)
		}
		reportPrecourseUsage(operation, "failure")
		return writePrecourseFailure(out, message, "请检查查询条件后重试")
	}
	result := payload{}
	if data, ok := body["data"].(map[string]any); ok {
		for key, value := range data {
			result[key] = value
		}
	} else {
		result["data"] = body["data"]
	}
	result["operation"] = operation
	result["url"] = target.String()
	reportPrecourseUsage(operation, "success")
	return writeJSON(out, success("precourse", result))
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
