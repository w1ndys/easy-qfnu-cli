package qfnu

import (
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
	liRE             = regexp.MustCompile(`(?is)<li\b[^>]*>(.*?)</li\s*>`)
	h2RE             = regexp.MustCompile(`(?is)<h2\b[^>]*>(.*?)</h2\s*>`)
	aRE              = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a\s*>`)
	pRE              = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p\s*>`)
	titleRE          = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)
	infoRE           = regexp.MustCompile(`(?i)(?:/|^)info/(\d+)/(\d+)(?:\.htm)?`)
	dateRE           = regexp.MustCompile(`\b(20\d{2}[-/.]\d{1,2}[-/.]\d{1,2})\b`)
	pageRE           = regexp.MustCompile(`(?i)(?:第\s*\d+\s*/\s*|页次\s*[:：]\s*\d+\s*/\s*)(\d+)`)
	articleHeaderRE  = regexp.MustCompile(`(?is)<form\b[^>]*name=["']_newscontent_fromname["'][^>]*>.*?<h2\b[^>]*>(.*?)</h2>`)
	articleDateRE    = regexp.MustCompile(`发布时间\s*[:：]?\s*(20\d{2}[-/.]\d{1,2}[-/.]\d{1,2})`)
	articleContentRE = regexp.MustCompile(`(?is)<div\b[^>]*id=["']vsb_content["'][^>]*>(.*?)</div>`)
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

type jwcListOptions struct {
	channel string
	page    int
	limit   int
}

func listJWC(args []string) (payload, error) {
	options, err := parseJWCListOptions(args)
	if err != nil {
		return nil, err
	}
	item, err := resolveChannel(options.channel)
	if err != nil {
		return nil, err
	}
	raw, finalURL, err := listJWCPage(item, options.page)
	if err != nil {
		return nil, &jwcError{message: "failed to fetch JWC list: " + err.Error(), hint: "请检查网络后重试"}
	}
	rows := parseListItems(raw, finalURL)
	if options.limit > 0 && len(rows) > options.limit {
		rows = rows[:options.limit]
	}
	return success("jwc", payload{"channel": item.Key, "title": item.Title, "kind": item.Kind, "page": options.page, "limit": options.limit, "total": nil, "total_pages": options.page, "count": len(rows), "items": rows}), nil
}

func parseJWCListOptions(args []string) (jwcListOptions, error) {
	options := jwcListOptions{channel: "notices", page: 1, limit: 10}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if index+1 >= len(args) {
			return jwcListOptions{}, fmt.Errorf("%s requires a value", arg)
		}
		value := args[index+1]
		switch arg {
		case "--channel", "-c":
			options.channel = value
		case "--page":
			parsed, err := parseJWCInteger(arg, value)
			if err != nil {
				return jwcListOptions{}, err
			}
			options.page = parsed
		case "--limit":
			parsed, err := parseJWCInteger(arg, value)
			if err != nil {
				return jwcListOptions{}, err
			}
			options.limit = parsed
		default:
			return jwcListOptions{}, fmt.Errorf("unknown option: %s", arg)
		}
		index++
	}
	return options, nil
}

func parseJWCInteger(option, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", option)
	}
	return parsed, nil
}

func channelsJWC() payload {
	return success("jwc", payload{"channels": channels})
}

type jwcSearchOptions struct {
	keyword string
	page    int
	limit   int
}

func searchJWC(args []string) (payload, error) {
	options, err := parseJWCSearchOptions(args)
	if err != nil {
		return nil, err
	}
	raw, finalURL, err := searchJWCPage(options.keyword, options.page)
	if err != nil {
		return nil, &jwcError{message: "failed to search JWC: " + err.Error(), hint: "请检查网络后重试"}
	}
	rows := parseListItems(raw, finalURL)
	if options.limit > 0 && len(rows) > options.limit {
		rows = rows[:options.limit]
	}
	totalPages, err := parseJWCPageCount(raw)
	if err != nil {
		return nil, err
	}
	return success("jwc", payload{"query": strings.TrimSpace(options.keyword), "page": options.page, "limit": options.limit, "total": nil, "total_pages": totalPages, "count": len(rows), "items": rows, "url": finalURL}), nil
}

