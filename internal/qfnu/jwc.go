package qfnu

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const jwcBase = "https://jwc.qfnu.edu.cn"

var (
	liRE    = regexp.MustCompile(`(?is)<li\b[^>]*>(.*?)</li\s*>`)
	h2RE    = regexp.MustCompile(`(?is)<h2\b[^>]*>(.*?)</h2\s*>`)
	aRE     = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a\s*>`)
	pRE     = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p\s*>`)
	titleRE = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)
	infoRE  = regexp.MustCompile(`(?i)(?:/|^)info/(\d+)/(\d+)(?:\.htm)?`)
	dateRE  = regexp.MustCompile(`\b(20\d{2}[-/.]\d{1,2}[-/.]\d{1,2})\b`)
	pageRE  = regexp.MustCompile(`(?i)(?:第\s*\d+\s*/\s*|页次\s*[:：]\s*\d+\s*/\s*)(\d+)`)
)

type jwcError struct{ message, hint string }

func (e *jwcError) Error() string { return e.message }

func cleanHTML(fragment string) string {
	fragment = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`).ReplaceAllString(fragment, " ")
	fragment = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`).ReplaceAllString(fragment, " ")
	fragment = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(fragment, "\n")
	fragment = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(fragment, " ")
	fragment = html.UnescapeString(fragment)
	lines := strings.Fields(strings.ReplaceAll(fragment, "\u00a0", " "))
	return strings.TrimSpace(strings.Join(lines, " "))
}

