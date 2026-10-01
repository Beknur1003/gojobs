package boards

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
)

// habrSearches are the list queries: the Golang skill tag, and a text search
// for roles that name Go without tagging it. Both are needed; neither covers
// the other.
var habrSearches = []url.Values{
	{"skills[]": {"101"}, "type": {"all"}},
	{"q": {"golang"}, "type": {"all"}},
}

// habrMaxPages caps one query; Go roles there fill one or two pages of 25.
const habrMaxPages = 8

// Habr reads Habr Career (career.habr.com), the largest Russian-language IT
// board after hh.ru. The list comes from the JSON its own pages use; the
// description from each vacancy page's schema.org JobPosting.
type Habr struct{ http Getter }

func NewHabr(http Getter) *Habr { return &Habr{http: http} }

func (s *Habr) Name() string { return "habr" }

type habrVacancy struct {
	ID         int    `json:"id"`
	Href       string `json:"href"`
	Title      string `json:"title"`
	RemoteWork bool   `json:"remoteWork"`
	Company    struct {
		Title string `json:"title"`
	} `json:"company"`
	Salary struct {
		From     flexInt `json:"from"`
		To       flexInt `json:"to"`
		Currency string  `json:"currency"`
	} `json:"salary"`
	Locations []struct {
		Title string `json:"title"`
	} `json:"locations"`
	Skills []struct {
		Title string `json:"title"`
	} `json:"skills"`
	Qualification string `json:"qualification"`
	Published     struct {
		Date string `json:"date"`
	} `json:"publishedDate"`
}

func (s *Habr) Fetch(ctx context.Context) ([]models.Posting, error) {
	seen := map[int]bool{}
	var list []habrVacancy
	for _, search := range habrSearches {
		for page := 1; page <= habrMaxPages; page++ {
			q := url.Values{}
			for k, v := range search {
				q[k] = v
			}
			q.Set("page", strconv.Itoa(page))
			var resp struct {
				List []habrVacancy `json:"list"`
				Meta struct {
					TotalPages int `json:"totalPages"`
				} `json:"meta"`
			}
			if err := s.http.GetJSON(ctx, "https://career.habr.com/api/frontend/vacancies?"+q.Encode(), &resp); err != nil {
				return nil, fmt.Errorf("habr.Fetch: %w", err)
			}
			for _, v := range resp.List {
				if v.ID > 0 && !seen[v.ID] {
					seen[v.ID] = true
					list = append(list, v)
				}
			}
			if page >= resp.Meta.TotalPages {
				break
			}
		}
	}

	out := make([]models.Posting, 0, len(list))
	for _, v := range list {
		link := "https://career.habr.com/vacancies/" + strconv.Itoa(v.ID)
		page, err := s.http.Get(ctx, link)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			continue // one page failing should not drop the board
		}
		jp, ok := findJobPosting(page)
		if !ok {
			continue // archived since the list was read
		}
		text, links := htmlToText(jp.Description)
		out = append(out, habrPosting(v, link, text, links))
	}
	return out, nil
}

func habrPosting(v habrVacancy, link, text string, links []string) models.Posting {
	var locs, tags []string
	for _, l := range v.Locations {
		locs = append(locs, l.Title)
	}
	for _, sk := range v.Skills {
		tags = append(tags, sk.Title)
	}
	posted, _ := time.Parse(time.RFC3339, v.Published.Date)
	p := models.Posting{
		Source:     "habr",
		SourceName: "Хабр Карьера",
		ExternalID: strconv.Itoa(v.ID),
		URL:        link,
		ApplyURL:   link,
		Title:      strings.TrimSpace(v.Title),
		Company:    strings.TrimSpace(v.Company.Title),
		Location:   strings.Join(locs, ", "),
		Text:       text,
		Links:      links,
		Tags:       tags,
		Hints:      v.Qualification,
		Remote:     v.RemoteWork,
		PostedAt:   posted.UTC(),
	}
	if lo, hi := int(v.Salary.From), int(v.Salary.To); lo > 0 || hi > 0 {
		cur := strings.ToUpper(v.Salary.Currency)
		if cur == "RUR" {
			cur = "RUB"
		}
		p.Salary = models.Salary{Min: lo, Max: hi, Currency: cur, Period: models.PeriodMonth}
	}
	return p
}
