package qfnu

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseXKRoundsReadsIDsAndTableFields(t *testing.T) {
	raw := `
	<table>
	<tr><th>选课轮次名称</th><th>开始时间</th><th>结束时间</th><th>操作</th></tr>
	<tr>
	  <td>2025-2026-3公选课</td>
	  <td>2026-03-01 08:00</td>
	  <td>2026-03-07 18:00</td>
	  <td><a href="#" onclick="xsxkFun('ABC123')">进入选课</a></td>
	</tr>
	</table>
	<a id="jrxk" href="/jsxsd/xsxk/xsxk_index?jx0502zbid=ABC123">进入选课</a>
	`
	rounds := parseXKRounds(raw)
	if len(rounds) != 1 || rounds[0].ID != "ABC123" {
		t.Fatalf("rounds = %#v", rounds)
	}
	if rounds[0].Name != "2025-2026-3公选课" || rounds[0].Start != "2026-03-01 08:00" || rounds[0].End != "2026-03-07 18:00" {
		t.Fatalf("round fields = %#v", rounds[0])
	}
}

func TestParseXKCoursesStripsHTMLAndKeepsModule(t *testing.T) {
	raw := `{"aaData":[{"kch":"1001","kcmc":"<span>音乐鉴赏</span>","skls":"王老师","syrs":"<font>3</font>","xkrs":10,"pkrs":40,"sksj":"周一 1-2","skdd":"日照1教","dwmc":"音乐学院","ktmc":"01班","ctsm":null}]}`
	mod := xkModules[4]
	items, err := parseXKCourses(raw, mod)
	if err != nil || len(items) != 1 {
		t.Fatalf("parseXKCourses() err=%v items=%v", err, items)
	}
	if items[0]["course_name"] != "音乐鉴赏" || items[0]["remaining"] != "3" {
		t.Fatalf("item = %#v", items[0])
	}
	if items[0]["module"] != "ggxxkxk" || items[0]["module_name"] != "公选课选课" {
		t.Fatalf("module = %#v", items[0])
	}
	if _, ok := items[0]["jx0404id"]; ok {
		t.Fatal("teaching-class id must not appear in query results")
	}
}

func TestSummarizeXKModulesReportsWhereCourseLives(t *testing.T) {
	items := []payload{
		{"module": "ggxxkxk", "module_name": "公选课选课"},
		{"module": "ggxxkxk", "module_name": "公选课选课"},
		{"module": "xxxk", "module_name": "选修选课"},
	}
	located := summarizeXKModules(items)
	if len(located) != 2 {
		t.Fatalf("located = %#v", located)
	}
	if located[0]["key"] != "ggxxkxk" || located[0]["count"] != 2 {
		t.Fatalf("first = %#v", located[0])
	}
	if located[1]["key"] != "xxxk" || located[1]["count"] != 1 {
		t.Fatalf("second = %#v", located[1])
	}
}

func TestResolveXKModulesAcceptsChineseAliases(t *testing.T) {
	mods, err := resolveXKModules([]string{"公选课", "选修"})
	if err != nil || len(mods) != 2 || mods[0].Key != "ggxxkxk" || mods[1].Key != "xxxk" {
		t.Fatalf("mods=%v err=%v", mods, err)
	}
}

func TestPickXKRoundRequiresIDWhenMultiple(t *testing.T) {
	rounds := []xkRound{{ID: "A"}, {ID: "B"}}
	if _, err := pickXKRound(rounds, ""); err == nil {
		t.Fatal("expected error for multiple rounds")
	}
	got, err := pickXKRound(rounds, "B")
	if err != nil || got.ID != "B" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestXKSearchLocatesCourseModule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/jsxsd/xsxk/xklc_list":
			io.WriteString(w, `<a onclick="xsxkFun('R1')">进入</a>`)
		case r.URL.Path == "/jsxsd/xsxk/xsxk_index":
			w.WriteHeader(http.StatusOK)
		case strings.Contains(r.URL.Path, "xsxkGgxxkxk"):
			io.WriteString(w, `{"aaData":[{"kch":"1001","kcmc":"音乐鉴赏","skls":"王","syrs":"2"}]}`)
		case strings.HasPrefix(r.URL.Path, "/jsxsd/xsxkkc/"):
			io.WriteString(w, `{"aaData":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	oldList, oldEnter, oldSearch := xkListURL, xkEnterURL, xkSearchRoot
	xkListURL = server.URL + "/jsxsd/xsxk/xklc_list"
	xkEnterURL = server.URL + "/jsxsd/xsxk/xsxk_index"
	xkSearchRoot = server.URL + "/jsxsd/xsxkkc"
	t.Cleanup(func() {
		xkListURL, xkEnterURL, xkSearchRoot = oldList, oldEnter, oldSearch
	})

	client, err := newJWXTClient("", "")
	if err != nil {
		t.Fatal(err)
	}
	query, err := parseXKCommand("search", []string{"--course", "音乐鉴赏", "--limit", "20"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.xkSearch(query)
	if err != nil {
		t.Fatal(err)
	}
	if result["query_kind"] != "live" {
		t.Fatalf("query_kind = %v", result["query_kind"])
	}
	located, _ := result["located_modules"].([]payload)
	if len(located) != 1 || located[0]["key"] != "ggxxkxk" {
		t.Fatalf("located_modules = %#v", result["located_modules"])
	}
	items, _ := result["items"].([]payload)
	if len(items) != 1 || items[0]["course_name"] != "音乐鉴赏" {
		t.Fatalf("items = %#v", result["items"])
	}
}

func TestXKSearchJSONOmitsSelectionIDs(t *testing.T) {
	raw := `{"aaData":[{"kch":"1","kcmc":"课","jx0404id":"secret","jx02id":"also"}]}`
	items, err := parseXKCourses(raw, xkModules[0])
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(items[0])
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "jx0404") {
		t.Fatalf("leaked selection id: %s", encoded)
	}
}
