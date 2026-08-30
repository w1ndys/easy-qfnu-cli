package qfnu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var updateManifestURL = "https://github.com/w1ndys/easy-qfnu-skill/releases/latest/download/manifest.json"

type releaseManifest struct {
	ReleaseVersion string `json:"release_version"`
	CLIVersion     string `json:"cli_version"`
	SkillVersion   string `json:"skill_version"`
}

type skillInstallation struct {
	path    string
	version string
}

func checkUpdates(out, errOut io.Writer) int {
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

	issues := make([]payload, 0, 2)
	if compareVersions(manifest.CLIVersion, version) > 0 {
		issues = append(issues, payload{
			"kind":            "cli",
			"current_version": version,
			"latest_version":  manifest.CLIVersion,
			"hint":            "请先安装最新 easy-qfnu CLI，再重新运行当前命令。",
			"release_url":     "https://github.com/w1ndys/easy-qfnu-skill/releases/latest",
		})
	}

	if installation, ok := findSkillInstallation(); ok {
		if compareVersions(manifest.SkillVersion, installation.version) > 0 {
			issues = append(issues, payload{
				"kind":            "skill",
				"current_version": installation.version,
				"latest_version":  manifest.SkillVersion,
				"skill_path":      installation.path,
				"hint":            "请先更新 easy-qfnu-skill，重新读取更新后的 SKILL.md，再重试当前命令。",
				"release_url":     "https://github.com/w1ndys/easy-qfnu-skill/releases/latest",
			})
		}
	} else {
		fmt.Fprintln(errOut, "easy-qfnu: 未找到 easy-qfnu-skill/SKILL.md，无法比较 skill 版本；可设置 EASY_QFNU_SKILL_DIR 指向 skill 目录。")
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
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseManifest{}, fmt.Errorf("manifest status: %s", resp.Status)
	}
	var manifest releaseManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return releaseManifest{}, err
	}
	if manifest.CLIVersion == "" || manifest.SkillVersion == "" {
		return releaseManifest{}, fmt.Errorf("manifest 缺少 cli_version 或 skill_version")
	}
	return manifest, nil
}

func findSkillInstallation() (skillInstallation, bool) {
	candidates := make([]string, 0, 12)
	if value := strings.TrimSpace(os.Getenv("EASY_QFNU_SKILL_DIR")); value != "" {
		candidates = append(candidates, value)
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			candidates = append(candidates, dir, filepath.Join(dir, "easy-qfnu-skill"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if codeHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codeHome != "" {
			candidates = append(candidates, filepath.Join(codeHome, "skills", "easy-qfnu-skill"))
		}
		candidates = append(candidates, filepath.Join(home, ".codex", "skills", "easy-qfnu-skill"))
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		path, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
			continue
		}
		if skillVersion, ok := readSkillVersion(filepath.Join(path, "VERSION")); ok {
			return skillInstallation{path: path, version: skillVersion}, true
		}
	}
	return skillInstallation{}, false
}

func readSkillVersion(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	version := strings.TrimSpace(string(data))
	return version, version != ""
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
