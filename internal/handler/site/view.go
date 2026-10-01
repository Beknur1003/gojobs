package site

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Beknur1003/gojobs/internal/models"
)

type basePage struct {
	Title    string
	BasePath string
	BaseURL  string
	Repo     string
	Updated  time.Time
	FeedSize int
	FeedDays int
}

func (b *Builder) basePage(updated time.Time, feedSize int) basePage {
	return basePage{
		Title: b.cfg.Title, BasePath: b.cfg.BasePath, BaseURL: b.cfg.BaseURL, Repo: b.cfg.Repo,
		Updated: updated, FeedSize: feedSize, FeedDays: b.cfg.FeedDays,
	}
}

type indexPage struct {
	basePage
	Jobs  []card // first cards, rendered server-side for no-JS readers and crawlers
	Total int
	Stats feedStats
}

type jobPage struct {
	basePage
	Job    jobView
	InFeed bool
}

type aboutPage struct {
	basePage
	Feeds []feedRow
}

type feedJSON struct {
	Updated time.Time `json:"updated"`
	Jobs    []card    `json:"jobs"`
}

// card is one feed entry. Keys are short: the whole feed ships as one file.
type card struct {
	ID         string   `json:"id"`
	Slug       string   `json:"s"`
	Title      string   `json:"t"`
	Company    string   `json:"c,omitempty"`
	Summary    string   `json:"m,omitempty"`
	Salary     string   `json:"sal,omitempty"`
	USDMin     int      `json:"u0,omitempty"`
	USDMax     int      `json:"u1,omitempty"`
	Formats    []string `json:"f,omitempty"`
	Grades     []string `json:"g,omitempty"`
	English    string   `json:"e,omitempty"`
	Stack      []string `json:"k,omitempty"`
	Lang       string   `json:"l"`
	Location   string   `json:"loc,omitempty"`
	Relocation bool     `json:"r,omitempty"`
	Contacts   []string `json:"ct,omitempty"` // contact kinds present
	Kind       string   `json:"src"`          // telegram | board | company | hn
	SourceName string   `json:"sn"`
	More       int      `json:"n,omitempty"` // other places that published it
	Posted     int64    `json:"d"`
}

type feedStats struct {
	Total, Direct, WithSalary, Remote int
}

func statsOf(feed []models.Job) feedStats {
	s := feedStats{Total: len(feed)}
	for _, j := range feed {
		if j.HasDirectContact() {
			s.Direct++
		}
		if j.Salary.Known() {
			s.WithSalary++
		}
		for _, f := range j.Formats {
			if f == models.FormatRemote {
				s.Remote++
				break
			}
		}
	}
	return s
}

func cardsOf(jobs []models.Job, limit int) []card {
	out := make([]card, 0, min(limit, len(jobs)))
	for _, j := range jobs[:min(limit, len(jobs))] {
		c := card{
			ID: j.ID, Slug: j.Slug, Title: j.Title, Company: j.Company, Summary: j.Summary,
			Salary: salaryText(j.Salary), USDMin: j.Salary.MonthlyUSDMin, USDMax: j.Salary.MonthlyUSDMax,
			Formats: strs(j.Formats), Grades: strs(j.Grades), English: j.English, Stack: firstN(j.Stack, 8),
			Lang: j.Lang, Location: j.Location, Relocation: j.Relocation,
			Kind: kindOf(j.Sources[0].Source), SourceName: j.Sources[0].Name, More: len(j.Sources) - 1,
			Posted: j.PostedAt.Unix(),
		}
		seen := map[models.ContactKind]bool{}
		for _, ct := range j.Contacts {
			if !seen[ct.Kind] {
				seen[ct.Kind] = true
				c.Contacts = append(c.Contacts, string(ct.Kind))
			}
		}
		out = append(out, c)
	}
	return out
}

func kindOf(source string) string {
	switch source {
	case "telegram", "hn":
		return source
	case "greenhouse", "lever", "ashby":
		return "company"
	default:
		return "board"
	}
}

