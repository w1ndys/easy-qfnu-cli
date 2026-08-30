package qfnu

import "testing"

func TestEncodeCredentialsMatchesQFNUProtocol(t *testing.T) {
	got := encodeCredentials("abc", "pw", "XYZ123", "10120")
	want := "aXbcY%Z1%%pw"
	if got != want {
		t.Fatalf("encodeCredentials() = %q, want %q", got, want)
	}
}

func TestEncodeCredentialsKeepsCharactersAfterProtocolLimit(t *testing.T) {
	username := "12345678901234567890"
	got := encodeCredentials(username, "pw", "", "")
	if got != username[:20]+"%%%pw" {
		t.Fatalf("unexpected encoded value: %q", got)
	}
}

func TestParseListItems(t *testing.T) {
	raw := `<ul class="n_listxx1"><li><h2><a href="info/1103/7719.htm" title="通知标题">通知标题</a><span class="time">2026-07-13</span></h2><p>摘要内容</p></li></ul>`
	items := parseListItems(raw, jwcBase+"/tz_j_.htm")
	if len(items) != 1 || items[0].ID != "7719" || items[0].CategoryID != "1103" {
		t.Fatalf("unexpected list item: %#v", items)
	}
}
