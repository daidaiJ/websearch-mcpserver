package everything

import "testing"

func TestAppendExcludes(t *testing.T) {
	got := AppendExcludes(`d:\code\ai\ *.go`, []string{`\obj\`, `\node_modules\`, `old backup`, "", "  "})
	want := `d:\code\ai\ *.go !\obj\ !\node_modules\ !"old backup"`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if AppendExcludes(`*.go`, nil) != `*.go` {
		t.Error("无排除项应原样返回")
	}
}
