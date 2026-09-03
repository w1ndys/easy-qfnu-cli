package qfnu

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	evaluationLinkRE   = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a\s*>`)
	evaluationTableRE  = regexp.MustCompile(`(?is)<table\b[^>]*id=["']dataList["'][^>]*>(.*?)</table\s*>`)
	evaluationRowRE    = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr\s*>`)
	evaluationCellRE   = regexp.MustCompile(`(?is)<(?:td|th)\b[^>]*>(.*?)</(?:td|th)\s*>`)
	evaluationAnchorRE = regexp.MustCompile(`(?is)<a\b([^>]*)>`)
	evaluationFormRE   = regexp.MustCompile(`(?is)<form\b[^>]*id=["']Form1["'][^>]*>(.*?)</form\s*>`)
	evaluationInputRE  = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	evaluationRadioRE  = regexp.MustCompile(`(?is)<input\b[^>]*type=["']radio["'][^>]*>`)
	evaluationScoreRE  = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)
)

func evaluationListURL(raw string) string {
	for _, match := range evaluationLinkRE.FindAllStringSubmatch(raw, -1) {
		href := attr(match[1], "href")
		if href != "" && (strings.Contains(href, "/jsxsd/xspj/xspj_list.do") || strings.Contains(stripTags(match[2]), "进入评价")) {
			return resolveURL(evaluationFind, href)
		}
	}
	return ""
}

func evaluationRows(raw string) []payload {
	table := raw
	if match := evaluationTableRE.FindStringSubmatch(raw); len(match) > 1 {
		table = match[1]
	}
	rows := evaluationRowRE.FindAllStringSubmatch(table, -1)
	if len(rows) < 2 {
		return []payload{}
	}
	headers := evaluationHeaders(rows[0][1])
	items := make([]payload, 0, len(rows)-1)
	for index, row := range rows[1:] {
		items = append(items, evaluationRow(index, row[1], headers))
	}
	return items
}

func evaluationHeaders(raw string) []string {
	matches := evaluationCellRE.FindAllStringSubmatch(raw, -1)
	headers := make([]string, 0, len(matches))
	for _, match := range matches {
		headers = append(headers, stripTags(match[1]))
	}
	return headers
}

func evaluationRow(index int, raw string, headers []string) payload {
	cells := evaluationCellRE.FindAllStringSubmatch(raw, -1)
	item := payload{"id": strconv.Itoa(index), "course_name": "未知课程", "teacher_name": "未知教师", "status": "未评"}
	for position, header := range headers {
		if position >= len(cells) {
			continue
		}
		value := stripTags(cells[position][1])
		applyEvaluationCell(item, strings.TrimSpace(header), value, cells[position][1])
	}
	return item
}

func applyEvaluationCell(item payload, header, value, raw string) {
	switch header {
	case "课程名称", "课程":
		item["course_name"] = value
	case "授课教师", "教师":
		item["teacher_name"] = value
	case "是否提交", "提交状态":
		if strings.Contains(value, "是") || strings.Contains(value, "已") {
			item["status"] = "已提交"
		}
	case "已评", "评价状态":
		if strings.Contains(value, "是") || strings.Contains(value, "已") {
			item["status"] = "已评"
		}
	case "操作":
		if link := evaluationAnchorRE.FindStringSubmatch(raw); len(link) > 1 {
			item["href"] = resolveURL(evaluationFind, attr(link[1], "href"))
		}
	}
}

