package qfnu

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func parseProfile(raw string) payload {
	profile := payload{}
	labels := map[string]string{"学生姓名": "name", "姓名": "name", "学生编号": "student_id", "学号": "student_id", "所属院系": "college", "专业名称": "major", "班级名称": "class_name"}
	plain := stripTags(raw)
	for label, key := range labels {
		if index := strings.Index(plain, label); index >= 0 {
			value := strings.TrimLeft(strings.TrimSpace(plain[index+len(label):]), " :：\t")
			if fields := strings.Fields(value); len(fields) > 0 {
				profile[key] = fields[0]
			}
		}
	}
	return profile
}

func mergeProfile(dst, src payload) {
	for key, value := range src {
		if text, ok := value.(string); ok && text != "" {
			dst[key] = text
		}
	}
}

func (c *jwxtClient) enrichProfile(profile payload) string {
	status, _, body, err := c.text(http.MethodGet, profileURL, nil, nil)
	if err != nil {
		return "个人资料补全请求失败：" + err.Error()
	}
	if status != http.StatusOK {
		return fmt.Sprintf("个人资料补全返回 HTTP %d", status)
	}
	mergeProfile(profile, parseProfile(body))
	return ""
}

func stripTags(raw string) string {
	raw = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`).ReplaceAllString(raw, " ")
	raw = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`).ReplaceAllString(raw, " ")
	raw = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(raw, " ")
	raw = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(raw, " ")
	return strings.Join(strings.Fields(raw), " ")
}

func parseTable(raw string) [][]string {
	rowRE := regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr\s*>`)
	cellRE := regexp.MustCompile(`(?is)<(?:td|th)\b[^>]*>(.*?)</(?:td|th)\s*>`)
	var rows [][]string
	for _, row := range rowRE.FindAllStringSubmatch(raw, -1) {
		var cells []string
		for _, cell := range cellRE.FindAllStringSubmatch(row[1], -1) {
			cells = append(cells, stripTags(cell[1]))
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
}

func parseGrades(raw, semester string) []payload {
	rows := parseTable(raw)
	if len(rows) < 2 {
		return []payload{}
	}
	keys := map[string]string{"开课学期": "semester", "课程编号": "course_id", "课程名称": "course_name", "分组名": "group_name", "成绩": "score", "成绩标识": "score_flag", "学分": "credits", "总学时": "total_hours", "绩点": "gpa", "补重学期": "makeup_semester", "考核方式": "assessment_method", "考试性质": "exam_nature", "课程属性": "course_attribute", "课程性质": "course_nature", "课程类别": "course_category"}
	var result []payload
	for _, row := range rows[1:] {
		item := payload{}
		for i, header := range rows[0] {
			if i < len(row) {
				if key, ok := keys[strings.TrimSpace(header)]; ok {
					item[key] = row[i]
				}
			}
		}
		if len(item) > 0 {
			if semester != "" {
				item["semester"] = semester
			}
			result = append(result, item)
		}
	}
	return result
}

func parseSchedule(raw string) []payload {
	rows := parseTable(raw)
	var result []payload
	weekdays := map[string]string{"星期一": "周一", "星期二": "周二", "星期三": "周三", "星期四": "周四", "星期五": "周五", "星期六": "周六", "星期日": "周日", "星期天": "周日", "周一": "周一", "周二": "周二", "周三": "周三", "周四": "周四", "周五": "周五", "周六": "周六", "周日": "周日"}
	for _, row := range rows {
		for i, value := range row {
			day := ""
			for label, normalized := range weekdays {
				if strings.Contains(value, label) {
					day = normalized
					break
				}
			}
			if day == "" || i == 0 || strings.TrimSpace(value) == "" {
				continue
			}
			lines := strings.Fields(value)
			result = append(result, payload{"day": day, "period": strconv.Itoa(i), "text": strings.Join(lines, " "), "lines": lines, "course_name": lines[0]})
		}
	}
	return result
}

func (c *jwxtClient) status() (payload, error) {
	if len(c.jar.Cookies(jwxtOriginURL())) == 0 {
		return success("jwxt", payload{"logged_in": false, "session_path": c.sessionPath, "hint": "run easy-qfnu jwxt login first"}), nil
	}
	status, finalURL, main, err := c.text(http.MethodGet, mainURL, nil, nil)
	if err != nil {
		return success("jwxt", payload{"logged_in": false, "session_path": c.sessionPath, "error": err.Error()}), nil
	}
	if status != http.StatusOK || containsAny(main, []string{"请输入账号", "请输入密码", "请输入验证码"}) || !containsAny(main, []string{"教学一体化服务平台", "glyphicon-class"}) {
		if c.ocrURL != "" {
			username, password, credentialErr := loadCredentialsFile()
			if credentialErr != nil {
				return nil, credentialErr
			}
			if username != "" && password != "" {
				if relogin, reloginErr := c.login(username, password, "", false); reloginErr == nil {
					relogin["auto_relogin"] = true
					return relogin, nil
				}
			}
		}
		hint := "run easy-qfnu jwxt login again"
		if c.ocrURL == "" && c.meta.Username != "" {
			hint = "会话已过期且未配置 QFNU_OCR_URL；请运行 easy-qfnu jwxt captcha，再用 easy-qfnu jwxt login --captcha 提交识别结果"
		}
		return success("jwxt", payload{"logged_in": false, "session_path": c.sessionPath, "hint": hint}), nil
	}
	profile := parseProfile(main)
	profileWarning := c.enrichProfile(profile)
	if err := c.persist(payload{"profile": profile, "username": c.meta.Username}); err != nil {
		return nil, err
	}
	result := success("jwxt", payload{"logged_in": true, "username": c.meta.Username, "profile": profile, "main_url": finalURL, "session_path": c.sessionPath})
	if profileWarning != "" {
		// Session validity comes from the main page; expose enrichment failure separately.
		result["profile_warning"] = profileWarning
	}
	return result, nil
}

func (c *jwxtClient) grades(semester string) (payload, error) {
	params := url.Values{}
	if strings.TrimSpace(semester) != "" {
		params.Set("kksj", strings.TrimSpace(semester))
	}
	target := gradeURL
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	status, finalURL, raw, err := c.text(http.MethodGet, target, nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || containsAny(raw, []string{"请输入账号", "请输入密码", "请输入验证码"}) {
		return nil, &jwxtError{message: "grades page requires login", hint: "run easy-qfnu jwxt status or login again"}
	}
	items := parseGrades(raw, semester)
	return success("jwxt", payload{"semester": semester, "count": len(items), "items": items, "grades": items, "url": finalURL, "session_path": c.sessionPath}), nil
}

func (c *jwxtClient) schedule(semester, week, mode string) (payload, error) {
	params := url.Values{"sfFD": {"1"}}
	if semester != "" {
		params.Set("xnxq01id", semester)
	}
	if week != "" {
		params.Set("zc", week)
	}
	if mode != "" {
		params.Set("kbjcmsid", mode)
	}
	status, finalURL, raw, err := c.text(http.MethodGet, scheduleURL+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || containsAny(raw, []string{"请输入账号", "请输入密码", "请输入验证码"}) {
		return nil, &jwxtError{message: "schedule page requires login", hint: "run easy-qfnu jwxt status or login again"}
	}
	items := parseSchedule(raw)
	return success("jwxt", payload{"semester": semester, "week": week, "kbjcmsid": mode, "count": len(items), "items": items, "schedule": items, "url": finalURL, "session_path": c.sessionPath}), nil
}
