package qfnu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"
)

const telemetryEndpoint = "https://hub.easy-qfnu.top/v1/telemetry/events"

type telemetryEvent struct {
	Feature    string `json:"feature"`
	Status     string `json:"status"`
	CLIVersion string `json:"cli_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	OccurredAt string `json:"occurred_at"`
}

type telemetryClient struct {
	endpoint string
	http     *http.Client
	now      func() time.Time
}

var reportAnonymousEvent = func(feature, status string) error {
	client := telemetryClient{
		endpoint: telemetryEndpoint,
		http:     &http.Client{Timeout: 2 * time.Second},
		now:      time.Now,
	}
	return client.send(feature, status)
}

func (c telemetryClient) send(feature, status string) error {
	event := telemetryEvent{
		Feature:    feature,
		Status:     status,
		CLIVersion: version,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		OccurredAt: c.now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "easy-qfnu/"+version)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	if bodyErr := discardResponseBody(resp); bodyErr != nil {
		return bodyErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry HTTP %d", resp.StatusCode)
	}
	return nil
}

func reportLoginSuccess(result payload) {
	result["telemetry_notice"] = "已触发匿名登录时间上报；不含学号、姓名、Cookie 或其他身份信息"
	if err := reportAnonymousEvent("jwxt.login", "success"); err != nil {
		// 匿名统计是旁路能力，网络或服务异常不能改变登录成功结果。
		return
	}
}
