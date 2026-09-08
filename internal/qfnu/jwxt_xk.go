package qfnu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	xkLiveNotice = "这是选课轮次即时查询：数据来自当前开放轮次的教务库，比公开预选课缓存更准确及时。默认会扫描全部选课模块，因此能探测目标课程实际所在模块；网页前端可能按年级隐藏部分模块入口，本查询不受该限制。本命令只读，不会提交选课。"
	xkCachedHint = "当前没有开放的选课轮次时，可改用 easy-qfnu precourse search 查询公开预选课缓存（定时快照，可能滞后）"
)

var (
	xkListURL    = jwxtBase + "/jsxsd/xsxk/xklc_list"
	xkEnterURL   = jwxtBase + "/jsxsd/xsxk/xsxk_index"
	xkSearchRoot = jwxtBase + "/jsxsd/xsxkkc"
)

type xkModule struct {
	Key    string
	Path   string
	ComeIn string
	Name   string
}

var xkModules = []xkModule{
	{Key: "knjxk", Path: "xsxkKnjxk", ComeIn: "comeInKnjxk", Name: "专业内跨年级选课"},
	{Key: "bxqjhxk", Path: "xsxkBxqjhxk", ComeIn: "comeInBxqjhxk", Name: "本学期计划选课"},
	{Key: "xxxk", Path: "xsxkXxxk", ComeIn: "comeInXxxk", Name: "选修选课"},
	{Key: "fawxk", Path: "xsxkFawxk", ComeIn: "comeInFawxk", Name: "计划外选课"},
	{Key: "ggxxkxk", Path: "xsxkGgxxkxk", ComeIn: "comeInGgxxkxk", Name: "公选课选课"},
}

var (
	xkIDFromQuery = regexp.MustCompile(`jx0502zbid=([A-Za-z0-9]+)`)
	xkIDFromCall  = regexp.MustCompile(`(?i)(?:xsxkFun|jrxk)\(['"]([A-Za-z0-9]+)['"]\)`)
)

type xkRound struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type xkSearchQuery struct {
	roundID string
	course  string
	teacher string
	limit   int
	modules []xkModule
}

func runJWXTXK(args []string, out io.Writer) int {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return printXKUsage(out)
		}
	}
	if len(args) == 0 {
		return printXKUsage(out)
	}
	action := args[0]
	query, err := parseXKCommand(action, args[1:])
	if err != nil {
		return writeJSON(out, failure("jwxt", err.Error(), "支持 easy-qfnu jwxt xk rounds 与 easy-qfnu jwxt xk search"))
	}
	client, clientErr := newJWXTClient("", "")
	if clientErr != nil {
		return writeJSON(out, failure("jwxt", clientErr.Error(), "无法初始化教务客户端"))
	}
	if loadErr := client.load(); loadErr != nil {
		return writeJSON(out, failure("jwxt", loadErr.Error(), "请检查本地会话文件"))
	}
	var result payload
	var runErr error
	switch action {
	case "rounds":
		result, runErr = client.xkRounds()
	case "search":
		result, runErr = client.xkSearch(query)
	default:
		return writeJSON(out, failure("jwxt", "unknown xk action: "+action, "支持 rounds、search"))
	}
	if runErr != nil {
		reportUsage("jwxt.xk."+action, "failure")
		if known, ok := runErr.(*jwxtError); ok {
			return writeJSON(out, failure("jwxt", known.message, known.hint))
		}
		return writeJSON(out, failure("jwxt", runErr.Error(), "请检查网络和本地会话后重试"))
	}
	reportUsage("jwxt.xk."+action, "success")
	return writeJSON(out, result)
}

func printXKUsage(out io.Writer) int {
	usage := "Usage: easy-qfnu jwxt xk <rounds|search>\n" +
		"  easy-qfnu jwxt xk rounds\n" +
		"  easy-qfnu jwxt xk search [--round ID] [--module 公选课] [--course 课程] [--teacher 教师] [--limit 50]\n" +
		"  search 默认扫描全部选课模块，结果里的 located_modules 表示目标课程实际所在模块。\n"
	if _, err := fmt.Fprint(out, usage); err != nil {
		return 1
	}
	return 2
}

