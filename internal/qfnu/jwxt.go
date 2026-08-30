package qfnu

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	jwxtBase       = "http://zhjw.qfnu.edu.cn"
	captchaURL     = jwxtBase + "/verifycode.servlet"
	sessURL        = jwxtBase + "/Logon.do?method=logon&flag=sess"
	loginURL       = jwxtBase + "/Logon.do?method=logonLdap"
	mainURL        = jwxtBase + "/jsxsd/framework/xsMain.jsp"
	profileURL     = jwxtBase + "/jsxsd/framework/xsMain_new.jsp?t1=1"
	gradeURL       = jwxtBase + "/jsxsd/kscj/cjcx_list"
	scheduleURL    = jwxtBase + "/jsxsd/xskb/xskb_list.do"
	evaluationFind = jwxtBase + "/jsxsd/xspj/xspj_find.do"
)

type jwxtError struct{ message, hint string }

func (e *jwxtError) Error() string { return e.message }

type sessionFile struct {
	Cookies        []sessionCookie `json:"cookies"`
	Username       string          `json:"username,omitempty"`
	CaptchaPending bool            `json:"captcha_pending,omitempty"`
	Profile        payload         `json:"profile,omitempty"`
	UpdatedAt      string          `json:"updated_at,omitempty"`
}

type sessionCookie struct {
	Name, Value, Path, Domain string
	Expires                   time.Time
	Secure                    bool
}

type jwxtClient struct {
	sessionPath string
	ocrURL      string
	jar         *cookiejar.Jar
	http        *http.Client
	meta        sessionFile
}

func stateDir() string {
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return filepath.Join(value, "easy-qfnu-skill")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".local", "state", "easy-qfnu-skill")
	}
	return filepath.Join(home, ".local", "state", "easy-qfnu-skill")
}

func defaultSessionPath() string {
	if value := os.Getenv("QFNU_JWXT_COOKIE_PATH"); value != "" {
		return expandPath(value)
	}
	if value := os.Getenv("QFNU_JWXT_SESSION_PATH"); value != "" {
		return expandPath(value)
	}
	return filepath.Join(stateDir(), "jwxt-session.json")
}

func defaultCredentialsPath() string {
	if value := os.Getenv("QFNU_JWXT_CREDENTIALS_PATH"); value != "" {
		return expandPath(value)
	}
	return filepath.Join(stateDir(), "jwxt-credentials.json")
}

func defaultCaptchaPath() string { return filepath.Join(stateDir(), "jwxt-captcha.png") }

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func newJWXTClient(sessionPath, ocrURL string) *jwxtClient {
	jar, _ := cookiejar.New(nil)
	client := &jwxtClient{sessionPath: defaultSessionPath(), ocrURL: strings.TrimRight(ocrURL, "/"), jar: jar}
	if sessionPath != "" {
		client.sessionPath = expandPath(sessionPath)
	}
	client.http = &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	client.load()
	return client
}

func mustURL(value string) *url.URL { parsed, _ := url.Parse(value); return parsed }

func (c *jwxtClient) load() {
	data, err := os.ReadFile(c.sessionPath)
	if err != nil {
		return
	}
	var saved sessionFile
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	c.meta = saved
	for _, item := range saved.Cookies {
		cookie := &http.Cookie{Name: item.Name, Value: item.Value, Path: item.Path, Domain: item.Domain, Expires: item.Expires, Secure: item.Secure}
		c.jar.SetCookies(mustURL(jwxtBase), []*http.Cookie{cookie})
	}
}

func (c *jwxtClient) resetJar() {
	jar, _ := cookiejar.New(nil)
	c.jar = jar
	c.http.Jar = jar
	c.meta = sessionFile{}
}

func (c *jwxtClient) persist(fields payload) error {
	for key, value := range fields {
		switch key {
		case "username":
			c.meta.Username, _ = value.(string)
		case "captcha_pending":
			c.meta.CaptchaPending, _ = value.(bool)
		case "profile":
			if profile, ok := value.(payload); ok {
				c.meta.Profile = profile
			}
		}
	}
	c.meta.Cookies = nil
	for _, item := range c.jar.Cookies(mustURL(jwxtBase)) {
		c.meta.Cookies = append(c.meta.Cookies, sessionCookie{Name: item.Name, Value: item.Value, Path: item.Path, Domain: item.Domain, Expires: item.Expires, Secure: item.Secure})
	}
	c.meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Dir(c.sessionPath), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.sessionPath, append(data, '\n'), 0600)
}