func (c *jwxtClient) evaluations() (payload, error) {
	status, _, raw, err := c.text(http.MethodGet, evaluationFind, nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || containsAny(raw, []string{"请输入账号", "请输入密码", "请输入验证码"}) {
		return nil, &jwxtError{message: "evaluation page requires login", hint: "run easy-qfnu jwxt status or login again"}
	}
	listURL := evaluationListURL(raw)
	if listURL == "" {
		return nil, &jwxtError{message: "no active evaluation batch", hint: "当前没有可用的评教批次"}
	}
	listStatus, finalURL, listRaw, err := c.text(http.MethodGet, listURL, nil, nil)
	if err != nil || listStatus != http.StatusOK {
		return nil, &jwxtError{message: "evaluation list page unavailable", hint: "请检查登录会话后重试"}
	}
	items := evaluationRows(listRaw)
	return success("jwxt", payload{"batch_url": listURL, "page_count": 1, "count": len(items), "items": items, "evaluations": items, "url": finalURL, "session_path": c.sessionPath}), nil
}

type evaluationDetail struct {
	Summary payload
	Static  url.Values
	IDs     []string
	Options map[string][]payload
}

func parseEvaluationDetail(raw string, summary payload) (*evaluationDetail, error) {
	form := raw
	if match := evaluationFormRE.FindStringSubmatch(raw); len(match) > 1 {
		form = match[1]
	}
	detail := &evaluationDetail{Summary: summary, Static: evaluationStaticFields(form), Options: map[string][]payload{}}
	for _, row := range evaluationRowRE.FindAllStringSubmatch(form, -1) {
		indicator, options, err := parseEvaluationIndicator(row[1])
		if err != nil {
			return nil, err
		}
		if indicator == "" || len(options) == 0 {
			continue
		}
		detail.IDs = append(detail.IDs, indicator)
		detail.Options[indicator] = options
	}
	if len(detail.IDs) == 0 {
		return nil, &jwxtError{message: "evaluation indicators not found", hint: "当前课程的评教指标无法解析"}
	}
	return detail, nil
}

func evaluationStaticFields(form string) url.Values {
	fields := url.Values{}
	for _, tag := range evaluationInputRE.FindAllString(form, -1) {
		name := attr(tag, "name")
		typ := strings.ToLower(attr(tag, "type"))
		if name != "" && typ == "hidden" && name != "pj06xh" {
			fields.Set(name, attr(tag, "value"))
		}
	}
	return fields
}

func parseEvaluationIndicator(row string) (string, []payload, error) {
	indicator := ""
	for _, tag := range evaluationInputRE.FindAllString(row, -1) {
		if attr(tag, "name") == "pj06xh" {
			indicator = attr(tag, "value")
			break
		}
	}
	if indicator == "" {
		return "", nil, nil
	}
	options, err := evaluationOptions(row)
	return indicator, options, err
}

func evaluationOptions(row string) ([]payload, error) {
	radios := evaluationRadioRE.FindAllString(row, -1)
	if len(radios) == 0 {
		return nil, nil
	}
	score, err := evaluationScore(row)
	if err != nil {
		return nil, err
	}
	options := make([]payload, 0, len(radios))
	for _, tag := range radios {
		id := attr(tag, "value")
		if id != "" {
			options = append(options, payload{"option_id": id, "label": id, "score": score})
		}
	}
	return options, nil
}

func evaluationScore(row string) (float64, error) {
	match := evaluationScoreRE.FindStringSubmatch(row)
	if len(match) < 2 {
		return 0, nil
	}
	score, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid evaluation option score %q: %w", match[1], err)
	}
	return score, nil
}

func (c *jwxtClient) evaluationDetail(summary payload) (*evaluationDetail, error) {
	href, ok := summary["href"].(string)
	if !ok || strings.TrimSpace(href) == "" {
		return nil, &jwxtError{message: "evaluation detail link not found", hint: "课程列表没有提供评教链接"}
	}
	href = strings.TrimSpace(href)
	parsed, err := url.Parse(href)
	if err != nil || parsed.Host != "zhjw.qfnu.edu.cn" {
		return nil, &jwxtError{message: "evaluation detail URL is outside JWXT host"}
	}
	status, _, raw, err := c.text(http.MethodGet, href, nil, nil)
	if err != nil || status != http.StatusOK {
		return nil, &jwxtError{message: "evaluation detail page unavailable", hint: "请检查登录会话后重试"}
	}
	if containsAny(raw, []string{"请输入账号", "请输入密码", "请输入验证码"}) {
		return nil, &jwxtError{message: "evaluation detail requires login", hint: "请先重新登录教务系统"}
	}
	return parseEvaluationDetail(raw, summary)
}

