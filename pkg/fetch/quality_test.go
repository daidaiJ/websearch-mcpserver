package fetch

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAssessContent(t *testing.T) {
	longArticle := strings.Repeat("This is a substantive article paragraph with enough prose to read. ", 30)
	linkListWithProse := "# Link directory\n\n" + strings.Repeat("A descriptive sentence explains the context of these links. ", 25) +
		"\n\n- [One](https://example.com/1)\n- [Two](https://example.com/2)\n- [Three](https://example.com/3)\n"
	articleAboutCaptcha := "# Understanding CAPTCHA\n\n" + strings.Repeat("CAPTCHA challenges are discussed in detail in this article. ", 80)

	tests := []struct {
		name       string
		markdown   string
		wantKind   QualityKind
		wantNav    bool
		wantBot    bool
		minLinks   int
		maxVisible int
	}{
		{
			name:     "empty",
			markdown: "",
		},
		{
			name:     "short valid document",
			markdown: "# Status\n\nOpen",
		},
		{
			name:     "short document with one link",
			markdown: "# Status\n\nOpen [status page](https://example.com/status)",
		},
		{
			name:     "navigation shell",
			markdown: "# App\n\n[Home](/) [Docs](/docs) [Pricing](/pricing) [Login](/login)",
			wantKind: QualityNavOnly,
			wantNav:  true,
			minLinks: 4,
		},
		{
			name:     "cloudflare challenge",
			markdown: "# Just a moment...\n\nChecking your browser before accessing example.com. Enable JavaScript and cookies to continue.",
			wantKind: QualityBotCheck,
			wantBot:  true,
		},
		{
			name:     "chinese challenge",
			markdown: "# 安全验证\n\n请完成人机验证后继续访问。",
			wantKind: QualityBotCheck,
			wantBot:  true,
		},
		{
			name:     "normal article",
			markdown: "# Guide\n\n" + longArticle + "\n\n[Reference](https://example.com/ref)",
		},
		{
			name:     "link list with prose",
			markdown: linkListWithProse,
		},
		{
			name:     "long article discussing captcha",
			markdown: articleAboutCaptcha,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AssessContent(tt.markdown)
			if got.Kind() != tt.wantKind || got.LowQuality() != (tt.wantKind != "") {
				t.Fatalf("kind = %q low = %v, want %q (%+v)", got.Kind(), got.LowQuality(), tt.wantKind, got)
			}
			if got.NavOnly != tt.wantNav || got.BotCheck != tt.wantBot {
				t.Fatalf("nav/bot = %v/%v, want %v/%v (%+v)", got.NavOnly, got.BotCheck, tt.wantNav, tt.wantBot, got)
			}
			if tt.minLinks > 0 && got.LinkCount < tt.minLinks {
				t.Fatalf("link count = %d, want >= %d", got.LinkCount, tt.minLinks)
			}
		})
	}
}

func TestAssessContentIgnoresImageAltAndURLs(t *testing.T) {
	got := AssessContent("# Page\n\n![logo](https://example.com/logo.png) [Read more](https://example.com/read-more?utm_source=x)")
	if got.LinkCount != 1 {
		t.Fatalf("link count = %d, want 1", got.LinkCount)
	}
	if got.VisibleChars == 0 {
		t.Fatal("visible text should include link label")
	}
}

func TestIsLowQuality(t *testing.T) {
	low := &LowQualityError{Kind: QualityNavOnly, URL: "https://example.com", VisibleChars: 100}
	if !IsLowQuality(fmt.Errorf("outer: %w", low)) {
		t.Fatal("wrapped LowQualityError not detected")
	}
	if IsLowQuality(errors.New("plain")) {
		t.Fatal("plain error must not be classified as low quality")
	}
}

func TestQualityKindMessageKey(t *testing.T) {
	if QualityBotCheck.MessageKey() != "fetch.quality.bot_check" {
		t.Fatalf("bot key = %q", QualityBotCheck.MessageKey())
	}
	if QualityNavOnly.MessageKey() != "fetch.quality.nav_only" {
		t.Fatalf("nav key = %q", QualityNavOnly.MessageKey())
	}
	if QualityKind("other").MessageKey() != "" {
		t.Fatal("unknown kind should have no message key")
	}
}
