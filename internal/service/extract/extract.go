// Package extract turns a raw posting into a structured job with plain rules:
// regular expressions and small dictionaries, no model calls. Hirify uses a
// cascade of LLMs for this; rules get most of the way for one language (Go)
// and run anywhere for free.
package extract

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/Beknur1003/gojobs/internal/models"
)

// Noise is what a channel repeats under every post: "subscribe to @channel",
// the admin's handle, a folder link. It is learned per channel from the batch
// (see pipeline) and removed before extraction.
type Noise struct {
	Lines    map[string]bool // CleanLine'd lines
	Contacts map[string]bool // ContactKey values
}

const summaryLen = 240

// Normalize builds a Job from one posting. It does not decide whether the
// posting belongs on the board; IsVacancy and IsGo do that.
func Normalize(p models.Posting, noise Noise) models.Job {
	text, removed := StripNoise(p.Text, noise.Lines)
	links := p.Links
	if channel, ok := strings.CutPrefix(p.Feed, "telegram:"); ok {
		var footer string
		text, footer = StripFooter(text, channel)
		removed += "\n" + footer
	}
	links = DropFooterLinks(links, strings.TrimSpace(removed), text)

	title, company := strings.TrimSpace(p.Title), strings.TrimSpace(p.Company)
	if title == "" || company == "" {
		t, c := TitleAndCompany(text)
		if title == "" {
			title = t
		}
		if company == "" {
			company = c
		}
	}
	title = CleanLine(title)

	head := strings.Join([]string{title, p.Hints, p.Location, firstRunes(text, 700)}, "\n")

	salary := Monthly(p.Salary)
	if !salary.Known() || !plausible(salary) {
		salary = Salary(title + "\n" + p.Hints + "\n" + text)
	}

	withURLs := p.Source == "telegram" || p.Source == "hn"
	id := ID(p.Source, p.ExternalID)

	return models.Job{
		ID:         id,
		Slug:       Slug(title, id),
		Title:      title,
		Company:    company,
		Text:       text,
		Summary:    Summary(text, title, company, summaryLen),
		Salary:     salary,
		Formats:    Formats(head, p.Remote),
		Grades:     Grades(title, p.Hints+"\n"+firstRunes(text, 500)),
		Employment: Employment(head),
		English:    English(p.Hints + "\n" + text),
		Stack:      Stack(title + "\n" + text + "\n" + strings.Join(p.Tags, " ")),
		Location:   strings.TrimSpace(p.Location),
		Relocation: Relocation(head + "\n" + text),
		Lang:       Lang(text),
		Contacts:   Contacts(text, links, noise.Contacts, withURLs),
		ApplyURL:   p.ApplyURL,
		Sources: []models.SourceRef{{
			Source: p.Source, Feed: p.Feed, Name: p.SourceName, ExternalID: p.ExternalID, URL: p.URL, PostedAt: p.PostedAt,
		}},
		PostedAt: p.PostedAt,
	}
}

// StripNoise drops lines that the channel repeats under every post and
// returns them separately, so links from those lines can be dropped too.
func StripNoise(text string, lines map[string]bool) (kept, removed string) {
	if len(lines) == 0 {
		return text, ""
	}
	var keep, drop []string
	for _, l := range strings.Split(text, "\n") {
		if c := CleanLine(l); c != "" && lines[c] {
			drop = append(drop, l)
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimSpace(strings.Join(keep, "\n")), strings.Join(drop, "\n")
}

// ID is stable for a given source posting, so a re-run never renames a page.
func ID(source, externalID string) string {
	sum := sha1.Sum([]byte(source + "|" + externalID))
	return hex.EncodeToString(sum[:])[:10]
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug is a readable, ASCII URL segment: "senior-go-razrabotchik-3f9a1c".
func Slug(title, id string) string {
	s := nonSlug.ReplaceAllString(Translit(strings.ToLower(title)), "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		return id[:6]
	}
	return s + "-" + id[:6]
}

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z",
	'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r",
	'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	'қ': "q", 'ғ': "g", 'ү': "u", 'ұ': "u", 'ң': "n", 'ө': "o", 'һ': "h", 'ә': "a", 'і': "i",
}

func Translit(s string) string {
	var b strings.Builder
	for _, r := range s {
		if t, ok := translit[r]; ok {
			b.WriteString(t)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