func evaluationOptionValues(option payload) (string, float64, error) {
	optionID, ok := option["option_id"].(string)
	if !ok || strings.TrimSpace(optionID) == "" {
		return "", 0, errors.New("evaluation option is missing option_id")
	}
	score, ok := option["score"].(float64)
	if !ok {
		return "", 0, fmt.Errorf("evaluation option %s has an invalid score", optionID)
	}
	return optionID, score, nil
}

func evaluationPreset(detail *evaluationDetail, target int) (map[string]string, float64, error) {
	totals := map[int]map[string]string{0: {}}
	for _, id := range detail.IDs {
		next := map[int]map[string]string{}
		for total, selections := range totals {
			for _, option := range detail.Options[id] {
				value, score, err := evaluationOptionValues(option)
				if err != nil {
					return nil, 0, err
				}
				candidate := make(map[string]string, len(selections)+1)
				for key, selected := range selections {
					candidate[key] = selected
				}
				candidate[id] = value
				scaled := int(score*100 + 0.5)
				if _, exists := next[total+scaled]; !exists {
					next[total+scaled] = candidate
				}
			}
		}
		if len(next) == 0 {
			return nil, 0, fmt.Errorf("evaluation indicator %s has no valid options", id)
		}
		totals = next
	}
	best, bestDistance := 0, int(^uint(0)>>1)
	for total := range totals {
		distance := total - target*100
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance || (distance == bestDistance && total > best) {
			best, bestDistance = total, distance
		}
	}
	return totals[best], float64(best) / 100, nil
}

func evaluationPreview(detail *evaluationDetail, selections map[string]string) ([]payload, error) {
	preview := make([]payload, 0, len(detail.IDs))
	for _, id := range detail.IDs {
		found := false
		for _, option := range detail.Options[id] {
			optionID, score, err := evaluationOptionValues(option)
			if err != nil {
				return nil, err
			}
			if optionID != selections[id] {
				continue
			}
			label, ok := option["label"].(string)
			if !ok || strings.TrimSpace(label) == "" {
				return nil, fmt.Errorf("evaluation option %s is missing label", optionID)
			}
			preview = append(preview, payload{"id": id, "option": label, "score": score})
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("evaluation indicator %s has no selected option", id)
		}
	}
	return preview, nil
}

func (c *jwxtClient) submitEvaluation(detail *evaluationDetail, selections map[string]string) (string, error) {
	form := url.Values{}
	for key, values := range detail.Static {
		for _, value := range values {
			form.Add(key, value)
		}
	}
	form.Set("issubmit", "1")
	for _, id := range detail.IDs {
		form.Add("pj06xh", id)
		selected := selections[id]
		if selected == "" {
			return "", fmt.Errorf("evaluation indicator %s has no selection", id)
		}
		form.Set("pj0601id_"+id, selected)
		for _, option := range detail.Options[id] {
			optionID, score, err := evaluationOptionValues(option)
			if err != nil {
				return "", err
			}
			form.Set("pj0601fz_"+id+"_"+optionID, strconv.FormatFloat(score, 'f', -1, 64))
		}
	}
	status, _, raw, err := c.text(http.MethodPost, jwxtBase+"/jsxsd/xspj/xspj_save.do", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Referer": fmt.Sprint(detail.Summary["href"]), "Origin": jwxtBase})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", &jwxtError{message: fmt.Sprintf("evaluation submit HTTP %d", status), hint: "提交状态不确定，请登录教务系统官方页面核对；不会自动重复提交"}
	}
	message := stripTags(raw)
	if containsAny(message, []string{"保存成功", "提交成功", "评价成功"}) {
		return message, nil
	}
	return "", &jwxtError{message: "evaluation submit result was not confirmed", hint: "教务系统未返回成功提示，请登录官方页面核对状态"}
}

type evaluationPlan struct {
	detail     *evaluationDetail
	selections map[string]string
}