// jobView is a Job prepared for its page.
type jobView struct {
	models.Job
	SalaryText string
	SalaryUSD  string
	Emails     []string
	Telegrams  []string
	Links      []string
}

func viewOf(j models.Job) jobView {
	v := jobView{Job: j, SalaryText: salaryText(j.Salary), SalaryUSD: salaryUSD(j.Salary)}
	for _, c := range j.Contacts {
		switch c.Kind {
		case models.ContactEmail:
			v.Emails = append(v.Emails, c.Value)
		case models.ContactTelegram:
			v.Telegrams = append(v.Telegrams, c.Value)
		case models.ContactURL:
			if c.Value != j.ApplyURL {
				v.Links = append(v.Links, c.Value)
			}
		}
	}
	return v
}

type feedRow struct {
	Name, Kind, Link string
	Status           models.FeedStatus
	OK               bool
}

func feedRows(feeds map[string]models.FeedStatus) []feedRow {
	rows := make([]feedRow, 0, len(feeds))
	for name, st := range feeds {
		kind, id, _ := strings.Cut(name, ":")
		r := feedRow{Name: name, Kind: kind, Status: st, OK: st.Error == "" && !st.LastOK.IsZero()}
		switch kind {
		case "telegram":
			r.Name, r.Link = "@"+id, "https://t.me/s/"+id
		case "greenhouse", "lever", "ashby":
			r.Name = id + " · " + kind
		default:
			r.Name, r.Link = boardNames[kind], boardLinks[kind]
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, k int) bool {
		if rows[i].Kind != rows[k].Kind {
			return kindOrder(rows[i].Kind) < kindOrder(rows[k].Kind)
		}
		return rows[i].Name < rows[k].Name
	})
	return rows
}

var boardNames = map[string]string{
	"remoteok": "Remote OK", "remotive": "Remotive", "himalayas": "Himalayas", "jobicy": "Jobicy",
	"hn": "HN: Who is hiring", "weworkremotely": "We Work Remotely",
}

var boardLinks = map[string]string{
	"remoteok": "https://remoteok.com/remote-golang-jobs", "remotive": "https://remotive.com",
	"himalayas": "https://himalayas.app/jobs/golang", "jobicy": "https://jobicy.com",
	"hn": "https://news.ycombinator.com/submitted?id=whoishiring", "weworkremotely": "https://weworkremotely.com",
}

func kindOrder(k string) int {
	switch k {
	case "telegram":
		return 0
	case "greenhouse", "lever", "ashby":
		return 2
	default:
		return 1
	}
}

// ---------------------------------------------------------------- formatting

var currencySign = map[string]string{
	"USD": "$", "EUR": "€", "GBP": "£", "RUB": "₽", "KZT": "₸", "UAH": "₴", "USDT": "USDT",
}

var periodText = map[models.Period]string{models.PeriodMonth: "/мес", models.PeriodYear: "/год", models.PeriodHour: "/час"}

// salaryText is the salary as stated: "250 000 – 350 000 ₽/мес".
func salaryText(s models.Salary) string {
	if !s.Known() {
		return ""
	}
	sign := currencySign[s.Currency]
	if sign == "" {
		sign = s.Currency
	}
	prefix := sign == "$" || sign == "€" || sign == "£"
	money := func(v int) string {
		if prefix {
			return sign + groupThousands(v)
		}
		return groupThousands(v) + " " + sign
	}

	var out string
	switch {
	case s.Min > 0 && s.Max > 0 && s.Min != s.Max:
		if prefix {
			out = money(s.Min) + " – " + money(s.Max)
		} else {
			out = groupThousands(s.Min) + " – " + money(s.Max)
		}
	case s.Min > 0:
		out = "от " + money(s.Min)
	default:
		out = "до " + money(s.Max)
	}
	return out + periodText[s.Period]
}

