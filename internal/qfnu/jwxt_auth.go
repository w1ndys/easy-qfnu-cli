package qfnu

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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
	preparedCaptcha, err := c.prepareLoginCaptcha(captcha)
	if err != nil {
		return nil, err
	}
	loginBody, err := c.submitLogin(username, password, preparedCaptcha)
	if err != nil {
		return nil, err
	}
	if err := validateLoginResponse(loginBody); err != nil {
		return nil, err
	}
	return c.buildLoginResult(username, password, saveCredentials)
}

func (c *jwxtClient) prepareLoginCaptcha(captcha string) (string, error) {
	if captcha != "" {
		return captcha, nil
	}
	if err := c.resetJar(); err != nil {
		return "", err
	}
	if err := c.initSession(); err != nil {
		return "", err
	}
	image, err := c.fetchCaptcha()
	if err != nil {
		return "", err
	}
	result, err := c.recognize(image)
	if err != nil {
		return "", &jwxtError{message: err.Error(), hint: "部署独立 ddddocr 服务，或运行 easy-qfnu jwxt captcha 后手动传入验证码"}
	}
	return result, nil
}

func (c *jwxtClient) submitLogin(username, password, captcha string) (string, error) {
	scode, sxh, err := c.loginSessionCodes()
	if err != nil {
		return "", err
	}
	encoded := encodeCredentials(username, password, scode, sxh)
	form := url.Values{"userAccount": {""}, "userPassword": {""}, "RANDOMCODE": {captcha}, "encoded": {encoded}}
	_, _, loginBody, err := c.textSameOrigin(http.MethodPost, loginURL, strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	return loginBody, err
}

func (c *jwxtClient) loginSessionCodes() (string, string, error) {
	if len(c.jar.Cookies(jwxtOriginURL())) == 0 {
		return "", "", &jwxtError{message: "no active captcha session", hint: "先运行 easy-qfnu jwxt captcha，再用 --captcha 提交识别结果"}
	}
	status, _, raw, err := c.text(http.MethodPost, sessURL, strings.NewReader(""), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil || status >= 400 {
		return "", "", &jwxtError{message: "failed to obtain scode/sxh"}
	}
	parts := strings.SplitN(strings.TrimSpace(raw), "#", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", &jwxtError{message: "invalid scode/sxh"}
	}
	return parts[0], parts[1], nil
}

func validateLoginResponse(raw string) error {
	if message := parseLoginMessage(raw); message != "" {
		return &jwxtError{message: message, hint: loginFailureHint(message)}
	}
	if containsAny(raw, []string{"密码错误", "用户名或密码错误", "用户名密码错误", "您提供的用户名或者密码有误"}) {
		return &jwxtError{message: "username or password is wrong", hint: "核对学号和学校服务大厅密码，不要重复提交错误密码"}
	}
	if containsAny(raw, []string{"验证码错误", "验证码不正确"}) {
		return &jwxtError{message: "captcha rejected by 教务系统", hint: "重新运行 easy-qfnu jwxt captcha 获取新验证码"}
	}
	return nil
}

func (c *jwxtClient) buildLoginResult(username, password string, saveCredentials bool) (payload, error) {
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
	recordCredentialSave(result, username, password, saveCredentials)
	return result, nil
}

func recordCredentialSave(result payload, username, password string, enabled bool) {
	result["credentials_saved"] = false
	if !enabled {
		return
	}
	if err := saveCredentialsFile(username, password); err != nil {
		result["hint"] = err.Error()
		return
	}
	result["credentials_saved"] = true
	result["credentials_path"] = defaultCredentialsPath()
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
