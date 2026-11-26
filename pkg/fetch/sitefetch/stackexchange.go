package sitefetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"websearch/pkg/i18n"
)

// StackExchange question pages are server-rendered but the generic HTML path
// loses the accepted-answer ordering and folds comments into the page chrome.
// The public API returns the body as Markdown plus the accepted flag, which is
// what an agent needs for a focused read.

var seAPIEndpoint = "https://api.stackexchange.com/2.3"

// seQuestionPathRe and seShortPathRe match /questions/{id}[/slug] and /q/{id}.
var (
	seQuestionPathRe = regexp.MustCompile(`^/questions/(\d+)(?:/|$)`)
	seShortPathRe    = regexp.MustCompile(`^/q/(\d+)(?:/|$)`)
)

// seFilter adds question.body_markdown and answer.body_markdown to the
// withbody base filter. Filter strings are deterministic and do not expire;
// regenerate with GET /2.3/filters/create if the API ever retires it.
const seFilter = "!X-cWn5YrCQCchzB5B4*yqi6eO0BYWbSmsTE.VZm"

const (
	seMaxAnswers  = 5
	seMaxComments = 30
)

// StackExchangeFetcher reads a question through the StackExchange API.
type StackExchangeFetcher struct {
	client *http.Client
}

func NewStackExchangeFetcher() *StackExchangeFetcher {
	return &StackExchangeFetcher{client: &http.Client{Timeout: 15 * time.Second}}
}

func (f *StackExchangeFetcher) Provider() string { return "stackexchange_api" }

func (f *StackExchangeFetcher) Match(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if _, ok := stackExchangeSite(u.Hostname()); !ok {
		return false
	}
	return seQuestionPathRe.MatchString(u.Path) || seShortPathRe.MatchString(u.Path)
}

type seOwner struct {
	DisplayName string `json:"display_name"`
}

type seComment struct {
	Score int     `json:"score"`
	Body  string  `json:"body"`
	Owner seOwner `json:"owner"`
}

type seAnswer struct {
	Score        int         `json:"score"`
	IsAccepted   bool        `json:"is_accepted"`
	BodyMarkdown string      `json:"body_markdown"`
	Body         string      `json:"body"`
	Owner        seOwner     `json:"owner"`
	Comments     []seComment `json:"comments"`
}

type seQuestion struct {
	QuestionID       int         `json:"question_id"`
	Title            string      `json:"title"`
	Link             string      `json:"link"`
	BodyMarkdown     string      `json:"body_markdown"`
	Body             string      `json:"body"`
	Score            int         `json:"score"`
	AnswerCount      int         `json:"answer_count"`
	AcceptedAnswerID int         `json:"accepted_answer_id"`
	Tags             []string    `json:"tags"`
	Answers          []seAnswer  `json:"answers"`
	Owner            seOwner     `json:"owner"`
	Comments         []seComment `json:"comments"`
}

type seResponse struct {
	Items []seQuestion `json:"items"`
}

func (f *StackExchangeFetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	site, ok := stackExchangeSite(u.Hostname())
	if !ok {
		return "", errors.New(i18n.T("fetch.sitefetch.stackexchange_unsupported"))
	}
	id, ok := stackExchangeQuestionID(u.Path)
	if !ok {
		return "", errors.New(i18n.T("fetch.sitefetch.stackexchange_unsupported"))
	}

	endpoint := fmt.Sprintf("%s/questions/%s?site=%s&filter=%s",
		strings.TrimRight(seAPIEndpoint, "/"), url.PathEscape(id), url.QueryEscape(site), url.QueryEscape(seFilter))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "websearch-mcpserver/1.0 (StackExchange fetch)")
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T("fetch.sitefetch.request_failed", "Provider", "StackExchange"), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", i18n.T("fetch.sitefetch.api_http", "Provider", "StackExchange", "Status", resp.StatusCode))
	}
	var out seResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if len(out.Items) == 0 {
		return "", errors.New(i18n.T("fetch.sitefetch.stackexchange_empty", "ID", id))
	}
	return renderStackExchangeQuestion(out.Items[0]), nil
}

