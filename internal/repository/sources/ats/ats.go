// Package ats reads company career pages hosted on Greenhouse, Lever, Ashby
// and Workday through their public job-board APIs. These are vacancies
// straight from the employer. A board lists every role, so each adapter
// takes a Filter and drops what cannot be a Go role before it costs a
// download or memory; the pipeline makes the final call.
package ats

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/httpx"
	"github.com/Beknur1003/gojobs/internal/repository/sources/htmltext"
)

type Getter interface {
	GetJSON(ctx context.Context, rawURL string, dst any) error
}

type Poster interface {
	Getter
	PostJSON(ctx context.Context, rawURL string, payload, dst any) error
}

// Filter decides early what is worth fetching and keeping. Title runs on the
// light list of a board; Keep runs once the description is known.
type Filter struct {
	Title func(title string) bool
	Keep  func(title, text string) bool
}

func (f Filter) title(t string) bool { return f.Title == nil || f.Title(t) }

func (f Filter) keep(title, text string) bool { return f.Keep == nil || f.Keep(title, text) }

// roleError reports a failure on one role. It deliberately does not wrap the
// cause: a 404 on one role must not read as "the board is gone".
func roleError(id any, err error) error {
	return fmt.Errorf("role %v: %v", id, err)
}

// stub reports a listing that is still open and was fetched in full before.
func stub(source, id, url, title string) models.Posting {
	return models.Posting{Source: source, ExternalID: id, URL: url, Title: title, Stub: true}
}

// ---------------------------------------------------------------- Greenhouse

// greenhouseBulk is how many new descriptions justify one content=true
// download of the whole board instead of one request per role.
const greenhouseBulk = 10

// Greenhouse is one company's board; each board is its own feed. It reads the
// board as a light list every day and downloads descriptions only for roles
// it has not seen in this version.
type Greenhouse struct {
	http   Getter
	board  string
	filter Filter
	cache  *Cache
}

func NewGreenhouse(http Getter, board string, filter Filter, cache *Cache) *Greenhouse {
	return &Greenhouse{http: http, board: board, filter: filter, cache: cache}
}

func (s *Greenhouse) Name() string { return "greenhouse:" + s.board }

type greenhouseJob struct {
	ID             int64  `json:"id"`
	Title          string `json:"title"`
	URL            string `json:"absolute_url"`
	Company        string `json:"company_name"`
	FirstPublished string `json:"first_published"`
	UpdatedAt      string `json:"updated_at"`
	Location       struct {
		Name string `json:"name"`
	} `json:"location"`
	Content string `json:"content"` // HTML, entity-escaped once more
}

func (s *Greenhouse) Fetch(ctx context.Context) ([]models.Posting, error) {
	base := "https://boards-api.greenhouse.io/v1/boards/" + url.PathEscape(s.board) + "/jobs"
	var list struct {
		Jobs []greenhouseJob `json:"jobs"`
	}
	if err := s.http.GetJSON(ctx, base, &list); err != nil {
		return nil, fmt.Errorf("greenhouse.Fetch %s: %w", s.board, err)
	}

	feed := s.Name()
	versions := map[string]string{}
	var (
		out  []models.Posting
		need []greenhouseJob
	)
	for _, j := range list.Jobs {
		if !s.filter.title(j.Title) {
			continue
		}
		id := strconv.FormatInt(j.ID, 10)
		if s.cache.Has(feed, id, j.UpdatedAt) {
			versions[id] = j.UpdatedAt
			out = append(out, stub("greenhouse", s.externalID(j.ID), j.URL, j.Title))
			continue
		}
		need = append(need, j)
	}

	full, err := s.details(ctx, base, need)
	for _, j := range full {
		versions[strconv.FormatInt(j.ID, 10)] = j.UpdatedAt
		if p, ok := s.posting(j); ok {
			out = append(out, p)
		}
	}
	s.cache.Replace(feed, versions)
	if err != nil {
		return out, fmt.Errorf("greenhouse.Fetch %s: %w", s.board, err)
	}
	return out, nil
}