func attr(tag, name string) string {
	quoted := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]*)"|\b` + regexp.QuoteMeta(name) + `\s*=\s*'([^']*)'`)
	if m := quoted.FindStringSubmatch(tag); len(m) > 2 {
		if m[1] != "" {
			return html.UnescapeString(m[1])
		}
		return html.UnescapeString(m[2])
	}
	unquoted := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*=\s*([^\s>]+)`)
	if m := unquoted.FindStringSubmatch(tag); len(m) > 1 {
		return html.UnescapeString(strings.Trim(m[1], "\"'"))
	}
	return ""
}

func requestJWC(method, target string, body io.Reader, headers map[string]string) (string, string, error) {
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "easy-qfnu-skill/easy-qfnu")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.Request.URL.String(), err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.Request.URL.String(), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(b), resp.Request.URL.String(), nil
}

func resolveChannel(name string) (channel, error) {
	name = strings.TrimSpace(name)
	for _, item := range channels {
		if item.Key == name || item.Title == name || strings.Contains(item.Title, name) {
			return item, nil
		}
	}
	return channel{}, &jwcError{message: "unknown JWC channel: " + name, hint: "run easy-qfnu jwc channels to list supported channels"}
}

func parseListItems(raw, pageURL string) []listItem {
	var items []listItem
	for _, li := range liRE.FindAllStringSubmatch(raw, -1) {
		fragment := li[1]
		h2 := h2RE.FindStringSubmatch(fragment)
		if len(h2) == 0 {
			continue
		}
		links := aRE.FindAllStringSubmatch(h2[1], -1)
		if len(links) == 0 {
			continue
		}
		link := links[0]
		href := attr(link[1], "href")
		resolved := resolveURL(pageURL, href)
		title := strings.TrimSpace(attr(link[1], "title"))
		if title == "" {
			title = cleanHTML(link[2])
		}
		date := ""
		if m := dateRE.FindStringSubmatch(cleanHTML(fragment)); len(m) > 1 {
			date = strings.ReplaceAll(m[1], "/", "-")
			date = strings.ReplaceAll(date, ".", "-")
		}
		summary := ""
		if p := pRE.FindStringSubmatch(fragment); len(p) > 1 {
			summary = cleanHTML(p[1])
		}
		id, category := "", ""
		if m := infoRE.FindStringSubmatch(resolved); len(m) > 2 {
			category, id = m[1], m[2]
		}
		unpublished := strings.Contains(strings.ToLower(resolved), "content.jsp")
		items = append(items, listItem{ID: id, CategoryID: category, Title: title, Date: date, URL: resolved, Summary: summary, Unpublished: unpublished})
	}
	return items
}

func resolveURL(base, href string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return href
	}
	return baseURL.ResolveReference(parsed).String()
}

func listPage(item channel, page int) (string, string, error) {
	if page < 1 {
		return "", "", &jwcError{message: "page must be at least 1"}
	}
	path := "/" + item.Slug + ".htm"
	if page > 1 {
		path = "/" + item.Slug + "/" + strconv.Itoa(page) + ".htm"
	}
	return requestJWC(http.MethodGet, jwcBase+path, nil, nil)
}

func listJWC(args []string) (payload, error) {
	channelName, page, limit := "notices", 1, 10
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--channel", "-c":
			if i+1 >= len(args) {
				return nil, errors.New("--channel requires a value")
			}
			channelName = args[i+1]
			i++
		case "--page":
			if i+1 >= len(args) {
				return nil, errors.New("--page requires a value")
			}
			page, _ = strconv.Atoi(args[i+1])
			i++
		case "--limit":
			if i+1 >= len(args) {
				return nil, errors.New("--limit requires a value")
			}
			limit, _ = strconv.Atoi(args[i+1])
			i++
		default:
			return nil, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	item, err := resolveChannel(channelName)
	if err != nil {
		return nil, err
	}
	raw, finalURL, err := listPage(item, page)
	if err != nil {
		return nil, &jwcError{message: "failed to fetch JWC list: " + err.Error(), hint: "请检查网络后重试"}
	}
	rows := parseListItems(raw, finalURL)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return success("jwc", payload{"channel": item.Key, "title": item.Title, "kind": item.Kind, "page": page, "limit": limit, "total": nil, "total_pages": page, "count": len(rows), "items": rows}), nil
}

func channelsJWC() payload {
	return success("jwc", payload{"channels": channels})
}

func searchJWC(args []string) (payload, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return nil, &jwcError{message: "search keyword is empty"}
	}
	keyword, page, limit := args[0], 1, 10
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--page":
			if i+1 >= len(args) {
				return nil, errors.New("--page requires a value")
			}
			page, _ = strconv.Atoi(args[i+1])
			i++
		case "--limit":
			if i+1 >= len(args) {
				return nil, errors.New("--limit requires a value")
			}
			limit, _ = strconv.Atoi(args[i+1])
			i++
		default:
			return nil, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(keyword)))
	var raw, finalURL string
	var err error
	if page <= 1 {
		form := url.Values{"lucenenewssearchkey": {encoded}, "_lucenesearchtype": {"1"}, "searchScope": {"1"}}
		raw, finalURL, err = requestJWC(http.MethodPost, jwcBase+"/ssjg.jsp?wbtreeid=1001", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Referer": jwcBase + "/"})
	} else {
		target := jwcBase + "/ssjg.jsp?wbtreeid=1001&searchScope=1&currentnum=" + strconv.Itoa(page) + "&newskeycode2=" + url.QueryEscape(encoded)
		raw, finalURL, err = requestJWC(http.MethodGet, target, nil, map[string]string{"Referer": jwcBase + "/"})
	}
	if err != nil {
		return nil, &jwcError{message: "failed to search JWC: " + err.Error(), hint: "请检查网络后重试"}
	}
	rows := parseListItems(raw, finalURL)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	totalPages := 1
	if m := pageRE.FindStringSubmatch(cleanHTML(raw)); len(m) > 1 {
		totalPages, _ = strconv.Atoi(m[1])
	}
	return success("jwc", payload{"query": strings.TrimSpace(keyword), "page": page, "limit": limit, "total": nil, "total_pages": totalPages, "count": len(rows), "items": rows, "url": finalURL}), nil
}

func articleJWC(target string) (payload, error) {
	target = strings.TrimSpace(target)
	var resolved string
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		resolved = target
	} else if strings.Contains(target, "content.jsp") {
		resolved = resolveURL(jwcBase+"/", target)
	} else {
		if !strings.HasPrefix(target, "/") {
			target = "/" + target
		}
		if !strings.HasSuffix(target, ".htm") {
			target += ".htm"
		}
		resolved = resolveURL(jwcBase+"/", target)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host != "jwc.qfnu.edu.cn" {
		return nil, &jwcError{message: "refusing non-JWC URL: " + resolved}
	}
	raw, finalURL, err := requestJWC(http.MethodGet, resolved, nil, nil)
	if err != nil {
		return nil, &jwcError{message: "failed to fetch article: " + err.Error(), hint: "请检查文章地址或网络连接"}
	}
	if strings.Contains(raw, "系统提示") && !strings.Contains(raw, "vsb_content") {
		return nil, &jwcError{message: "article is not publicly readable: " + resolved, hint: "该文章仍是 content.jsp 草稿，正文需要登录后才能查看"}
	}
	title := ""
	if m := regexp.MustCompile(`(?is)<form\b[^>]*name=["']_newscontent_fromname["'][^>]*>.*?<h2\b[^>]*>(.*?)</h2>`).FindStringSubmatch(raw); len(m) > 1 {
		title = cleanHTML(m[1])
	}
	if title == "" && len(titleRE.FindStringSubmatch(raw)) > 1 {
		title = cleanHTML(titleRE.FindStringSubmatch(raw)[1])
	}
	date := ""
	if m := regexp.MustCompile(`发布时间\s*[:：]?\s*(20\d{2}[-/.]\d{1,2}[-/.]\d{1,2})`).FindStringSubmatch(cleanHTML(raw)); len(m) > 1 {
		date = strings.ReplaceAll(strings.ReplaceAll(m[1], "/", "-"), ".", "-")
	}
	content := ""
	if m := regexp.MustCompile(`(?is)<div\b[^>]*id=["']vsb_content["'][^>]*>(.*?)</div>`).FindStringSubmatch(raw); len(m) > 1 {
		content = cleanHTML(m[1])
	}
	if content == "" {
		content = cleanHTML(raw)
	}
	if title == "" {
		return nil, &jwcError{message: "could not parse article at " + finalURL}
	}
	id, category := "", ""
	if m := infoRE.FindStringSubmatch(finalURL); len(m) > 2 {
		category, id = m[1], m[2]
	}
	return success("jwc", payload{"id": id, "category_id": category, "title": title, "date": date, "editor": "", "section": "", "breadcrumb": []string{}, "url": finalURL, "content_text": content, "attachments": []any{}}), nil
}

func runJWC(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" {
		return usageJWC(out)
	}
	var result payload
	var err error
	switch args[0] {
	case "channels":
		result = channelsJWC()
	case "list":
		result, err = listJWC(args[1:])
	case "search":
		result, err = searchJWC(args[1:])
	case "get":
		if len(args) < 2 {
			err = errors.New("get requires a URL or info path")
		} else {
			result, err = articleJWC(args[1])
		}
	default:
		err = fmt.Errorf("unknown action: %s", args[0])
	}
	if err != nil {
		if e, ok := err.(*jwcError); ok {
			result = failure("jwc", e.message, e.hint)
		} else {
			result = failure("jwc", err.Error(), "")
		}
		return writeJSON(out, result)
	}
	return writeJSON(out, result)
}

func usageJWC(w io.Writer) int {
	fmt.Fprintln(w, "Usage: easy-qfnu jwc <list|get|search|channels> [options]")
	return 2
}
