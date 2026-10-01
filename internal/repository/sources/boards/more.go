package boards

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
)

// ---------------------------------------------------------------- RSS

// RSS reads a plain job feed: Djinni, golangprojects. Feeds differ only in
// how the title carries the company, so that part is a function.
type RSS struct {
	http    Getter
	id      string // source id: "djinni"
	display string // "Djinni"
	urls    []string
	goOnly  bool
	split   func(title string) (role, company string)
}

func (s *RSS) Name() string { return s.id }

// NewDjinni reads Djinni's Go feed: Ukrainian and remote roles, titles
// without the company.
func NewDjinni(http Getter) *RSS {
	return &RSS{
		http: http, id: "djinni", display: "Djinni", goOnly: true,
		urls:  []string{"https://djinni.co/jobs/rss/?primary_keyword=Golang"},
		split: func(t string) (string, string) { return t, "" },
	}
}

// NewGolangProjects reads golangprojects.com, a Go-only board: "Role @ Company".
func NewGolangProjects(http Getter) *RSS {
	return &RSS{
		http: http, id: "golangprojects", display: "Golang Projects", goOnly: true,
		urls: []string{"https://www.golangprojects.com/rss.xml"},
		split: func(t string) (string, string) {
			// The feed puts non-breaking spaces around "@".
			t = strings.Join(strings.Fields(t), " ")
			if i := strings.LastIndex(t, " @ "); i > 0 {
				return t[:i], t[i+3:]
			}
			return t, ""
		},
	}
}

type rssFeed struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		GUID        string `xml:"guid"`
		Description string `xml:"description"`
		PubDate     string `xml:"pubDate"`
	} `xml:"channel>item"`
}

func (s *RSS) Fetch(ctx context.Context) ([]models.Posting, error) {
	var out []models.Posting
	for _, u := range s.urls {
		body, err := s.http.Get(ctx, u)
		if err != nil {
			return out, fmt.Errorf("%s.Fetch: %w", s.id, err)
		}
		var feed rssFeed
		if err := xml.Unmarshal(body, &feed); err != nil {
			return out, fmt.Errorf("%s.Fetch: parse: %w", s.id, err)
		}
		for _, it := range feed.Items {
			if it.Link == "" {
				continue
			}
			role, company := s.split(strings.TrimSpace(it.Title))
			text, links := htmlToText(it.Description)
			id := it.GUID
			if id == "" {
				id = it.Link
			}
			out = append(out, models.Posting{
				Source:     s.id,
				SourceName: s.display,
				ExternalID: id,
				URL:        it.Link,
				Title:      role,
				Company:    company,
				Text:       text,
				Links:      links,
				ApplyURL:   it.Link,
				PostedAt:   parseRSSDate(it.PubDate),
				GoOnly:     s.goOnly,
			})
		}
	}
	return out, nil
}

func parseRSSDate(s string) time.Time {
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// ---------------------------------------------------------------- Arbeitnow

// arbeitnowPages caps one run; the newest pages are what changes daily.
const arbeitnowPages = 4

// Arbeitnow is a free European board (mostly Germany). It has every role, so
// postings are not GoOnly.
type Arbeitnow struct{ http Getter }

func NewArbeitnow(http Getter) *Arbeitnow { return &Arbeitnow{http: http} }

func (s *Arbeitnow) Name() string { return "arbeitnow" }

func (s *Arbeitnow) Fetch(ctx context.Context) ([]models.Posting, error) {
	var out []models.Posting
	for page := 1; page <= arbeitnowPages; page++ {
		var resp struct {
			Data []struct {
				Slug        string   `json:"slug"`
				Company     string   `json:"company_name"`
				Title       string   `json:"title"`
				Description string   `json:"description"`
				Remote      bool     `json:"remote"`
				URL         string   `json:"url"`
				Tags        []string `json:"tags"`
				JobTypes    []string `json:"job_types"`
				Location    string   `json:"location"`
				CreatedAt   flexInt  `json:"created_at"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := s.http.GetJSON(ctx, "https://www.arbeitnow.com/api/job-board-api?page="+strconv.Itoa(page), &resp); err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return nil, fmt.Errorf("arbeitnow.Fetch: %w", err)
		}
		for _, j := range resp.Data {
			text, links := htmlToText(j.Description)
			// The API's url is the listing page (rarely the employer's site);
			// a link built from the slug alone is a 404.
			link := j.URL
			if !strings.Contains(link, "arbeitnow.") {
				// An employer homepage is shared by its roles and would merge
				// them by URL; such rare rows are skipped.
				continue
			}
			out = append(out, models.Posting{
				Source:     "arbeitnow",
				SourceName: "Arbeitnow",
				ExternalID: j.Slug,
				URL:        link,
				Title:      j.Title,
				Company:    j.Company,
				Location:   j.Location,
				Text:       text,
				Links:      links,
				Hints:      strings.Join(j.JobTypes, ", "),
				Remote:     j.Remote,
				ApplyURL:   link,
				PostedAt:   time.Unix(int64(j.CreatedAt), 0).UTC(),
			})
		}
		if resp.Links.Next == "" || len(resp.Data) == 0 {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- Working Nomads

// WorkingNomads publishes its latest remote roles as one JSON list.
type WorkingNomads struct{ http Getter }

func NewWorkingNomads(http Getter) *WorkingNomads { return &WorkingNomads{http: http} }

func (s *WorkingNomads) Name() string { return "workingnomads" }

func (s *WorkingNomads) Fetch(ctx context.Context) ([]models.Posting, error) {
	var jobs []struct {
		URL         string `json:"url"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Company     string `json:"company_name"`
		Category    string `json:"category_name"`
		Tags        string `json:"tags"`
		Location    string `json:"location"`
		PubDate     string `json:"pub_date"`
	}
	if err := s.http.GetJSON(ctx, "https://www.workingnomads.com/api/exposed_jobs/", &jobs); err != nil {
		return nil, fmt.Errorf("workingnomads.Fetch: %w", err)
	}
	out := make([]models.Posting, 0, len(jobs))
	for _, j := range jobs {
		text, links := htmlToText(j.Description)
		posted, _ := time.Parse(time.RFC3339, j.PubDate)
		out = append(out, models.Posting{
			Source:     "workingnomads",
			SourceName: "Working Nomads",
			ExternalID: j.URL,
			URL:        j.URL,
			Title:      j.Title,
			Company:    j.Company,
			Location:   j.Location,
			Text:       text,
			Links:      links,
			Tags:       splitSkills(j.Tags),
			Hints:      j.Category,
			Remote:     true,
			ApplyURL:   j.URL,
			PostedAt:   posted.UTC(),
		})
	}
	return out, nil
}
