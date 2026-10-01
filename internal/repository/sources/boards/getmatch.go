package boards

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/Beknur1003/gojobs/internal/models"
)

// getmatchPages is how many of the newest Go-looking vacancy pages one run
// reads. The sitemap lists every vacancy ever published; open ones are
// among the newest, and an archived page is skipped after one request.
const getmatchPages = 150

// getmatchGoURL matches a vacancy whose address names Go:
// /vacancies/35405-senior-go-razrabotchik-gigachat.
var getmatchGoURL = regexp.MustCompile(`<loc>(https://getmatch\.ru/vacancies/(\d+)-(?:[a-z0-9-]*-)?(?:go|golang)(?:-[a-z0-9-]*)?)</loc>`)

// GetMatch reads getmatch.ru, a Russian IT board where vacancies state their
// salary. Its API is closed to robots (robots.txt), so this reads what is
// open: the sitemap for vacancy addresses and each page's schema.org
// JobPosting and description.
type GetMatch struct{ http Getter }

func NewGetMatch(http Getter) *GetMatch { return &GetMatch{http: http} }

func (s *GetMatch) Name() string { return "getmatch" }

func (s *GetMatch) Fetch(ctx context.Context) ([]models.Posting, error) {
	sitemap, err := s.http.Get(ctx, "https://getmatch.ru/sitemap.xml")
	if err != nil {
		return nil, fmt.Errorf("getmatch.Fetch: sitemap: %w", err)
	}
	type page struct {
		url string
		id  int
	}
	var pages []page
	for _, m := range getmatchGoURL.FindAllSubmatch(sitemap, -1) {
		id, _ := strconv.Atoi(string(m[2]))
		pages = append(pages, page{url: string(m[1]), id: id})
	}
	slices.SortFunc(pages, func(a, b page) int { return b.id - a.id })
	pages = pages[:min(getmatchPages, len(pages))]

	var out []models.Posting
	for _, p := range pages {
		body, err := s.http.Get(ctx, p.url)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			continue
		}
		jp, ok := findJobPosting(body)
		if !ok {
			continue // archived: the page drops its JobPosting
		}
		desc := sectionHTML(body, "b-vacancy-description")
		if desc == "" {
			desc = jp.Description
		}
		text, links := htmlToText(desc)
		out = append(out, models.Posting{
			Source:     "getmatch",
			SourceName: "GetMatch",
			ExternalID: strconv.Itoa(p.id),
			URL:        p.url,
			ApplyURL:   p.url,
			Title:      jp.Title,
			Company:    jp.Organization.Name,
			Location:   jp.location(),
			Text:       text,
			Links:      links,
			Salary:     jp.salary(),
			Remote:     jp.LocationType == "TELECOMMUTE",
			PostedAt:   jp.posted(),
		})
	}
	return out, nil
}