func parseJWCSearchOptions(args []string) (jwcSearchOptions, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return jwcSearchOptions{}, &jwcError{message: "search keyword is empty"}
	}
	options := jwcSearchOptions{keyword: args[0], page: 1, limit: 10}
	for index := 1; index < len(args); index++ {
		arg := args[index]
		if index+1 >= len(args) {
			return jwcSearchOptions{}, fmt.Errorf("%s requires a value", arg)
		}
		parsed, err := parseJWCInteger(arg, args[index+1])
		if err != nil && (arg == "--page" || arg == "--limit") {
			return jwcSearchOptions{}, err
		}
		switch arg {
		case "--page":
			options.page = parsed
		case "--limit":
			options.limit = parsed
		default:
			return jwcSearchOptions{}, fmt.Errorf("unknown option: %s", arg)
		}
		index++
	}
	return options, nil
}

func parseJWCPageCount(raw string) (int, error) {
	match := pageRE.FindStringSubmatch(cleanHTML(raw))
	if len(match) < 2 {
		return 1, nil
	}
	parsed, err := strconv.Atoi(match[1])
	if err != nil || parsed < 1 {
		return 0, &jwcError{message: "invalid JWC pagination metadata", hint: "请稍后重试"}
	}
	return parsed, nil
}

func articleJWC(target string) (payload, error) {
	resolved, err := resolveJWCArticleURL(target)
	if err != nil {
		return nil, err
	}
	raw, finalURL, err := requestJWC(http.MethodGet, resolved, nil, nil)
	if err != nil {
		return nil, &jwcError{message: "failed to fetch article: " + err.Error(), hint: "请检查文章地址或网络连接"}
	}
	if strings.Contains(raw, "系统提示") && !strings.Contains(raw, "vsb_content") {
		return nil, &jwcError{message: "article is not publicly readable: " + resolved, hint: "该文章仍是 content.jsp 草稿，正文需要登录后才能查看"}
	}
	return parseJWCArticle(raw, finalURL)
}

func resolveJWCArticleURL(target string) (string, error) {
	target = strings.TrimSpace(target)
	resolved := target
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		if !strings.Contains(target, "content.jsp") {
			if !strings.HasPrefix(target, "/") {
				target = "/" + target
			}
			if !strings.HasSuffix(target, ".htm") {
				target += ".htm"
			}
		}
		resolved = resolveURL(jwcBase+"/", target)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host != "jwc.qfnu.edu.cn" {
		return "", &jwcError{message: "refusing non-JWC URL: " + resolved}
	}
	return resolved, nil
}

func parseJWCArticle(raw, finalURL string) (payload, error) {
	title := parseJWCArticleTitle(raw)
	if title == "" {
		return nil, &jwcError{message: "could not parse article at " + finalURL}
	}
	date := ""
	if match := articleDateRE.FindStringSubmatch(cleanHTML(raw)); len(match) > 1 {
		date = strings.ReplaceAll(strings.ReplaceAll(match[1], "/", "-"), ".", "-")
	}
	content := ""
	if match := articleContentRE.FindStringSubmatch(raw); len(match) > 1 {
		content = cleanHTML(match[1])
	}
	if content == "" {
		content = cleanHTML(raw)
	}
	id, category := "", ""
	if match := infoRE.FindStringSubmatch(finalURL); len(match) > 2 {
		category, id = match[1], match[2]
	}
	return success("jwc", payload{"id": id, "category_id": category, "title": title, "date": date, "editor": "", "section": "", "breadcrumb": []string{}, "url": finalURL, "content_text": content, "attachments": []any{}}), nil
}

func parseJWCArticleTitle(raw string) string {
	title := ""
	if match := articleHeaderRE.FindStringSubmatch(raw); len(match) > 1 {
		title = cleanHTML(match[1])
	}
	if title == "" {
		if match := titleRE.FindStringSubmatch(raw); len(match) > 1 {
			title = cleanHTML(match[1])
		}
	}
	return title
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
	if _, err := fmt.Fprintln(w, "Usage: easy-qfnu jwc <list|get|search|channels> [options]"); err != nil {
		return 1
	}
	return 2
}
