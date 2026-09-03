package qfnu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
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