func (c *jwxtClient) evaluate(score int, courses []string, confirm bool) (payload, error) {
	if score < 0 || score > 100 {
		return nil, &jwxtError{message: "target score must be between 0 and 100"}
	}
	preview, plans, err := c.buildEvaluationPlans(score, courses)
	if err != nil {
		return nil, err
	}
	result := c.evaluationResult(score, preview)
	if !confirm {
		result["dry_run"], result["requires_confirmation"], result["hint"] = true, len(preview) > 0, "当前为预览模式；确认课程、教师和分数后，重新运行相同命令并追加 --confirm 才会提交"
		return result, nil
	}
	return c.submitEvaluationPlans(result, preview, plans), nil
}

func (c *jwxtClient) buildEvaluationPlans(score int, courses []string) ([]payload, []evaluationPlan, error) {
	listing, err := c.evaluations()
	if err != nil {
		return nil, nil, err
	}
	items, ok := listing["items"].([]payload)
	if !ok {
		return nil, nil, errors.New("evaluation listing has invalid items")
	}
	preview := make([]payload, 0)
	plans := make([]evaluationPlan, 0)
	for _, item := range items {
		if len(courses) > 0 && !containsString(courses, fmt.Sprint(item["id"])) {
			continue
		}
		status, ok := item["status"].(string)
		if !ok {
			return nil, nil, errors.New("evaluation item has invalid status")
		}
		if status != "未评" {
			continue
		}
		previewItem, plan, err := c.buildEvaluationPlan(item, score)
		if err != nil {
			return nil, nil, err
		}
		preview = append(preview, previewItem)
		plans = append(plans, plan)
	}
	return preview, plans, nil
}

func (c *jwxtClient) buildEvaluationPlan(item payload, score int) (payload, evaluationPlan, error) {
	detail, err := c.evaluationDetail(item)
	if err != nil {
		return nil, evaluationPlan{}, err
	}
	selections, total, err := evaluationPreset(detail, score)
	if err != nil {
		return nil, evaluationPlan{}, err
	}
	indicators, err := evaluationPreview(detail, selections)
	if err != nil {
		return nil, evaluationPlan{}, err
	}
	preview := payload{"id": item["id"], "course_name": item["course_name"], "teacher_name": item["teacher_name"], "target_score": score, "total_score": total, "indicators": indicators}
	return preview, evaluationPlan{detail: detail, selections: selections}, nil
}

func (c *jwxtClient) evaluationResult(score int, preview []payload) payload {
	result := success("jwxt", payload{"action": "evaluate", "target_score": score, "count": len(preview), "items": preview, "evaluation_preview": preview, "session_path": c.sessionPath})
	if score == 100 {
		result["warning"] = "目标 100 分可能触发教务系统的选项限制，建议使用 98 或更低分数"
	}
	return result
}

func (c *jwxtClient) submitEvaluationPlans(result payload, preview []payload, plans []evaluationPlan) payload {
	results := make([]payload, 0, len(plans))
	for index, current := range plans {
		item := preview[index]
		entry := payload{"id": item["id"], "course_name": item["course_name"], "teacher_name": item["teacher_name"], "total_score": item["total_score"]}
		message, err := c.submitEvaluation(current.detail, current.selections)
		if err != nil {
			return failedEvaluationResult(result, preview, results, entry, index, err)
		}
		entry["ok"] = true
		entry["message"] = message
		results = append(results, entry)
	}
	result["dry_run"], result["submitted"], result["failed"], result["skipped"], result["results"] = false, len(results), 0, 0, results
	return result
}

func failedEvaluationResult(result payload, preview, results []payload, entry payload, index int, submitErr error) payload {
	entry["ok"] = false
	entry["error"] = submitErr.Error()
	results = append(results, entry)
	for _, skipped := range preview[index+1:] {
		results = append(results, payload{"id": skipped["id"], "course_name": skipped["course_name"], "teacher_name": skipped["teacher_name"], "ok": false, "skipped": true, "hint": "上一门课程提交结果异常，已停止后续提交；请先核对官方页面"})
	}
	result["ok"] = false
	result["dry_run"], result["submitted"], result["failed"], result["skipped"], result["results"] = false, 0, 1, len(results)-1, results
	result["hint"] = "部分课程提交失败；请根据 error 登录教务系统核对状态"
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
