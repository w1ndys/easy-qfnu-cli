package qfnu

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
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

// JWXT authentication completes through same-origin redirects; external redirects
// stay un-followed so a server cannot move the session to another origin.
func sameOriginRedirect(origin string) func(*http.Request, []*http.Request) error {
	parsedOrigin, parseErr := url.Parse(origin)
	return func(req *http.Request, _ []*http.Request) error {
		if parseErr != nil || parsedOrigin == nil || req.URL == nil || req.URL.User != nil ||
			!strings.EqualFold(req.URL.Scheme, parsedOrigin.Scheme) ||
			!strings.EqualFold(req.URL.Host, parsedOrigin.Host) {
			return http.ErrUseLastResponse
		}
		return nil
	}
}

func newJWXTClient(sessionPath, ocrURL string) (*jwxtClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create JWXT cookie jar: %w", err)
	}
	client := &jwxtClient{sessionPath: defaultSessionPath(), ocrURL: strings.TrimRight(ocrURL, "/"), jar: jar}
	if sessionPath != "" {
		client.sessionPath = expandPath(sessionPath)
	}
	client.http = &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return client, nil
}

// jwxtOriginURL builds the fixed cookie origin without silently discarding a parse error.
func jwxtOriginURL() *url.URL {
	return &url.URL{Scheme: "http", Host: "zhjw.qfnu.edu.cn"}
}

func (c *jwxtClient) load() error {
	data, err := os.ReadFile(c.sessionPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read JWXT session: %w", err)
	}
	var saved sessionFile
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("parse JWXT session: %w", err)
	}
	c.meta = saved
	for _, item := range saved.Cookies {
		cookie := &http.Cookie{Name: item.Name, Value: item.Value, Path: item.Path, Domain: item.Domain, Expires: item.Expires, Secure: item.Secure}
		c.jar.SetCookies(jwxtOriginURL(), []*http.Cookie{cookie})
	}
	return nil
}

func (c *jwxtClient) resetJar() error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("create JWXT cookie jar: %w", err)
	}
	c.jar = jar
	c.http.Jar = jar
	c.meta = sessionFile{}
	return nil
}

func (c *jwxtClient) persist(fields payload) error {
	for key, value := range fields {
		switch key {
		case "username":
			username, ok := value.(string)
			if !ok {
				return fmt.Errorf("session field %s must be a string", key)
			}
			c.meta.Username = username
		case "captcha_pending":
			pending, ok := value.(bool)
			if !ok {
				return fmt.Errorf("session field %s must be a boolean", key)
			}
			c.meta.CaptchaPending = pending
		case "profile":
			profile, ok := value.(payload)
			if !ok {
				return fmt.Errorf("session field %s must be an object", key)
			}
			c.meta.Profile = profile
		default:
			return fmt.Errorf("unsupported session field: %s", key)
		}
	}
	c.meta.Cookies = nil
	for _, item := range c.jar.Cookies(jwxtOriginURL()) {
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

func (c *jwxtClient) clear() error {
	if err := os.Remove(c.sessionPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear JWXT session: %w", err)
	}
	return c.resetJar()
}

func (c *jwxtClient) request(method, target string, body io.Reader, headers map[string]string) (int, string, []byte, error) {
	return c.requestWithClient(c.http, method, target, body, headers)
}

// The login endpoint hands off to JSXSD through same-origin 302s. Keep regular
// requests manual so authentication-sensitive pages remain explicitly checked.
func (c *jwxtClient) requestSameOrigin(method, target string, body io.Reader, headers map[string]string) (int, string, []byte, error) {
	redirectClient := *c.http
	redirectClient.CheckRedirect = sameOriginRedirect(jwxtBase)
	return c.requestWithClient(&redirectClient, method, target, body, headers)
}

func (c *jwxtClient) requestWithClient(client *http.Client, method, target string, body io.Reader, headers map[string]string) (int, string, []byte, error) {
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return 0, "", nil, err
	}
	req.Header.Set("User-Agent", "easy-qfnu-skill/easy-qfnu")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, target, nil, err
	}
	data, bodyErr := readResponseBody(resp)
	return resp.StatusCode, resp.Request.URL.String(), data, bodyErr
}