func (c *jwxtClient) clear() {
	_ = os.Remove(c.sessionPath)
	c.resetJar()
}

func (c *jwxtClient) request(method, target string, body io.Reader, headers map[string]string) (int, string, []byte, error) {
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return 0, "", nil, err
	}
	req.Header.Set("User-Agent", "easy-qfnu-skill/qfnu")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, target, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Request.URL.String(), data, err
}

func (c *jwxtClient) text(method, target string, body io.Reader, headers map[string]string) (int, string, string, error) {
	status, finalURL, data, err := c.request(method, target, body, headers)
	return status, finalURL, string(data), err
}

func encodeCredentials(username, password, scode, sxh string) string {
	raw := username + "%%%" + password
	chars := []rune(raw)
	var out strings.Builder
	scodePos := 0
	for i, r := range chars {
		if i >= 20 {
			out.WriteString(string(chars[i:]))
			break
		}
		out.WriteRune(r)
		if i >= len(sxh) {
			continue
		}
		count := int(sxh[i] - '0')
		if count <= 0 || scodePos >= len(scode) {
			continue
		}
		end := scodePos + count
		if end > len(scode) {
			end = len(scode)
		}
		out.WriteString(scode[scodePos:end])
		scodePos = end
	}
	return out.String()
}

func (c *jwxtClient) initSession() error {
	status, _, _, err := c.request(http.MethodGet, jwxtBase+"/", nil, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("session init HTTP %d", status)
	}
	return nil
}

func (c *jwxtClient) fetchCaptcha() ([]byte, error) {
	status, _, data, err := c.request(http.MethodGet, captchaURL, nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || len(data) == 0 {
		return nil, fmt.Errorf("captcha image HTTP %d", status)
	}
	return data, nil
}

func (c *jwxtClient) captcha(out string) (payload, error) {
	c.resetJar()
	if err := c.initSession(); err != nil {
		return nil, err
	}
	image, err := c.fetchCaptcha()
	if err != nil {
		return nil, err
	}
	path, err := filepath.Abs(expandPath(out))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, image, 0600); err != nil {
		return nil, err
	}
	if err := c.persist(payload{"captcha_pending": true}); err != nil {
		return nil, err
	}
	return success("jwxt", payload{"captcha_image_path": path, "session_path": c.sessionPath, "next": "easy-qfnu jwxt login --username <学号> --password <密码> --captcha <识图结果>", "hint": "请用模型或用户读取验证码；验证码错误时重新运行 jwxt captcha"}), nil
}