// stackExchangeSite maps a host to the API site parameter. Meta sites need the
// meta.<base> form; naive suffix stripping would produce a bare "meta".
func stackExchangeSite(host string) (string, bool) {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	switch host {
	case "stackoverflow.com":
		return "stackoverflow", true
	case "serverfault.com":
		return "serverfault", true
	case "superuser.com":
		return "superuser", true
	case "askubuntu.com":
		return "askubuntu", true
	case "mathoverflow.net":
		return "mathoverflow.net", true
	}
	if base, ok := strings.CutPrefix(host, "meta."); ok {
		switch base {
		case "stackexchange.com":
			return "meta.stackexchange", true
		case "stackoverflow.com":
			return "meta.stackoverflow", true
		case "serverfault.com":
			return "meta.serverfault", true
		case "superuser.com":
			return "meta.superuser", true
		case "askubuntu.com":
			return "meta.askubuntu", true
		case "mathoverflow.net":
			return "meta.mathoverflow.net", true
		}
	}
	if strings.HasSuffix(host, ".stackexchange.com") {
		return strings.TrimSuffix(host, ".stackexchange.com"), true
	}
	return "", false
}

func stackExchangeQuestionID(path string) (string, bool) {
	if m := seQuestionPathRe.FindStringSubmatch(path); len(m) == 2 {
		return m[1], true
	}
	if m := seShortPathRe.FindStringSubmatch(path); len(m) == 2 {
		return m[1], true
	}
	return "", false
}

func renderStackExchangeQuestion(q seQuestion) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", strings.TrimSpace(q.Title))

	meta := i18n.T("fetch.sitefetch.se_question", "Score", q.Score, "Answers", q.AnswerCount)
	if len(q.Tags) > 0 {
		meta += i18n.T("fetch.sitefetch.se_tags", "Tags", strings.Join(q.Tags, ", "))
	}
	sb.WriteString(meta)
	sb.WriteString("\n\n")
	if q.Link != "" {
		fmt.Fprintf(&sb, "%s\n\n", q.Link)
	}
	body := strings.TrimSpace(q.BodyMarkdown)
	if body == "" {
		body = strings.TrimSpace(q.Body)
	}
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n\n")
	}

	sb.WriteString(i18n.T("fetch.sitefetch.se_answers_head"))
	sb.WriteString("\n\n")
	answers := append([]seAnswer(nil), q.Answers...)
	sort.SliceStable(answers, func(i, j int) bool {
		if answers[i].IsAccepted != answers[j].IsAccepted {
			return answers[i].IsAccepted
		}
		return answers[i].Score > answers[j].Score
	})
	if len(answers) == 0 {
		sb.WriteString(i18n.T("fetch.sitefetch.se_no_answers"))
		sb.WriteString("\n")
		return sb.String()
	}
	if len(answers) > seMaxAnswers {
		answers = answers[:seMaxAnswers]
	}
	for _, answer := range answers {
		author := strings.TrimSpace(answer.Owner.DisplayName)
		if author == "" {
			author = "-"
		}
		if answer.IsAccepted {
			fmt.Fprintf(&sb, "%s\n\n", i18n.T("fetch.sitefetch.se_accepted", "Score", answer.Score, "Author", author))
		} else {
			fmt.Fprintf(&sb, "%s\n\n", i18n.T("fetch.sitefetch.se_answer", "Score", answer.Score, "Author", author))
		}
		text := strings.TrimSpace(answer.BodyMarkdown)
		if text == "" {
			text = strings.TrimSpace(answer.Body)
		}
		sb.WriteString(text)
		sb.WriteString("\n\n")
		if len(answer.Comments) > 0 {
			limit := len(answer.Comments)
			if limit > seMaxComments {
				limit = seMaxComments
			}
			for _, comment := range answer.Comments[:limit] {
				fmt.Fprintf(&sb, "> %s: %s\n", strings.TrimSpace(comment.Owner.DisplayName), strings.TrimSpace(comment.Body))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
