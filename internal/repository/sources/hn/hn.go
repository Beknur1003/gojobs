// Package hn reads the monthly "Ask HN: Who is hiring?" threads through the
// free Algolia HN API. Every top-level comment is one company's post, usually
// opening with a "Company | Role | Location | Remote | Salary" header line.
package hn

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/sources/htmltext"
)

const (
	searchURL = "https://hn.algolia.com/api/v1/search_by_date?tags=story,author_whoishiring&hitsPerPage=10"
	itemURL   = "https://hn.algolia.com/api/v1/items/"
)

type Getter interface {
	GetJSON(ctx context.Context, rawURL string, dst any) error
}

type Source struct {
	http    Getter
	threads int // how many recent monthly threads to read
}

func New(http Getter, threads int) *Source { return &Source{http: http, threads: threads} }

func (s *Source) Name() string { return "hn" }

type item struct {
	ID        int64  `json:"id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt int64  `json:"created_at_i"`
	Children  []item `json:"children"`
}

func (s *Source) Fetch(ctx context.Context) ([]models.Posting, error) {
	var search struct {
		Hits []struct {
			ObjectID string `json:"objectID"`
			Title    string `json:"title"`
		} `json:"hits"`
	}
	if err := s.http.GetJSON(ctx, searchURL, &search); err != nil {
		return nil, fmt.Errorf("hn.Fetch: find threads: %w", err)
	}

	var out []models.Posting
	read := 0
	for _, h := range search.Hits {
		if read == s.threads {
			break
		}
		if !strings.HasPrefix(h.Title, "Ask HN: Who is hiring?") {
			continue
		}
		read++

		var thread item
		if err := s.http.GetJSON(ctx, itemURL+h.ObjectID, &thread); err != nil {
			return out, fmt.Errorf("hn.Fetch: thread %s: %w", h.ObjectID, err)
		}
		for _, c := range thread.Children {
			if p, ok := toPosting(c); ok {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

var headerSep = regexp.MustCompile(`\s*\|\s*`)

func toPosting(c item) (models.Posting, bool) {
	if c.Text == "" || c.Author == "" {
		return models.Posting{}, false // deleted or flagged
	}
	text, links := htmltext.Convert(c.Text)
	header, _, _ := strings.Cut(text, "\n")

	var company, title, location string
	parts := headerSep.Split(header, -1)
	if len(parts) >= 2 {
		company = parts[0]
		title, location = pickRoleAndLocation(parts[1:])
	}

	id := strconv.FormatInt(c.ID, 10)
	return models.Posting{
		Source:     "hn",
		SourceName: "HN Who is hiring",
		ExternalID: id,
		URL:        "https://news.ycombinator.com/item?id=" + id,
		Title:      title,
		Company:    company,
		Location:   location,
		Text:       text,
		Links:      links,
		Hints:      header,
		PostedAt:   time.Unix(c.CreatedAt, 0).UTC(),
	}, true
}

var (
	roleWord = regexp.MustCompile(`(?i)engineer|developer|programmer|sre|devops|architect|lead|cto|head of|founding|backend|platform|infrastructure`)
	urlLike  = regexp.MustCompile(`(?i)^https?://|\.[a-z]{2,}(/|$)`)
)

// pickRoleAndLocation guesses which header fields are the role and the place.
// The header has no fixed order, so the first role-like field wins and the
// first remaining short field that is not a URL or a salary is the location.
func pickRoleAndLocation(fields []string) (role, location string) {
	for _, f := range fields {
		if role == "" && roleWord.MatchString(f) && !urlLike.MatchString(f) {
			role = f
			continue
		}
		if location == "" && !urlLike.MatchString(f) && !strings.ContainsAny(f, "$€£0123456789") && len(f) < 80 {
			location = f
		}
	}
	return role, location
}