func (c *jwxtClient) recognize(image []byte) (string, error) {
	if c.ocrURL == "" {
		return "", &jwxtError{message: "OCR URL is not configured", hint: "设置 QFNU_OCR_URL 或使用 jwxt login --captcha"}
	}
	form := url.Values{"image": {base64.StdEncoding.EncodeToString(image)}}
	status, _, data, err := c.request(http.MethodPost, c.ocrURL+"/ocr", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("OCR HTTP %d", status)
	}
	var response struct {
		Code    any    `json:"code"`
		Data    string `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", err
	}
	code := fmt.Sprint(response.Code)
	if response.Data == "" || (code != "200" && code != "0" && code != "<nil>") {
		return "", fmt.Errorf("OCR rejected captcha: %s", response.Message)
	}
	return strings.TrimSpace(response.Data), nil
}

func (c *jwxtClient) login(username, password, captcha string, saveCredentials bool) (payload, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, &jwxtError{message: "username/password required", hint: "传入 --username/--password 或设置 QFNU_JWXT_USERNAME/QFNU_JWXT_PASSWORD"}
	}
	if captcha == "" {
		c.resetJar()
		if err := c.initSession(); err != nil {
			return nil, err
		}
		image, err := c.fetchCaptcha()
		if err != nil {
			return nil, err
		}
		captcha, err = c.recognize(image)
		if err != nil {
			return nil, &jwxtError{message: err.Error(), hint: "部署独立 ddddocr 服务，或运行 easy-qfnu jwxt captcha 后手动传入验证码"}
		}
	}
	if len(c.jar.Cookies(mustURL(jwxtBase))) == 0 {
		return nil, &jwxtError{message: "no active captcha session", hint: "先运行 easy-qfnu jwxt captcha，再用 --captcha 提交识别结果"}
	}
	status, _, sess, err := c.text(http.MethodPost, sessURL, strings.NewReader(""), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil || status >= 400 {
		return nil, &jwxtError{message: "failed to obtain scode/sxh"}
	}
	parts := strings.SplitN(strings.TrimSpace(sess), "#", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, &jwxtError{message: "invalid scode/sxh"}
	}
	encoded := encodeCredentials(username, password, parts[0], parts[1])
	form := url.Values{"userAccount": {""}, "userPassword": {""}, "RANDOMCODE": {captcha}, "encoded": {encoded}}
	_, _, loginBody, err := c.text(http.MethodPost, loginURL, strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil {
		return nil, err
	}
	if containsAny(loginBody, []string{"密码错误", "用户名或密码错误", "用户名密码错误", "您提供的用户名或者密码有误"}) {
		return nil, &jwxtError{message: "username or password is wrong", hint: "核对学号和学校服务大厅密码，不要重复提交错误密码"}
	}
	if containsAny(loginBody, []string{"验证码错误", "验证码不正确"}) {
		return nil, &jwxtError{message: "captcha rejected by 教务系统", hint: "重新运行 easy-qfnu jwxt captcha 获取新验证码"}
	}
	status, _, main, err := c.text(http.MethodGet, mainURL, nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || !containsAny(main, []string{"教学一体化服务平台", "glyphicon-class"}) {
		return nil, &jwxtError{message: "login failed: success marker missing on xsMain.jsp"}
	}
	profile := parseProfile(main)
	if bodyStatus, _, body, _ := c.text(http.MethodGet, profileURL, nil, nil); bodyStatus == http.StatusOK {
		mergeProfile(profile, parseProfile(body))
	}
	if err := c.persist(payload{"username": username, "captcha_pending": false, "profile": profile}); err != nil {
		return nil, err
	}
	result := success("jwxt", payload{"logged_in": true, "username": username, "profile": profile, "main_url": mainURL, "session_path": c.sessionPath, "captcha": "vision"})
	if c.ocrURL != "" {
		result["ocr_url"] = c.ocrURL
	}
	if saveCredentials {
		if err := saveCredentialsFile(username, password); err != nil {
			result["credentials_saved"] = false
			result["hint"] = err.Error()
		} else {
			result["credentials_saved"] = true
			result["credentials_path"] = defaultCredentialsPath()
		}
	} else {
		result["credentials_saved"] = false
	}
	return result, nil
}

func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

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

func loadCredentialsFile() (string, string) {
	data, err := os.ReadFile(defaultCredentialsPath())
	if err != nil {
		return "", ""
	}
	var value struct{ Username, Password string }
	if json.Unmarshal(data, &value) != nil {
		return "", ""
	}
	return value.Username, value.Password
}

func saveCredentialsFile(username, password string) error {
	path := defaultCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(map[string]string{"username": username, "password": password}, "", "  ")
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func clearCredentialsFile() bool { return os.Remove(defaultCredentialsPath()) == nil }

func runJWXT(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Fprintln(out, "Usage: easy-qfnu jwxt <captcha|login|grades|schedule|evaluations|evaluate|status|logout|forget-credentials>")
		return 2
	}
	action := args[0]
	var ocrURL, sessionPath, username, password, captcha, output, semester, week, mode string
	var save, saveSet, forget, confirm bool
	targetScore := 89
	var courses []string
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--confirm" {
			confirm = true
			continue
		}
		if arg == "--forget-credentials" || arg == "--clear-credentials" {
			forget = true
			continue
		}
		if arg == "--save-credentials" {
			saveSet = true
			save = true
			if i+1 < len(args) && (args[i+1] == "yes" || args[i+1] == "no") {
				save = args[i+1] == "yes"
				i++
			}
			continue
		}
		if i+1 >= len(args) {
			return writeJSON(out, failure("jwxt", arg+" requires a value", ""))
		}
		value := args[i+1]
		switch arg {
		case "--ocr-url":
			ocrURL = value
		case "--session-path":
			sessionPath = value
		case "--username", "-u":
			username = value
		case "--password", "-p":
			password = value
		case "--captcha":
			captcha = value
		case "--out", "-o":
			output = value
		case "--semester", "--kksj", "--xnxq01id":
			semester = value
		case "--week", "--zc":
			week = value
		case "--kbjcmsid":
			mode = value
		case "--score", "--target-score":
			targetScore, _ = strconv.Atoi(value)
		case "--course":
			courses = append(courses, strings.Split(value, ",")...)
		default:
			return writeJSON(out, failure("jwxt", "unknown option: "+arg, ""))
		}
		i++
	}
	if ocrURL == "" {
		ocrURL = os.Getenv("QFNU_OCR_URL")
	}
	if !saveSet && strings.EqualFold(os.Getenv("QFNU_JWXT_SAVE_CREDENTIALS"), "yes") {
		save = true
	}
	client := newJWXTClient(sessionPath, ocrURL)
	if action == "forget-credentials" {
		return writeJSON(out, success("jwxt", payload{"credentials_removed": clearCredentialsFile(), "credentials_path": defaultCredentialsPath()}))
	}
	if action == "logout" {
		client.clear()
		result := success("jwxt", payload{"logged_in": false, "session_path": client.sessionPath})
		if forget {
			result["credentials_removed"] = clearCredentialsFile()
			result["credentials_path"] = defaultCredentialsPath()
		}
		return writeJSON(out, result)
	}
	var result payload
	var err error
	switch action {
	case "captcha":
		if output == "" {
			output = defaultCaptchaPath()
		}
		result, err = client.captcha(output)
	case "login":
		if username == "" {
			username = os.Getenv("QFNU_JWXT_USERNAME")
		}
		if password == "" {
			password = os.Getenv("QFNU_JWXT_PASSWORD")
		}
		if username == "" || password == "" {
			username, password = loadCredentialsFile()
		}
		result, err = client.login(username, password, captcha, save)
	case "status", "whoami":
		result, err = client.status()
	case "grades":
		result, err = client.grades(semester)
	case "schedule":
		result, err = client.schedule(semester, week, mode)
	case "evaluations":
		result, err = client.evaluations()
	case "evaluate":
		result, err = client.evaluate(targetScore, courses, confirm)
	default:
		err = fmt.Errorf("unknown action: %s", action)
	}
	if err != nil {
		if e, ok := err.(*jwxtError); ok {
			result = failure("jwxt", e.message, e.hint)
		} else {
			result = failure("jwxt", err.Error(), "请检查网络和本地会话后重试")
		}
		return writeJSON(out, result)
	}
	return writeJSON(out, result)
}

func (c *jwxtClient) status() (payload, error) {
	if len(c.jar.Cookies(mustURL(jwxtBase))) == 0 {
		return success("jwxt", payload{"logged_in": false, "session_path": c.sessionPath, "hint": "run easy-qfnu jwxt login first"}), nil
	}
	status, finalURL, main, err := c.text(http.MethodGet, mainURL, nil, nil)
	if err != nil {
		return success("jwxt", payload{"logged_in": false, "session_path": c.sessionPath, "error": err.Error()}), nil
	}
	if status != http.StatusOK || containsAny(main, []string{"请输入账号", "请输入密码", "请输入验证码"}) || !containsAny(main, []string{"教学一体化服务平台", "glyphicon-class"}) {
		if c.ocrURL != "" {
			username, password := loadCredentialsFile()
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
	if pstatus, _, body, _ := c.text(http.MethodGet, profileURL, nil, nil); pstatus == http.StatusOK {
		mergeProfile(profile, parseProfile(body))
	}
	_ = c.persist(payload{"profile": profile, "username": c.meta.Username})
	return success("jwxt", payload{"logged_in": true, "username": c.meta.Username, "profile": profile, "main_url": finalURL, "session_path": c.sessionPath}), nil
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

func evaluationListURL(raw string) string {
	linkRE := regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a\s*>`)
	for _, match := range linkRE.FindAllStringSubmatch(raw, -1) {
		href := attr(match[1], "href")
		if href != "" && (strings.Contains(href, "/jsxsd/xspj/xspj_list.do") || strings.Contains(stripTags(match[2]), "进入评价")) {
			return resolveURL(evaluationFind, href)
		}
	}
	return ""
}