// details returns need with descriptions filled in. Roles whose download
// failed are left out, so they are retried on the next run.
func (s *Greenhouse) details(ctx context.Context, base string, need []greenhouseJob) ([]greenhouseJob, error) {
	if len(need) == 0 {
		return nil, nil
	}
	if len(need) >= greenhouseBulk {
		var all struct {
			Jobs []greenhouseJob `json:"jobs"`
		}
		if err := s.http.GetJSON(ctx, base+"?content=true", &all); err != nil {
			return nil, err
		}
		byID := make(map[int64]greenhouseJob, len(all.Jobs))
		for _, j := range all.Jobs {
			byID[j.ID] = j
		}
		var out []greenhouseJob
		for _, j := range need {
			if full, ok := byID[j.ID]; ok {
				out = append(out, full)
			}
		}
		return out, nil
	}

	var out []greenhouseJob
	for _, j := range need {
		var full greenhouseJob
		if err := s.http.GetJSON(ctx, base+"/"+strconv.FormatInt(j.ID, 10), &full); err != nil {
			if errors.Is(err, httpx.ErrNotFound) {
				continue // the role closed between the list and now
			}
			return out, roleError(j.ID, err)
		}
		if full.Company == "" {
			full.Company = j.Company
		}
		out = append(out, full)
	}
	return out, nil
}

func (s *Greenhouse) posting(j greenhouseJob) (models.Posting, bool) {
	text, links := htmltext.Convert(html.UnescapeString(j.Content))
	if !s.filter.keep(j.Title, text) {
		return models.Posting{}, false
	}
	posted, err := time.Parse(time.RFC3339, j.FirstPublished)
	if err != nil {
		posted, _ = time.Parse(time.RFC3339, j.UpdatedAt)
	}
	company := j.Company
	if company == "" {
		company = s.board
	}
	return models.Posting{
		Source:     "greenhouse",
		SourceName: company + " (Greenhouse)",
		ExternalID: s.externalID(j.ID),
		URL:        j.URL,
		Title:      j.Title,
		Company:    company,
		Location:   j.Location.Name,
		Text:       text,
		Links:      links,
		ApplyURL:   j.URL,
		PostedAt:   posted,
	}, true
}

func (s *Greenhouse) externalID(id int64) string { return fmt.Sprintf("%s/%d", s.board, id) }

// ---------------------------------------------------------------- Lever

// Lever is one company's board. Its API has no light mode: descriptions come
// with the list, so the filter applies after one download.
type Lever struct {
	http    Getter
	company string
	filter  Filter
}

func NewLever(http Getter, company string, filter Filter) *Lever {
	return &Lever{http: http, company: company, filter: filter}
}

func (s *Lever) Name() string { return "lever:" + s.company }

func (s *Lever) Fetch(ctx context.Context) ([]models.Posting, error) {
	var jobs []struct {
		ID         string `json:"id"`
		Text       string `json:"text"`
		CreatedAt  int64  `json:"createdAt"`
		HostedURL  string `json:"hostedUrl"`
		ApplyURL   string `json:"applyUrl"`
		Workplace  string `json:"workplaceType"`
		Categories struct {
			Location   string `json:"location"`
			Commitment string `json:"commitment"`
		} `json:"categories"`
		Description string `json:"description"`
		Lists       []struct {
			Text    string `json:"text"`
			Content string `json:"content"`
		} `json:"lists"`
		Additional  string `json:"additional"`
		SalaryRange *struct {
			Min      float64 `json:"min"` // hourly rates come as 32.34
			Max      float64 `json:"max"`
			Currency string  `json:"currency"`
			Interval string  `json:"interval"`
		} `json:"salaryRange"`
	}
	u := "https://api.lever.co/v0/postings/" + url.PathEscape(s.company) + "?mode=json"
	if err := s.http.GetJSON(ctx, u, &jobs); err != nil {
		return nil, fmt.Errorf("lever.Fetch %s: %w", s.company, err)
	}

	var out []models.Posting
	for _, j := range jobs {
		if !s.filter.title(j.Text) {
			continue
		}
		var body strings.Builder
		body.WriteString(j.Description)
		for _, l := range j.Lists {
			body.WriteString("<h3>" + l.Text + "</h3><ul>" + l.Content + "</ul>")
		}
		body.WriteString(j.Additional)
		text, links := htmltext.Convert(body.String())
		if !s.filter.keep(j.Text, text) {
			continue
		}

		var salary models.Salary
		if r := j.SalaryRange; r != nil {
			salary = models.Salary{Min: int(math.Round(r.Min)), Max: int(math.Round(r.Max)), Currency: strings.ToUpper(r.Currency), Period: leverPeriod(r.Interval)}
		}
		out = append(out, models.Posting{
			Source:     "lever",
			SourceName: s.company + " (Lever)",
			ExternalID: s.company + "/" + j.ID,
			URL:        j.HostedURL,
			Title:      j.Text,
			Company:    s.company,
			Location:   j.Categories.Location,
			Text:       text,
			Links:      links,
			Hints:      strings.TrimSpace(j.Workplace + "\n" + j.Categories.Commitment),
			Salary:     salary,
			Remote:     j.Workplace == "remote",
			ApplyURL:   j.ApplyURL,
			PostedAt:   time.UnixMilli(j.CreatedAt).UTC(),
		})
	}
	return out, nil
}