func parseXKCommand(action string, args []string) (xkSearchQuery, error) {
	if action != "rounds" && action != "search" {
		return xkSearchQuery{}, fmt.Errorf("unknown xk action: %s", action)
	}
	if action == "rounds" {
		if len(args) > 0 {
			return xkSearchQuery{}, fmt.Errorf("rounds 不接受额外参数")
		}
		return xkSearchQuery{}, nil
	}
	query := xkSearchQuery{limit: 50}
	var moduleKeys []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--help" {
			return xkSearchQuery{}, fmt.Errorf("search 用法见 easy-qfnu jwxt xk --help")
		}
		if len(args) < index+2 {
			return xkSearchQuery{}, fmt.Errorf("%s requires a value", arg)
		}
		value := strings.TrimSpace(args[index+1])
		switch arg {
		case "--round":
			query.roundID = value
		case "--module":
			moduleKeys = append(moduleKeys, value)
		case "--course":
			query.course = value
		case "--teacher":
			query.teacher = value
		case "--limit":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return xkSearchQuery{}, fmt.Errorf("--limit must be an integer")
			}
			query.limit = parsed
		default:
			return xkSearchQuery{}, fmt.Errorf("unknown option: %s", arg)
		}
		index++
	}
	if query.limit < 1 || query.limit > 500 {
		return xkSearchQuery{}, fmt.Errorf("--limit 必须是 1 到 500")
	}
	modules, err := resolveXKModules(moduleKeys)
	if err != nil {
		return xkSearchQuery{}, err
	}
	query.modules = modules
	return query, nil
}

func resolveXKModules(keys []string) ([]xkModule, error) {
	if len(keys) == 0 {
		copied := make([]xkModule, len(xkModules))
		copy(copied, xkModules)
		return copied, nil
	}
	var result []xkModule
	seen := map[string]bool{}
	for _, key := range keys {
		mod, ok := findXKModule(key)
		if !ok {
			return nil, fmt.Errorf("unknown module: %s", key)
		}
		if seen[mod.Key] {
			continue
		}
		seen[mod.Key] = true
		result = append(result, mod)
	}
	return result, nil
}

func findXKModule(raw string) (xkModule, bool) {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	aliases := map[string]string{
		"knjxk": "knjxk", "xsxkknjxk": "knjxk", "跨年级": "knjxk", "专业内跨年级选课": "knjxk",
		"bxqjhxk": "bxqjhxk", "xsxkbxqjhxk": "bxqjhxk", "本学期计划": "bxqjhxk", "本学期计划选课": "bxqjhxk", "计划选课": "bxqjhxk",
		"xxxk": "xxxk", "xsxkxxxk": "xxxk", "选修": "xxxk", "选修选课": "xxxk",
		"fawxk": "fawxk", "xsxkfawxk": "fawxk", "计划外": "fawxk", "计划外选课": "fawxk",
		"ggxxkxk": "ggxxkxk", "xsxkggxxkxk": "ggxxkxk", "公选": "ggxxkxk", "公选课": "ggxxkxk", "公选课选课": "ggxxkxk",
	}
	key, ok := aliases[normalized]
	if !ok {
		key, ok = aliases[strings.TrimSpace(raw)]
	}
	if !ok {
		return xkModule{}, false
	}
	for _, mod := range xkModules {
		if mod.Key == key {
			return mod, true
		}
	}
	return xkModule{}, false
}

func (c *jwxtClient) xkRounds() (payload, error) {
	rounds, err := c.fetchXKRounds()
	if err != nil {
		return nil, err
	}
	return success("jwxt", payload{
		"operation":  "xk.rounds",
		"query_kind": "live",
		"notice":     xkLiveNotice,
		"count":      len(rounds),
		"rounds":     rounds,
	}), nil
}

