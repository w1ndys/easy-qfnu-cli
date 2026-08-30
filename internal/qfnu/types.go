package qfnu

import "encoding/json"

type payload map[string]any

type channel struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Slug  string `json:"slug"`
}

type listItem struct {
	ID          string `json:"id"`
	CategoryID  string `json:"category_id"`
	Title       string `json:"title"`
	Date        string `json:"date"`
	URL         string `json:"url"`
	Summary     string `json:"summary"`
	Unpublished bool   `json:"unpublished"`
}

var channels = []channel{
	{Key: "notices", Title: "Important notices (重要通知)", Kind: "aggregate", Slug: "tz_j_"},
	{Key: "announcements", Title: "Department announcements (部门公告)", Kind: "aggregate", Slug: "gg_j_"},
	{Key: "news", Title: "News (新闻)", Kind: "aggregate", Slug: "xw_j_"},
	{Key: "jxyj-notices", Title: "Teaching-research notices (教学研究通知)", Kind: "category", Slug: "jxyj/jxyjtz"},
	{Key: "jxyj-announcements", Title: "Teaching-research announcements (教学研究公告)", Kind: "category", Slug: "jxyj/jxyjgg"},
	{Key: "jxyj-news", Title: "Teaching-research news (教学研究新闻)", Kind: "category", Slug: "jxyj/jxyjxw"},
	{Key: "jwyx-notices", Title: "Academic-operations notices (教务运行通知)", Kind: "category", Slug: "jwyx/jwyxtz"},
	{Key: "jwyx-announcements", Title: "Academic-operations announcements (教务运行公告)", Kind: "category", Slug: "jwyx/jwyxgg"},
	{Key: "jwyx-news", Title: "Academic-operations news (教务运行新闻)", Kind: "category", Slug: "jwyx/jwyxxw"},
	{Key: "xjgl-notices", Title: "Student-status notices (学籍管理通知)", Kind: "category", Slug: "xjgl/xjgltz"},
	{Key: "xjgl-announcements", Title: "Student-status announcements (学籍管理公告)", Kind: "category", Slug: "xjgl/xjglgg"},
	{Key: "xjgl-news", Title: "Student-status news (学籍管理新闻)", Kind: "category", Slug: "xjgl/xjglxw"},
	{Key: "sjjx-notices", Title: "Practical-teaching notices (实践教学通知)", Kind: "category", Slug: "sjjx/sjjxtz"},
	{Key: "sjjx-announcements", Title: "Practical-teaching announcements (实践教学公告)", Kind: "category", Slug: "sjjx/sjjxgg"},
	{Key: "sjjx-news", Title: "Practical-teaching news (实践教学新闻)", Kind: "category", Slug: "sjjx/sjjxxw"},
	{Key: "jsfz-notices", Title: "Faculty-development notices (教师发展通知)", Kind: "category", Slug: "jsfz/jsfztz"},
	{Key: "jsfz-announcements", Title: "Faculty-development announcements (教师发展公告)", Kind: "category", Slug: "jsfz/jsfzgg"},
	{Key: "jsfz-news", Title: "Faculty-development news (教师发展新闻)", Kind: "category", Slug: "jsfz/jsfzxw"},
	{Key: "kcsz-notices", Title: "Curriculum ideology notices (课程思政通知)", Kind: "category", Slug: "kcsz/kcsztz"},
	{Key: "kcsz-announcements", Title: "Curriculum ideology announcements (课程思政公告)", Kind: "category", Slug: "kcsz/kcszgg"},
	{Key: "kcsz-news", Title: "Curriculum ideology news (课程思政新闻)", Kind: "category", Slug: "kcsz/kcszxw"},
}

func writeJSON(out interface{ Write([]byte) (int, error) }, value any) int {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return 1
	}
	data = append(data, '\n')
	if _, err = out.Write(data); err != nil {
		return 1
	}
	return 0
}

func success(source string, fields payload) payload {
	result := payload{"ok": true, "source": source}
	for key, value := range fields {
		result[key] = value
	}
	return result
}

func failure(source, message, hint string) payload {
	result := payload{"ok": false, "source": source, "error": message}
	if hint != "" {
		result["hint"] = hint
	}
	return result
}