func (c *jwxtClient) text(method, target string, body io.Reader, headers map[string]string) (int, string, string, error) {
	status, finalURL, data, err := c.request(method, target, body, headers)
	return status, finalURL, string(data), err
}

func (c *jwxtClient) textSameOrigin(method, target string, body io.Reader, headers map[string]string) (int, string, string, error) {
	status, finalURL, data, err := c.requestSameOrigin(method, target, body, headers)
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
	if err := c.resetJar(); err != nil {
		return nil, err
	}
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
		if err := c.resetJar(); err != nil {
			return nil, err
		}
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
	if len(c.jar.Cookies(jwxtOriginURL())) == 0 {
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
	_, _, loginBody, err := c.textSameOrigin(http.MethodPost, loginURL, strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil {
		return nil, err
	}
	if message := parseLoginMessage(loginBody); message != "" {
		return nil, &jwxtError{message: message, hint: loginFailureHint(message)}
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
	profileWarning := c.enrichProfile(profile)
	if err := c.persist(payload{"username": username, "captcha_pending": false, "profile": profile}); err != nil {
		return nil, err
	}
	result := success("jwxt", payload{"logged_in": true, "username": username, "profile": profile, "main_url": mainURL, "session_path": c.sessionPath, "captcha": "vision"})
	if profileWarning != "" {
		// The main page already proved login; profile enrichment failure is non-fatal.
		result["profile_warning"] = profileWarning
	}
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

func parseLoginMessage(raw string) string {
	startRE := regexp.MustCompile(`(?is)<([a-z][a-z0-9]*)\b[^>]*\bid\s*=\s*["']showMsg["'][^>]*>`)
	match := startRE.FindStringSubmatchIndex(raw)
	if len(match) < 4 {
		return ""
	}
	tag := raw[match[2]:match[3]]
	endRE := regexp.MustCompile(`(?is)</\s*` + regexp.QuoteMeta(tag) + `\s*>`)
	end := endRE.FindStringIndex(raw[match[1]:])
	if end == nil {
		return ""
	}
	message := html.UnescapeString(stripTags(raw[match[1] : match[1]+end[0]]))
	return strings.Join(strings.Fields(message), " ")
}

func loginFailureHint(message string) string {
	switch {
	case containsAny(message, []string{"验证码错误", "验证码不正确"}):
		return "重新运行 easy-qfnu jwxt captcha 获取新验证码"
	case containsAny(message, []string{"密码错误", "用户名或密码错误", "用户名密码错误", "用户名或者密码有误"}):
		return "核对学号和学校服务大厅密码，不要重复提交错误密码"
	case containsAny(message, []string{"其他地方登录", "别处登录", "异地登录"}):
		return "账号已在其他地方登录，请先退出已有会话后再重试"
	default:
		return "请根据教务系统返回的错误核对登录信息后重试"
	}
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

func loadCredentialsFile() (string, string, error) {
	data, err := os.ReadFile(defaultCredentialsPath())
	if errors.Is(err, os.ErrNotExist) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read credentials: %w", err)
	}
	var value struct{ Username, Password string }
	if err := json.Unmarshal(data, &value); err != nil {
		return "", "", fmt.Errorf("parse credentials: %w", err)
	}
	return value.Username, value.Password, nil
}

func saveCredentialsFile(username, password string) error {
	path := defaultCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]string{"username": username, "password": password}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func clearCredentialsFile() (bool, error) {
	err := os.Remove(defaultCredentialsPath())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remove credentials: %w", err)
	}
	return true, nil
}

func runJWXT(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" {
		if _, err := fmt.Fprintln(out, "Usage: easy-qfnu jwxt <captcha|login|grades|schedule|evaluations|evaluate|status|logout|forget-credentials|relay>"); err != nil {
			return 1
		}
		return 2
	}
	action := args[0]
	if action == "relay" {
		if len(args) != 2 {
			return writeJSON(out, failure("jwxt", "relay requires one fixed action", "支持 feedback、recommendation、rank"))
		}
		client, clientErr := newJWXTClient("", "")
		if clientErr != nil {
			return writeJSON(out, failure("jwxt", clientErr.Error(), "无法初始化教务客户端"))
		}
		if loadErr := client.load(); loadErr != nil {
			return writeJSON(out, failure("jwxt", loadErr.Error(), "请检查本地会话文件"))
		}
		return runJWXTRelay(args[1], client, os.Stdin, out)
	}
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
			parsed, parseErr := strconv.Atoi(value)
			if parseErr != nil {
				return writeJSON(out, failure("jwxt", arg+" must be an integer", ""))
			}
			targetScore = parsed
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
	if action == "forget-credentials" {
		removed, err := clearCredentialsFile()
		if err != nil {
			return writeJSON(out, failure("jwxt", err.Error(), "请检查凭据文件权限"))
		}
		return writeJSON(out, success("jwxt", payload{"credentials_removed": removed, "credentials_path": defaultCredentialsPath()}))
	}
	client, clientErr := newJWXTClient(sessionPath, ocrURL)
	if clientErr != nil {
		return writeJSON(out, failure("jwxt", clientErr.Error(), "无法初始化教务客户端"))
	}
	// Captcha, password login, and logout replace or remove the old session;
	// loading a damaged file first would prevent those recovery actions.
	needsSession := action != "captcha" && action != "logout" && (action != "login" || captcha != "")
	if needsSession {
		if loadErr := client.load(); loadErr != nil {
			return writeJSON(out, failure("jwxt", loadErr.Error(), "请检查本地会话文件；可运行 logout 清理损坏会话"))
		}
	}
	if action == "logout" {
		if err := client.clear(); err != nil {
			return writeJSON(out, failure("jwxt", err.Error(), "请检查本地会话文件权限"))
		}
		result := success("jwxt", payload{"logged_in": false, "session_path": client.sessionPath})
		if forget {
			removed, err := clearCredentialsFile()
			if err != nil {
				return writeJSON(out, failure("jwxt", err.Error(), "会话已清理；请检查凭据文件权限"))
			}
			result["credentials_removed"] = removed
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
			loadedUsername, loadedPassword, loadErr := loadCredentialsFile()
			if loadErr != nil {
				return writeJSON(out, failure("jwxt", loadErr.Error(), "请检查凭据文件"))
			}
			username, password = loadedUsername, loadedPassword
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
	if action == "login" {
		reportLoginSuccess(result)
	}
	return writeJSON(out, result)
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
		options := make([]payload, 0)
		radioRE := regexp.MustCompile(`(?is)<input\b[^>]*type=["']radio["'][^>]*>`)
		for _, tag := range radioRE.FindAllString(row[1], -1) {
			id := attr(tag, "value")
			if id == "" {
				continue
			}
			option := payload{"option_id": id, "label": id, "score": float64(0)}
			if m := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`).FindStringSubmatch(row[1]); len(m) > 1 {
				score, err := strconv.ParseFloat(m[1], 64)
				if err != nil {
					return nil, fmt.Errorf("invalid evaluation option score %q: %w", m[1], err)
				}
				option["score"] = score
			}
			options = append(options, option)
		}
		if len(options) > 0 {
			detail.IDs = append(detail.IDs, indicator)
			detail.Options[indicator] = options
		}
	}
	if len(detail.IDs) == 0 {
		return nil, &jwxtError{message: "evaluation indicators not found", hint: "当前课程的评教指标无法解析"}
	}
	return detail, nil
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

func (c *jwxtClient) evaluate(score int, courses []string, confirm bool) (payload, error) {
	if score < 0 || score > 100 {
		return nil, &jwxtError{message: "target score must be between 0 and 100"}
	}
	listing, err := c.evaluations()
	if err != nil {
		return nil, err
	}
	items, ok := listing["items"].([]payload)
	if !ok {
		return nil, errors.New("evaluation listing has invalid items")
	}
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
		status, ok := item["status"].(string)
		if !ok {
			return nil, errors.New("evaluation item has invalid status")
		}
		if status != "未评" {
			continue
		}
		detail, err := c.evaluationDetail(item)
		if err != nil {
			return nil, err
		}
		selections, total, presetErr := evaluationPreset(detail, score)
		if presetErr != nil {
			return nil, presetErr
		}
		indicators, previewErr := evaluationPreview(detail, selections)
		if previewErr != nil {
			return nil, previewErr
		}
		preview = append(preview, payload{"id": item["id"], "course_name": item["course_name"], "teacher_name": item["teacher_name"], "target_score": score, "total_score": total, "indicators": indicators})
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