func (c *jwxtClient) xkSearch(query xkSearchQuery) (payload, error) {
	rounds, err := c.fetchXKRounds()
	if err != nil {
		return nil, err
	}
	round, err := pickXKRound(rounds, query.roundID)
	if err != nil {
		return nil, err
	}
	if err := c.enterXKRound(round.ID); err != nil {
		return nil, err
	}
	items := make([]payload, 0)
	for _, mod := range query.modules {
		found, searchErr := c.searchXKModule(mod, query.course, query.teacher)
		if searchErr != nil {
			return nil, searchErr
		}
		items = append(items, found...)
	}
	located := summarizeXKModules(items)
	truncated := len(items) > query.limit
	if truncated {
		items = items[:query.limit]
	}
	return success("jwxt", payload{
		"operation":       "xk.search",
		"query_kind":      "live",
		"notice":          xkLiveNotice,
		"round":           round,
		"scanned_modules": xkModuleKeys(query.modules),
		"located_modules": located,
		"count":           len(items),
		"limit":           query.limit,
		"truncated":       truncated,
		"items":           items,
	}), nil
}

func (c *jwxtClient) fetchXKRounds() ([]xkRound, error) {
	status, _, raw, err := c.text(http.MethodGet, xkListURL, nil, nil)
	if err != nil {
		return nil, err
	}
	if err := xkSessionError(status, raw, false); err != nil {
		return nil, err
	}
	rounds := parseXKRounds(raw)
	if len(rounds) == 0 {
		return nil, &jwxtError{message: "当前没有开放的选课轮次", hint: xkCachedHint}
	}
	return rounds, nil
}

func (c *jwxtClient) enterXKRound(id string) error {
	target := xkEnterURL + "?jx0502zbid=" + url.QueryEscape(id)
	status, _, raw, err := c.text(http.MethodGet, target, nil, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return &jwxtError{message: fmt.Sprintf("进入选课轮次返回 HTTP %d", status), hint: "请确认轮次仍开放后重试"}
	}
	return xkSessionError(status, raw, true)
}

func (c *jwxtClient) searchXKModule(mod xkModule, course, teacher string) ([]payload, error) {
	params := url.Values{
		"kcxx": {course},
		"skls": {teacher},
		"sfym": {"false"},
		"sfct": {"false"},
		"sfxx": {"false"},
	}
	body := url.Values{"iDisplayStart": {"0"}, "iDisplayLength": {"10000"}}
	target := xkSearchRoot + "/" + mod.Path + "?" + params.Encode()
	headers := map[string]string{
		"Content-Type":     "application/x-www-form-urlencoded",
		"Accept":           "application/json, text/javascript, */*; q=0.01",
		"X-Requested-With": "XMLHttpRequest",
		"Referer":          xkSearchRoot + "/" + mod.ComeIn,
	}
	status, _, raw, err := c.text(http.MethodPost, target, strings.NewReader(body.Encode()), headers)
	if err != nil {
		return nil, err
	}
	if err := xkSessionError(status, raw, false); err != nil {
		return nil, err
	}
	return parseXKCourses(raw, mod)
}

func summarizeXKModules(items []payload) []payload {
	counts := map[string]int{}
	names := map[string]string{}
	order := make([]string, 0)
	for _, item := range items {
		key, _ := item["module"].(string)
		name, _ := item["module_name"].(string)
		if key == "" {
			continue
		}
		if counts[key] == 0 {
			order = append(order, key)
			names[key] = name
		}
		counts[key]++
	}
	result := make([]payload, 0, len(order))
	for _, key := range order {
		result = append(result, payload{"key": key, "name": names[key], "count": counts[key]})
	}
	return result
}

func xkModuleKeys(modules []xkModule) []string {
	keys := make([]string, 0, len(modules))
	for _, mod := range modules {
		keys = append(keys, mod.Key)
	}
	return keys
}

func pickXKRound(rounds []xkRound, id string) (xkRound, error) {
	id = strings.TrimSpace(id)
	if id != "" {
		for _, round := range rounds {
			if round.ID == id {
				return round, nil
			}
		}
		return xkRound{}, &jwxtError{message: "未找到指定选课轮次", hint: "先运行 easy-qfnu jwxt xk rounds 查看开放轮次"}
	}
	if len(rounds) == 1 {
		return rounds[0], nil
	}
	return xkRound{}, &jwxtError{message: "当前有多个开放选课轮次，需要指定 --round", hint: "先运行 easy-qfnu jwxt xk rounds，再带上 --round <id>"}
}