// salaryUSD is the monthly dollar estimate, shown only when it adds something.
func salaryUSD(s models.Salary) string {
	if !s.Known() || (s.Currency == "USD" && s.Period == models.PeriodMonth) {
		return ""
	}
	lo, hi := s.MonthlyUSDMin, s.MonthlyUSDMax
	switch {
	case lo > 0 && hi > 0 && lo != hi:
		return "≈ $" + groupThousands(roundTo(lo)) + " – " + groupThousands(roundTo(hi)) + " в месяц"
	case lo > 0:
		return "≈ от $" + groupThousands(roundTo(lo)) + " в месяц"
	default:
		return "≈ до $" + groupThousands(roundTo(hi)) + " в месяц"
	}
}

func roundTo(v int) int { return (v + 50) / 100 * 100 }

func groupThousands(v int) string {
	s := strconv.Itoa(v)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

var gradeText = map[string]string{"intern": "Стажёр", "junior": "Junior", "middle": "Middle", "senior": "Senior", "lead": "Lead"}
var formatText = map[string]string{"remote": "Удалёнка", "hybrid": "Гибрид", "office": "Офис"}
var employmentText = map[string]string{"fulltime": "Полная занятость", "parttime": "Частичная", "contract": "Контракт / B2B"}

var months = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

func ruDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d %s %d", t.Day(), months[t.Month()-1], t.Year())
}

var (
	linkRe = regexp.MustCompile(`https?://[^\s<>"]+[^\s<>".,;:!?)\]»'’]|[A-Za-z0-9][A-Za-z0-9._%+-]*@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}|(?:^|[^\p{L}\p{N}_.@/])@[A-Za-z][A-Za-z0-9_]{3,31}`)
)

// linkify escapes the text and turns URLs, emails and @handles into links.
func linkify(text string) template.HTML {
	var b strings.Builder
	last := 0
	for _, loc := range linkRe.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		m := text[start:end]
		// A "@handle" match carries the character before it (space, bracket,
		// dash); that character stays plain text.
		if r, size := utf8.DecodeRuneInString(m); r != '@' && size < len(m) && m[size] == '@' && !isEmailRune(r) {
			start += size
			m = m[size:]
		}
		b.WriteString(template.HTMLEscapeString(text[last:start]))
		switch {
		case strings.HasPrefix(m, "http"):
			fmt.Fprintf(&b, `<a href="%s" target="_blank" rel="noopener">%s</a>`, template.HTMLEscapeString(m), template.HTMLEscapeString(shortURL(m)))
		case strings.HasPrefix(m, "@"):
			fmt.Fprintf(&b, `<a href="https://t.me/%s" target="_blank" rel="noopener">%s</a>`, m[1:], template.HTMLEscapeString(m))
		default:
			fmt.Fprintf(&b, `<a href="mailto:%s">%s</a>`, template.HTMLEscapeString(m), template.HTMLEscapeString(m))
		}
		last = end
	}
	b.WriteString(template.HTMLEscapeString(text[last:]))
	return template.HTML(b.String()) //nolint:gosec // every piece above is escaped
}

func isEmailRune(r rune) bool {
	return r < 128 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || strings.ContainsRune("._%+-", r))
}

func shortURL(u string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	s = strings.TrimPrefix(s, "www.")
	if r := []rune(s); len(r) > 60 {
		return string(r[:57]) + "…"
	}
	return s
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	return strings.TrimPrefix(p.Host, "www.")
}