func evaluationRows(raw string) []payload {
	tableRE := regexp.MustCompile(`(?is)<table\b[^>]*id=["']dataList["'][^>]*>(.*?)</table\s*>`)
	table := raw
	if match := tableRE.FindStringSubmatch(raw); len(match) > 1 {
		table = match[1]
	}
	rowRE := regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr\s*>`)
	cellRE := regexp.MustCompile(`(?is)<(?:td|th)\b[^>]*>(.*?)</(?:td|th)\s*>`)
	rows := rowRE.FindAllStringSubmatch(table, -1)
	if len(rows) < 2 {
		return []payload{}
	}
	var headers []string
	for _, match := range cellRE.FindAllStringSubmatch(rows[0][1], -1) {
		headers = append(headers, stripTags(match[1]))
	}
	items := make([]payload, 0)
	for index, rowMatch := range rows[1:] {
		cells := cellRE.FindAllStringSubmatch(rowMatch[1], -1)
		values := make([]string, len(cells))
		for i, cell := range cells {
			values[i] = stripTags(cell[1])
		}
		item := payload{"id": strconv.Itoa(index), "course_name": "未知课程", "teacher_name": "未知教师", "status": "未评"}
		for i, header := range headers {
			if i >= len(values) {
				continue
			}
			switch strings.TrimSpace(header) {
			case "课程名称", "课程":
				item["course_name"] = values[i]
			case "授课教师", "教师":
				item["teacher_name"] = values[i]
			case "是否提交", "提交状态":
				if strings.Contains(values[i], "是") || strings.Contains(values[i], "已") {
					item["status"] = "已提交"
				}
			case "已评", "评价状态":
				if strings.Contains(values[i], "是") || strings.Contains(values[i], "已") {
					item["status"] = "已评"
				}
			case "操作":
				if link := regexp.MustCompile(`(?is)<a\b([^>]*)>`).FindStringSubmatch(cells[i][1]); len(link) > 1 {
					item["href"] = resolveURL(evaluationFind, attr(link[1], "href"))
				}
			}
		}
		items = append(items, item)
	}
	return items
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
	formRE := regexp.MustCompile(`(?is)<form\b[^>]*id=["']Form1["'][^>]*>(.*?)</form\s*>`)
	form := raw
	if match := formRE.FindStringSubmatch(raw); len(match) > 1 {
		form = match[1]
	}
	detail := &evaluationDetail{Summary: summary, Static: url.Values{}, Options: map[string][]payload{}}
	inputRE := regexp.MustCompile(`(?is)<input\b[^>]*>`)
	for _, tag := range inputRE.FindAllString(form, -1) {
		name, value, typ := attr(tag, "name"), attr(tag, "value"), strings.ToLower(attr(tag, "type"))
		if name == "" {
			continue
		}
		if typ == "hidden" && name != "pj06xh" {
			detail.Static.Set(name, value)
		}
	}
	rowRE := regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr\s*>`)
	for _, row := range rowRE.FindAllStringSubmatch(form, -1) {
		indicator := ""
		for _, tag := range inputRE.FindAllString(row[1], -1) {
			if attr(tag, "name") == "pj06xh" {
				indicator = attr(tag, "value")
				break
			}
		}
		if indicator == "" {
			continue
		}
		title := stripTags(row[1])
		options := make([]payload, 0)
		radioRE := regexp.MustCompile(`(?is)<input\b[^>]*type=["']radio["'][^>]*>`)
		for _, tag := range radioRE.FindAllString(row[1], -1) {
			id := attr(tag, "value")
			if id == "" {
				continue
			}
			option := payload{"option_id": id, "label": id, "score": float64(0)}
			if m := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`).FindStringSubmatch(row[1]); len(m) > 1 {
				option["score"], _ = strconv.ParseFloat(m[1], 64)
			}
			options = append(options, option)
		}
		if len(options) > 0 {
			detail.IDs = append(detail.IDs, indicator)
			detail.Options[indicator] = options
			_ = title
		}
	}
	if len(detail.IDs) == 0 {
		return nil, &jwxtError{message: "evaluation indicators not found", hint: "当前课程的评教指标无法解析"}
	}
	return detail, nil
}

func (c *jwxtClient) evaluationDetail(summary payload) (*evaluationDetail, error) {
	href, _ := summary["href"].(string)
	if href == "" {
		return nil, &jwxtError{message: "evaluation detail link not found", hint: "课程列表没有提供评教链接"}
	}
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

func evaluationPreset(detail *evaluationDetail, target int) (map[string]string, float64) {
	totals := map[int]map[string]string{0: {}}
	for _, id := range detail.IDs {
		next := map[int]map[string]string{}
		for total, selections := range totals {
			for _, option := range detail.Options[id] {
				score, _ := option["score"].(float64)
				value, _ := option["option_id"].(string)
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
	return totals[best], float64(best) / 100
}

func evaluationPreview(detail *evaluationDetail, selections map[string]string) []payload {
	preview := make([]payload, 0, len(detail.IDs))
	for _, id := range detail.IDs {
		for _, option := range detail.Options[id] {
			if option["option_id"] == selections[id] {
				preview = append(preview, payload{"id": id, "option": option["label"], "score": option["score"]})
				break
			}
		}
	}
	return preview
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
			optionID, _ := option["option_id"].(string)
			score, _ := option["score"].(float64)
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

func (c *jwxtClient) evaluate(score int, courses []string, confirm bool) (payload, error) {
	if score < 0 || score > 100 {
		return nil, &jwxtError{message: "target score must be between 0 and 100"}
	}
	listing, err := c.evaluations()
	if err != nil {
		return nil, err
	}
	items, _ := listing["items"].([]payload)
	preview := make([]payload, 0)
	type plan struct {
		detail     *evaluationDetail
		selections map[string]string
	}
	plans := make([]plan, 0)
	for _, item := range items {
		if len(courses) > 0 && !containsString(courses, fmt.Sprint(item["id"])) {
			continue
		}
		if status, _ := item["status"].(string); status != "未评" {
			continue
		}
		detail, err := c.evaluationDetail(item)
		if err != nil {
			return nil, err
		}
		selections, total := evaluationPreset(detail, score)
		preview = append(preview, payload{"id": item["id"], "course_name": item["course_name"], "teacher_name": item["teacher_name"], "target_score": score, "total_score": total, "indicators": evaluationPreview(detail, selections)})
		plans = append(plans, plan{detail: detail, selections: selections})
	}
	result := success("jwxt", payload{"action": "evaluate", "target_score": score, "count": len(preview), "items": preview, "evaluation_preview": preview, "session_path": c.sessionPath})
	if score == 100 {
		result["warning"] = "目标 100 分可能触发教务系统的选项限制，建议使用 98 或更低分数"
	}
	if !confirm {
		result["dry_run"], result["requires_confirmation"], result["hint"] = true, len(preview) > 0, "当前为预览模式；确认课程、教师和分数后，重新运行相同命令并追加 --confirm 才会提交"
		return result, nil
	}
	results := make([]payload, 0, len(plans))
	for index, current := range plans {
		item := preview[index]
		entry := payload{"id": item["id"], "course_name": item["course_name"], "teacher_name": item["teacher_name"], "total_score": item["total_score"]}
		message, err := c.submitEvaluation(current.detail, current.selections)
		if err != nil {
			entry["ok"] = false
			entry["error"] = err.Error()
			results = append(results, entry)
			for _, skipped := range preview[index+1:] {
				results = append(results, payload{"id": skipped["id"], "course_name": skipped["course_name"], "teacher_name": skipped["teacher_name"], "ok": false, "skipped": true, "hint": "上一门课程提交结果异常，已停止后续提交；请先核对官方页面"})
			}
			result["ok"] = false
			result["dry_run"], result["submitted"], result["failed"], result["skipped"], result["results"] = false, 0, 1, len(results)-1, results
			result["hint"] = "部分课程提交失败；请根据 error 登录教务系统核对状态"
			return result, nil
		}
		entry["ok"], entry["message"] = true, message
		results = append(results, entry)
	}
	result["dry_run"], result["submitted"], result["failed"], result["skipped"], result["results"] = false, len(results), 0, 0, results
	return result, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