func parseXKRounds(raw string) []xkRound {
	order := make([]string, 0)
	seen := map[string]*xkRound{}
	add := func(id string) *xkRound {
		if id == "" {
			return nil
		}
		if round, ok := seen[id]; ok {
			return round
		}
		round := &xkRound{ID: id}
		seen[id] = round
		order = append(order, id)
		return round
	}
	for _, match := range xkIDFromQuery.FindAllStringSubmatch(raw, -1) {
		add(match[1])
	}
	for _, match := range xkIDFromCall.FindAllStringSubmatch(raw, -1) {
		add(match[1])
	}
	rowRE := regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr\s*>`)
	cellRE := regexp.MustCompile(`(?is)<(?:td|th)\b[^>]*>(.*?)</(?:td|th)\s*>`)
	for _, row := range rowRE.FindAllStringSubmatch(raw, -1) {
		id := firstXKRoundID(row[0])
		if id == "" {
			continue
		}
		round := add(id)
		if round == nil {
			continue
		}
		cells := make([]string, 0)
		for _, cell := range cellRE.FindAllStringSubmatch(row[1], -1) {
			cells = append(cells, stripTags(cell[1]))
		}
		if len(cells) == 0 || strings.Contains(cells[0], "选课轮次名称") {
			continue
		}
		if round.Name == "" {
			round.Name = cells[0]
		}
		if round.Start == "" && len(cells) > 1 {
			round.Start = cells[1]
		}
		if round.End == "" && len(cells) > 2 {
			round.End = cells[2]
		}
	}
	rounds := make([]xkRound, 0, len(order))
	for _, id := range order {
		rounds = append(rounds, *seen[id])
	}
	return rounds
}

func firstXKRoundID(raw string) string {
	if match := xkIDFromQuery.FindStringSubmatch(raw); len(match) == 2 {
		return match[1]
	}
	if match := xkIDFromCall.FindStringSubmatch(raw); len(match) == 2 {
		return match[1]
	}
	return ""
}

func parseXKCourses(raw string, mod xkModule) ([]payload, error) {
	var parsed struct {
		AaData []map[string]any `json:"aaData"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, &jwxtError{message: "选课查询未返回课程 JSON", hint: "确认已进入开放轮次；网页前端隐藏模块时仍可查询"}
	}
	items := make([]payload, 0, len(parsed.AaData))
	for _, row := range parsed.AaData {
		item := payload{
			"module":      mod.Key,
			"module_name": mod.Name,
			"course_code": xkString(row["kch"]),
			"course_name": xkString(row["kcmc"]),
			"teacher":     xkString(row["skls"]),
			"remaining":   xkString(row["syrs"]),
			"selected":    row["xkrs"],
			"capacity":    row["pkrs"],
			"schedule":    xkString(row["sksj"]),
			"location":    xkString(row["skdd"]),
			"college":     xkString(row["dwmc"]),
			"class_name":  xkString(row["ktmc"]),
			"conflict":    xkString(row["ctsm"]),
		}
		items = append(items, item)
	}
	return items, nil
}

func xkString(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return ""
	}
	return strings.TrimSpace(stripTags(text))
}

func xkSessionError(status int, raw string, allowRedirect bool) error {
	if containsAny(raw, []string{"您的账号在其它地方登录"}) {
		return &jwxtError{message: "教务账号已在其它地方登录", hint: "停止自动重试；确认后再运行 easy-qfnu jwxt login"}
	}
	ok := status == http.StatusOK || (allowRedirect && (status == http.StatusFound || status == http.StatusMovedPermanently))
	if !ok {
		return &jwxtError{message: fmt.Sprintf("选课页面返回 HTTP %d", status), hint: "请检查网络后重试"}
	}
	if containsAny(raw, []string{"请输入账号", "请输入密码", "请输入验证码"}) {
		return &jwxtError{message: "选课查询需要已登录的教务会话", hint: "先运行 easy-qfnu jwxt login 或 jwxt status"}
	}
	return nil
}
