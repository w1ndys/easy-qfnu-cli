package qfnu

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type relayTarget struct {
	endpoint         string
	method           string
	sendsCookie      bool
	sendsIdempotency bool
}

var relayTargets = map[string]relayTarget{
	"feedback": {
		endpoint:         "https://hub.easy-qfnu.top/v1/feedback",
		method:           http.MethodPost,
		sendsCookie:      true,
		sendsIdempotency: true,
	},
	"recommendation": {
		endpoint:         "https://hub.easy-qfnu.top/v1/recommendation-submissions",
		method:           http.MethodPost,
		sendsCookie:      true,
		sendsIdempotency: true,
	},
	"rank": {
		endpoint:    "https://ranking.easy-qfnu.top/v1/rankings/me",
		method:      http.MethodGet,
		sendsCookie: true,
	},
}

func runJWXTRelay(action string, client *jwxtClient, input io.Reader, out io.Writer) int {
	target, ok := relayTargets[action]
	if !ok {
		return relayFailure(out, "unknown relay action: "+action, "支持 feedback、recommendation、rank")
	}
	body, err := io.ReadAll(input)
	if err != nil {
		return relayFailure(out, "failed to read relay input", "请通过标准输入提供 JSON")
	}
	body = bytes.TrimSpace(body)
	if !json.Valid(body) {
		return relayFailure(out, "relay input must be valid JSON", "请通过标准输入提供 JSON 对象")
	}
	endpoint, requestBody, err := relayRequest(target, body)
	if err != nil {
		return relayFailure(out, err.Error(), "请提供符合该操作约定的 JSON 对象")
	}

	req, err := http.NewRequest(relayMethod(target), endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return relayFailure(out, "failed to build relay request", "请稍后重试")
	}
	req.Header.Set("Accept", "application/json")
	if len(requestBody) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "easy-qfnu/"+version)
	if target.sendsCookie {
		cookie := client.cookieHeader()
		if cookie == "" {
			return relayFailure(out, "no active JWXT session", "请先运行 easy-qfnu jwxt login")
		}
		req.Header.Set("X-QFNU-JWXT-Cookie", cookie)
	}
	if target.sendsIdempotency {
		req.Header.Set("Idempotency-Key", relayIdempotencyKey(body))
	}

	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return relayFailure(out, "relay request failed", "请检查网络和远程服务后重试")
	}
	data, readErr := readResponseBody(response)
	if readErr != nil {
		return relayFailure(out, "failed to read relay response", "请稍后重试")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return relayFailure(out, fmt.Sprintf("relay HTTP %d", response.StatusCode), "远程服务没有返回 JSON")
	}
	if _, err := out.Write(append(data, '\n')); err != nil {
		return 1
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 1
	}
	return 0
}

func relayRequest(target relayTarget, body []byte) (string, []byte, error) {
	if relayMethod(target) != http.MethodGet {
		return target.endpoint, body, nil
	}
	var input struct {
		Scope       string   `json:"scope"`
		CourseCodes []string `json:"course_codes"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return "", nil, fmt.Errorf("rank input must be a JSON object with scope and course_codes")
	}
	query, err := url.Parse(target.endpoint)
	if err != nil {
		return "", nil, fmt.Errorf("failed to build rank request")
	}
	values := query.Query()
	if input.Scope != "" {
		values.Set("scope", input.Scope)
	}
	for _, courseCode := range input.CourseCodes {
		if strings.TrimSpace(courseCode) == "" {
			return "", nil, fmt.Errorf("course_codes cannot contain empty values")
		}
		values.Add("course_code", courseCode)
	}
	query.RawQuery = values.Encode()
	return query.String(), nil, nil
}

func relayMethod(target relayTarget) string {
	if target.method == "" {
		return http.MethodPost
	}
	return target.method
}

func relayFailure(out io.Writer, message, hint string) int {
	if writeJSON(out, failure("jwxt", message, hint)) != 0 {
		return 1
	}
	return 1
}

func relayIdempotencyKey(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func (c *jwxtClient) cookieHeader() string {
	cookies := c.jar.Cookies(jwxtOriginURL())
	values := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if strings.TrimSpace(cookie.Name) != "" {
			values = append(values, cookie.Name+"="+cookie.Value)
		}
	}
	return strings.Join(values, "; ")
}
