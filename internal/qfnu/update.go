package qfnu

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var updateManifestURL = "https://github.com/w1ndys/easy-qfnu-skill/releases/latest/download/manifest.json"

type releaseManifest struct {
	ReleaseVersion string `json:"release_version"`
	CLIVersion     string `json:"cli_version"`
}

func checkUpdates(out io.Writer) int {
	// Development builds are intentionally usable offline. Release builds have
	// a version injected by the release script and must pass this gate.
	if version == "dev" {
		return 0
	}
	manifest, err := fetchReleaseManifest()
	if err != nil {
		writeJSON(out, payload{
			"ok":              false,
			"source":          "easy-qfnu",
			"error":           "无法检查 easy-qfnu 更新",
			"update_required": true,
			"agent_action":    "先恢复公开 Release 清单访问，再重试原命令。",
			"hint":            "网络或公开 Release 清单不可用；为避免运行未知旧版本，当前命令已停止。请稍后重试。",
		})
		return 1
	}

	issues := make([]payload, 0, 1)
	if compareVersions(manifest.ReleaseVersion, version) > 0 {
		issues = append(issues, payload{
			"kind":            "cli",
			"current_version": version,
			"latest_version":  manifest.ReleaseVersion,
			"hint":            "请先安装最新 easy-qfnu CLI，并同步更新 easy-qfnu-skill，再重新运行当前命令。",
			"release_url":     "https://github.com/w1ndys/easy-qfnu-skill/releases/latest",
		})
	}

	if len(issues) == 0 {
		return 0
	}
	return writeJSON(out, payload{
		"ok":              false,
		"source":          "easy-qfnu",
		"error":           "检测到必须完成的更新",
		"update_required": true,
		"agent_action":    "先完成 updates 中的更新；skill 更新后重新读取 SKILL.md，然后重试原命令。",
		"updates":         issues,
	}) + 1
}

func fetchReleaseManifest() (releaseManifest, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, updateManifestURL, nil)
	if err != nil {
		return releaseManifest{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "easy-qfnu/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return releaseManifest{}, err
	}
	if resp.StatusCode != http.StatusOK {
		if bodyErr := discardResponseBody(resp); bodyErr != nil {
			return releaseManifest{}, bodyErr
		}
		return releaseManifest{}, fmt.Errorf("manifest status: %s", resp.Status)
	}
	var manifest releaseManifest
	if err := decodeResponseJSON(resp, &manifest); err != nil {
		return releaseManifest{}, err
	}
	if manifest.ReleaseVersion == "" {
		manifest.ReleaseVersion = manifest.CLIVersion
	}
	if manifest.ReleaseVersion == "" {
		return releaseManifest{}, fmt.Errorf("manifest 缺少 release_version")
	}
	return manifest, nil
}

func compareVersions(left, right string) int {
	leftParts, leftOK := versionParts(left)
	rightParts, rightOK := versionParts(right)
	if !leftOK || !rightOK {
		return strings.Compare(left, right)
	}
	length := len(leftParts)
	if len(rightParts) > length {
		length = len(rightParts)
	}
	for index := 0; index < length; index++ {
		leftValue, rightValue := 0, 0
		if index < len(leftParts) {
			leftValue = leftParts[index]
		}
		if index < len(rightParts) {
			rightValue = rightParts[index]
		}
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

func versionParts(value string) ([]int, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		return nil, false
	}
	parts := strings.Split(value, ".")
	result := make([]int, len(parts))
	for index, part := range parts {
		if part == "" {
			return nil, false
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, false
		}
		result[index] = number
	}
	return result, true
}