func leverPeriod(interval string) models.Period {
	switch {
	case strings.Contains(interval, "month"):
		return models.PeriodMonth
	case strings.Contains(interval, "hour"):
		return models.PeriodHour
	default:
		return models.PeriodYear
	}
}

// ---------------------------------------------------------------- Ashby

// Ashby is one company's board. Like Lever, the list carries descriptions.
type Ashby struct {
	http   Getter
	org    string
	filter Filter
}

func NewAshby(http Getter, org string, filter Filter) *Ashby {
	return &Ashby{http: http, org: org, filter: filter}
}

func (s *Ashby) Name() string { return "ashby:" + s.org }

func (s *Ashby) Fetch(ctx context.Context) ([]models.Posting, error) {
	var resp struct {
		Jobs []struct {
			ID             string `json:"id"`
			Title          string `json:"title"`
			EmploymentType string `json:"employmentType"`
			Location       string `json:"location"`
			PublishedAt    string `json:"publishedAt"`
			IsListed       bool   `json:"isListed"`
			IsRemote       bool   `json:"isRemote"`
			Workplace      string `json:"workplaceType"`
			JobURL         string `json:"jobUrl"`
			ApplyURL       string `json:"applyUrl"`
			Description    string `json:"descriptionHtml"`
			Compensation   *struct {
				Summary string `json:"compensationTierSummary"`
			} `json:"compensation"`
		} `json:"jobs"`
	}
	u := "https://api.ashbyhq.com/posting-api/job-board/" + url.PathEscape(s.org) + "?includeCompensation=true"
	if err := s.http.GetJSON(ctx, u, &resp); err != nil {
		return nil, fmt.Errorf("ashby.Fetch %s: %w", s.org, err)
	}

	var out []models.Posting
	for _, j := range resp.Jobs {
		title := strings.TrimSpace(j.Title)
		if !j.IsListed || !s.filter.title(title) {
			continue
		}
		text, links := htmltext.Convert(j.Description)
		if !s.filter.keep(title, text) {
			continue
		}
		posted, _ := time.Parse(time.RFC3339, j.PublishedAt)
		var comp string
		if j.Compensation != nil {
			comp = "Salary: " + j.Compensation.Summary
		}
		out = append(out, models.Posting{
			Source:     "ashby",
			SourceName: s.org + " (Ashby)",
			ExternalID: s.org + "/" + j.ID,
			URL:        j.JobURL,
			Title:      title,
			Company:    s.org,
			Location:   j.Location,
			Text:       text,
			Links:      links,
			Hints:      strings.TrimSpace(j.Workplace + "\n" + j.EmploymentType + "\n" + comp),
			// isRemote is also set on hybrid roles; workplaceType is the truth.
			Remote:   strings.EqualFold(j.Workplace, "remote") || (j.Workplace == "" && j.IsRemote),
			ApplyURL: j.ApplyURL,
			PostedAt: posted,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------- Workday

// workdayPages caps one board at 1000 hits. A board with more is reported as
// an error: a truncated list must not make the unread roles look closed.
const workdayPages = 50

// Workday is one employer's career site on Workday. Unlike the other boards
// it searches on the server, so one request returns only roles that mention
// Go; descriptions are fetched once per role and then cached.
type Workday struct {
	http   Poster
	tenant string
	wd     string // data center: "wd1", "wd5"...
	site   string
	query  string
	filter Filter
	cache  *Cache
}

// NewWorkday takes the board as "tenant|wd5|site", the form the seed lists use.
func NewWorkday(http Poster, board, query string, filter Filter, cache *Cache) (*Workday, error) {
	parts := strings.Split(board, "|")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, fmt.Errorf("ats.NewWorkday: board %q is not tenant|wd|site", board)
	}
	return &Workday{http: http, tenant: parts[0], wd: parts[1], site: parts[2], query: query, filter: filter, cache: cache}, nil
}

func (s *Workday) Name() string { return "workday:" + s.tenant + "|" + s.wd + "|" + s.site }

func (s *Workday) base() string {
	return fmt.Sprintf("https://%s.%s.myworkdayjobs.com/wday/cxs/%s/%s", s.tenant, s.wd, s.tenant, s.site)
}

type workdayHit struct {
	Title        string `json:"title"`
	ExternalPath string `json:"externalPath"`
	Locations    string `json:"locationsText"`
}

func (s *Workday) Fetch(ctx context.Context) ([]models.Posting, error) {
	var (
		hits    []workdayHit
		total   int
		pageErr error
	)
	for page := 0; page < workdayPages; page++ {
		var resp struct {
			Total int          `json:"total"`
			Jobs  []workdayHit `json:"jobPostings"`
		}
		payload := map[string]any{"appliedFacets": map[string]any{}, "limit": 20, "offset": page * 20, "searchText": s.query}
		if err := s.http.PostJSON(ctx, s.base()+"/jobs", payload, &resp); err != nil {
			if page == 0 {
				return nil, fmt.Errorf("workday.Fetch %s: %w", s.Name(), err)
			}
			pageErr = fmt.Errorf("page %d: %v", page, err)
			break
		}
		if page == 0 {
			total = resp.Total // Workday reports the total on the first page only
		}
		hits = append(hits, resp.Jobs...)
		if len(resp.Jobs) < 20 || len(hits) >= total {
			break
		}
	}
	if pageErr == nil && len(hits) < total {
		pageErr = fmt.Errorf("read %d of %d hits", len(hits), total)
	}

	feed := s.Name()
	versions := map[string]string{}
	var out []models.Posting
	var firstErr error
	for _, h := range hits {
		if h.ExternalPath == "" || !s.filter.title(h.Title) {
			continue
		}
		id := s.tenant + "/" + s.site + h.ExternalPath
		publicURL := fmt.Sprintf("https://%s.%s.myworkdayjobs.com/%s%s", s.tenant, s.wd, s.site, h.ExternalPath)
		if s.cache.Has(feed, h.ExternalPath, "1") {
			versions[h.ExternalPath] = "1"
			out = append(out, stub("workday", id, publicURL, h.Title))
			continue
		}
		p, err := s.detail(ctx, h, id, publicURL)
		if err != nil {
			if errors.Is(err, httpx.ErrNotFound) {
				continue // the role closed between the search and now
			}
			if firstErr == nil {
				firstErr = roleError(h.ExternalPath, err)
			}
			continue
		}
		versions[h.ExternalPath] = "1"
		if s.filter.keep(p.Title, p.Text) {
			out = append(out, p)
		}
	}
	s.cache.Replace(feed, versions)
	if firstErr == nil {
		firstErr = pageErr
	}
	if firstErr != nil {
		// Not %w: nothing here means the board itself is gone.
		return out, fmt.Errorf("workday.Fetch %s: %v", feed, firstErr)
	}
	return out, nil
}

func (s *Workday) detail(ctx context.Context, h workdayHit, id, publicURL string) (models.Posting, error) {
	var d struct {
		Info struct {
			Title       string `json:"title"`
			Description string `json:"jobDescription"`
			Location    string `json:"location"`
			StartDate   string `json:"startDate"`
			TimeType    string `json:"timeType"`
			RemoteType  string `json:"remoteType"`
			ExternalURL string `json:"externalUrl"`
		} `json:"jobPostingInfo"`
		Org struct {
			Name string `json:"name"`
		} `json:"hiringOrganization"`
	}
	if err := s.http.GetJSON(ctx, s.base()+h.ExternalPath, &d); err != nil {
		return models.Posting{}, err
	}

	text, links := htmltext.Convert(d.Info.Description)
	posted, _ := time.Parse("2006-01-02", d.Info.StartDate)
	company := d.Org.Name
	if company == "" {
		company = s.tenant
	}
	link := d.Info.ExternalURL
	if link == "" {
		link = publicURL
	}
	location := d.Info.Location
	if location == "" {
		location = h.Locations
	}
	title := d.Info.Title
	if title == "" {
		title = h.Title
	}
	return models.Posting{
		Source:     "workday",
		SourceName: company + " (Workday)",
		ExternalID: id,
		URL:        link,
		Title:      title,
		Company:    company,
		Location:   location,
		Text:       text,
		Links:      links,
		Hints:      strings.TrimSpace(d.Info.TimeType + "\n" + d.Info.RemoteType),
		Remote:     strings.Contains(strings.ToLower(d.Info.RemoteType), "remote"),
		ApplyURL:   link,
		PostedAt:   posted,
	}, nil
}
