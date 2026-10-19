package antirobot

import "testing"

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"scheme 与 host 小写", "HTTPS://Example.COM/Path", "https://example.com/Path"},
		{"去 fragment", "https://example.com/a#section", "https://example.com/a"},
		{"去尾斜杠", "https://example.com/a/", "https://example.com/a"},
		{"根路径保留", "https://example.com/", "https://example.com/"},
		{"剥 utm_*", "https://example.com/a?utm_source=x&utm_medium=y", "https://example.com/a"},
		{"剥 fbclid/gclid", "https://example.com/a?fbclid=1&gclid=2", "https://example.com/a"},
		{"剥 ref 与 spm", "https://example.com/a?ref=nav&spm=1.2", "https://example.com/a"},
		{"保留业务参数并排序", "https://example.com/a?b=2&a=1", "https://example.com/a?a=1&b=2"},
		{"混合剥离与保留", "https://example.com/a?id=7&utm_campaign=c", "https://example.com/a?id=7"},
		{"中文路径保留转义", "https://example.com/搜索?q=1", "https://example.com/%E6%90%9C%E7%B4%A2?q=1"},
		{"首尾空白", "  https://example.com/a  ", "https://example.com/a"},
		{"非法 URL 回退原文", "://bad url", "://bad url"},
		{"无 host 回退原文", "mailto:a@b.com", "mailto:a@b.com"},
		{"空串", "", ""},
		{"大小写参数名统一剥离", "https://example.com/a?UTM_Source=x", "https://example.com/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeURL(tc.in); got != tc.want {
				t.Errorf("NormalizeURL(%q) = %q, 期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormalizeURLVariantsAgree 同一页面的常见变体必须收敛到同一键。
func TestNormalizeURLVariantsAgree(t *testing.T) {
	want := NormalizeURL("https://example.com/article")
	variants := []string{
		"https://EXAMPLE.com/article/",
		"https://example.com/article?utm_source=bing&utm_campaign=x",
		"https://example.com/article#comments",
		"https://example.com/article?fbclid=abc",
		"  https://example.com/article  ",
	}
	for _, v := range variants {
		if got := NormalizeURL(v); got != want {
			t.Errorf("变体 %q 归一化为 %q, 期望 %q", v, got, want)
		}
	}
}