func (b *Builder) funcs() template.FuncMap {
	return template.FuncMap{
		"path":       func(p string) string { return b.cfg.BasePath + p },
		"abs":        func(p string) string { return b.cfg.BaseURL + p },
		"date":       ruDate,
		"iso":        func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		"unix":       func(t time.Time) int64 { return t.Unix() },
		"grade":      func(g models.Grade) string { return gradeText[string(g)] },
		"gradeKey":   func(g string) string { return gradeText[g] },
		"format":     func(f models.WorkFormat) string { return formatText[string(f)] },
		"formatKey":  func(f string) string { return formatText[f] },
		"employment": func(e models.Employment) string { return employmentText[string(e)] },
		"upper":      strings.ToUpper,
		"linkify":    linkify,
		"host":       hostOf,
		"short":      shortURL,
		"num":        groupThousands,
		"mailto": func(addr, title string) string {
			return "mailto:" + addr + "?subject=" + url.PathEscape("Отклик: "+title)
		},
		"plural": plural,
		"json": func(v any) template.JS {
			raw, _ := jsonMarshal(v)
			return template.JS(raw) //nolint:gosec // json.Marshal escapes <, > and &
		},
		"seo": func(j jobView) string {
			parts := []string{j.Title}
			if j.Company != "" {
				parts = append(parts, j.Company)
			}
			if j.SalaryText != "" {
				parts = append(parts, j.SalaryText)
			}
			for _, f := range j.Formats {
				parts = append(parts, formatText[string(f)])
			}
			return strings.Join(parts, " · ") + ". " + j.Summary
		},
	}
}

// plural picks the Russian form: plural(n, "вакансия", "вакансии", "вакансий").
func plural(n int, one, few, many string) string {
	n = n % 100
	switch {
	case n >= 11 && n <= 14:
		return many
	case n%10 == 1:
		return one
	case n%10 >= 2 && n%10 <= 4:
		return few
	default:
		return many
	}
}

// ---------------------------------------------------------------- sitemap, RSS

func (b *Builder) sitemap(feed []models.Job) []byte {
	var buf strings.Builder
	buf.WriteString(xml.Header + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	write := func(loc string, mod time.Time) {
		buf.WriteString("<url><loc>")
		_ = xml.EscapeText(&xmlWriter{&buf}, []byte(b.cfg.BaseURL+loc))
		buf.WriteString("</loc>")
		if !mod.IsZero() {
			buf.WriteString("<lastmod>" + mod.UTC().Format("2006-01-02") + "</lastmod>")
		}
		buf.WriteString("</url>\n")
	}
	write("/", b.now())
	write("/about/", time.Time{})
	for _, j := range feed {
		write("/jobs/"+j.Slug+"/", j.LastSeen)
	}
	buf.WriteString("</urlset>\n")
	return []byte(buf.String())
}

func (b *Builder) rss(feed []models.Job, updated time.Time) []byte {
	type item struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		GUID        string `xml:"guid"`
		PubDate     string `xml:"pubDate"`
		Description string `xml:"description"`
	}
	type channel struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Language    string `xml:"language"`
		LastBuild   string `xml:"lastBuildDate"`
		Items       []item `xml:"item"`
	}
	type rssDoc struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel channel  `xml:"channel"`
	}

	doc := rssDoc{Version: "2.0", Channel: channel{
		Title: b.cfg.Title, Link: b.cfg.BaseURL + "/", Language: "ru",
		Description: "Свежие вакансии Go-разработчиков с прямыми контактами", LastBuild: updated.UTC().Format(time.RFC1123Z),
	}}
	for _, j := range feed[:min(100, len(feed))] {
		title := j.Title
		if j.Company != "" {
			title += " — " + j.Company
		}
		desc := j.Summary
		if s := salaryText(j.Salary); s != "" {
			desc = s + ". " + desc
		}
		link := b.cfg.BaseURL + "/jobs/" + j.Slug + "/"
		doc.Channel.Items = append(doc.Channel.Items, item{
			Title: title, Link: link, GUID: link, PubDate: j.PostedAt.UTC().Format(time.RFC1123Z), Description: desc,
		})
	}
	raw, _ := xml.MarshalIndent(doc, "", " ")
	return append([]byte(xml.Header), raw...)
}

type xmlWriter struct{ b *strings.Builder }

func (w *xmlWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

func strs[T ~string](in []T) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, string(v))
	}
	return out
}

func firstN(in []string, n int) []string { return in[:min(n, len(in))] }
