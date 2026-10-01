package boards

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
)

// WWR reads the We Work Remotely back-end RSS feed. The feed covers every
// back-end language, so these postings are not GoOnly: rules keep the Go ones.
type WWR struct{ http Getter }

func NewWWR(http Getter) *WWR { return &WWR{http: http} }

func (s *WWR) Name() string { return "weworkremotely" }

type wwrFeed struct {
	Items []struct {
		Title       string `xml:"title"`
		Region      string `xml:"region"`
		Country     string `xml:"country"`
		Skills      string `xml:"skills"`
		Type        string `xml:"type"`
		Description string `xml:"description"`
		PubDate     string `xml:"pubDate"`
		Link        string `xml:"link"`
		GUID        string `xml:"guid"`
	} `xml:"channel>item"`
}

var wwrFeeds = []string{
	"https://weworkremotely.com/categories/remote-back-end-programming-jobs.rss",
	"https://weworkremotely.com/categories/remote-full-stack-programming-jobs.rss",
	"https://weworkremotely.com/categories/remote-devops-sysadmin-jobs.rss",
}

func (s *WWR) Fetch(ctx context.Context) ([]models.Posting, error) {
	var out []models.Posting
	for _, feedURL := range wwrFeeds {
		body, err := s.http.Get(ctx, feedURL)
		if err != nil {
			return out, fmt.Errorf("wwr.Fetch: %w", err)
		}
		var feed wwrFeed
		if err := xml.Unmarshal(body, &feed); err != nil {
			return out, fmt.Errorf("wwr.Fetch: parse %s: %w", feedURL, err)
		}

		for _, it := range feed.Items {
			// Titles are "Company: Role".
			company, title, found := strings.Cut(it.Title, ": ")
			if !found {
				company, title = "", it.Title
			}
			text, links := htmlToText(it.Description)
			posted, _ := time.Parse(time.RFC1123Z, it.PubDate)
			location := it.Region
			if it.Country != "" && !strings.Contains(strings.ToLower(it.Region), "anywhere") {
				location = strings.TrimSpace(it.Region + " " + it.Country)
			}
			out = append(out, models.Posting{
				Source:     "weworkremotely",
				SourceName: "We Work Remotely",
				ExternalID: it.GUID,
				URL:        it.Link,
				Title:      strings.TrimSpace(title),
				Company:    strings.TrimSpace(company),
				Location:   location,
				Text:       text,
				Links:      links,
				Tags:       splitSkills(it.Skills),
				Hints:      it.Type,
				Remote:     true,
				ApplyURL:   it.Link,
				PostedAt:   posted,
			})
		}
	}
	return out, nil
}

// splitSkills turns "Java, PostgreSQL, and Spring" into a list.
func splitSkills(s string) []string {
	s = strings.ReplaceAll(s, " and ", ", ")
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
