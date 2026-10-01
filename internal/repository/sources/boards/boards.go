// Package boards adapts the free remote job boards that publish open JSON or
// RSS feeds. Each one asks for the same thing in return: link to the original
// listing and name the board as the source. The site does both on every card.
package boards

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/sources/htmltext"
)

type Getter interface {
	Get(ctx context.Context, rawURL string) ([]byte, error)
	GetJSON(ctx context.Context, rawURL string, dst any) error
}

// flexInt accepts 120000, "120000" and null: the boards are not consistent.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*f = 0
		return nil //nolint:nilerr // a junk salary must not drop the whole feed
	}
	*f = flexInt(v)
	return nil
}

// flexStrings accepts both "a" and ["a","b"].
type flexStrings []string

func (f *flexStrings) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '[' {
		var list []string
		if err := json.Unmarshal(b, &list); err != nil {
			return err
		}
		*f = list
		return nil
	}
	var one string
	if err := json.Unmarshal(b, &one); err != nil {
		*f = nil
		return nil //nolint:nilerr // unknown shape: treat as absent
	}
	if one != "" {
		*f = []string{one}
	}
	return nil
}

func htmlToText(s string) (string, []string) {
	return htmltext.Convert(s)
}

func hints(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------- Remote OK

type RemoteOK struct{ http Getter }

func NewRemoteOK(http Getter) *RemoteOK { return &RemoteOK{http: http} }

func (s *RemoteOK) Name() string { return "remoteok" }

type remoteOKJob struct {
	ID          string   `json:"id"`
	Date        string   `json:"date"`
	Company     string   `json:"company"`
	Position    string   `json:"position"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
	Location    string   `json:"location"`
	ApplyURL    string   `json:"apply_url"`
	SalaryMin   flexInt  `json:"salary_min"`
	SalaryMax   flexInt  `json:"salary_max"`
	URL         string   `json:"url"`
}

func (s *RemoteOK) Fetch(ctx context.Context) ([]models.Posting, error) {
	// The first element is the API's legal notice, not a job.
	var raw []json.RawMessage
	if err := s.http.GetJSON(ctx, "https://remoteok.com/api?tag=golang", &raw); err != nil {
		return nil, fmt.Errorf("remoteok.Fetch: %w", err)
	}

	var out []models.Posting
	for _, r := range raw {
		var j remoteOKJob
		if err := json.Unmarshal(r, &j); err != nil || j.ID == "" || j.Position == "" {
			continue
		}
		text, links := htmlToText(html.UnescapeString(j.Description))
		posted, _ := time.Parse(time.RFC3339, j.Date)
		out = append(out, models.Posting{
			Source:     "remoteok",
			SourceName: "Remote OK",
			ExternalID: j.ID,
			URL:        j.URL,
			Title:      j.Position,
			Company:    j.Company,
			Location:   j.Location,
			Text:       text,
			Links:      links,
			// Tags are not passed on: Remote OK tags unrelated roles ("Field
			// Representative") with golang, so the text has to show Go itself.
			Salary:   models.Salary{Min: int(j.SalaryMin), Max: int(j.SalaryMax), Currency: "USD", Period: models.PeriodYear},
			Remote:   true,
			ApplyURL: j.URL, // their terms: send applicants through Remote OK
			PostedAt: posted,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------- Remotive

type Remotive struct{ http Getter }

func NewRemotive(http Getter) *Remotive { return &Remotive{http: http} }

func (s *Remotive) Name() string { return "remotive" }

func (s *Remotive) Fetch(ctx context.Context) ([]models.Posting, error) {
	var resp struct {
		Jobs []struct {
			ID          int      `json:"id"`
			URL         string   `json:"url"`
			Title       string   `json:"title"`
			Company     string   `json:"company_name"`
			Tags        []string `json:"tags"`
			JobType     string   `json:"job_type"`
			Published   string   `json:"publication_date"`
			Location    string   `json:"candidate_required_location"`
			Salary      string   `json:"salary"`
			Description string   `json:"description"`
		} `json:"jobs"`
	}
	// Remotive asks for at most a few calls a day; this runs once.
	if err := s.http.GetJSON(ctx, "https://remotive.com/api/remote-jobs?search=golang", &resp); err != nil {
		return nil, fmt.Errorf("remotive.Fetch: %w", err)
	}

	out := make([]models.Posting, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		text, links := htmlToText(j.Description)
		posted, _ := time.Parse("2006-01-02T15:04:05", j.Published)
		out = append(out, models.Posting{
			Source:     "remotive",
			SourceName: "Remotive",
			ExternalID: strconv.Itoa(j.ID),
			URL:        j.URL,
			Title:      j.Title,
			Company:    j.Company,
			Location:   j.Location,
			Text:       text,
			Links:      links,
			Tags:       j.Tags,
			Hints:      hints("Salary: "+j.Salary, strings.ReplaceAll(j.JobType, "_", " ")),
			Remote:     true,
			ApplyURL:   j.URL,
			PostedAt:   posted,
			// Not GoOnly: the search is fuzzy and returns e.g. content reviewers.
		})
	}
	return out, nil
}

// ---------------------------------------------------------------- Himalayas

type Himalayas struct{ http Getter }

func NewHimalayas(http Getter) *Himalayas { return &Himalayas{http: http} }

func (s *Himalayas) Name() string { return "himalayas" }

type himalayasPage struct {
	Offset     int `json:"offset"`
	TotalCount int `json:"totalCount"`
	Jobs       []struct {
		Title          string   `json:"title"`
		Company        string   `json:"companyName"`
		EmploymentType string   `json:"employmentType"`
		MinSalary      flexInt  `json:"minSalary"`
		MaxSalary      flexInt  `json:"maxSalary"`
		SalaryPeriod   string   `json:"salaryPeriod"`
		Currency       string   `json:"currency"`
		Seniority      []string `json:"seniority"`
		Locations      []string `json:"locationRestrictions"`
		Categories     []string `json:"categories"`
		Description    string   `json:"description"`
		PubDate        flexInt  `json:"pubDate"`
		ExpiryDate     flexInt  `json:"expiryDate"`
		Link           string   `json:"applicationLink"`
		GUID           string   `json:"guid"`
	} `json:"jobs"`
}

// himalayasMaxPages caps one run. The search returns ~200 Go jobs, 20 a page.
const himalayasMaxPages = 15

// Fetch pages through the search endpoint, which takes ?page=N (1-based);
// the cursor described in the API notes belongs to the full feed, not search.
func (s *Himalayas) Fetch(ctx context.Context) ([]models.Posting, error) {
	var out []models.Posting
	now := time.Now()
	for page := 1; page <= himalayasMaxPages; page++ {
		var resp himalayasPage
		u := "https://himalayas.app/jobs/api/search?q=golang&page=" + strconv.Itoa(page)
		if err := s.http.GetJSON(ctx, u, &resp); err != nil {
			if len(out) > 0 {
				return out, nil // keep what the earlier pages gave
			}
			return nil, fmt.Errorf("himalayas.Fetch: %w", err)
		}

		for _, j := range resp.Jobs {
			if j.ExpiryDate > 0 && time.Unix(int64(j.ExpiryDate), 0).Before(now) {
				continue
			}
			text, links := htmlToText(j.Description)
			period := models.PeriodYear
			if strings.HasPrefix(j.SalaryPeriod, "month") {
				period = models.PeriodMonth
			} else if strings.HasPrefix(j.SalaryPeriod, "hour") {
				period = models.PeriodHour
			}
			out = append(out, models.Posting{
				Source:     "himalayas",
				SourceName: "Himalayas",
				ExternalID: j.GUID,
				URL:        j.GUID,
				Title:      j.Title,
				Company:    j.Company,
				Location:   strings.Join(j.Locations, ", "),
				Text:       text,
				Links:      links,
				Tags:       j.Categories,
				Hints:      hints(strings.Join(j.Seniority, ", "), j.EmploymentType),
				Salary:     models.Salary{Min: int(j.MinSalary), Max: int(j.MaxSalary), Currency: strings.ToUpper(j.Currency), Period: period},
				Remote:     true,
				ApplyURL:   j.Link,
				PostedAt:   time.Unix(int64(j.PubDate), 0).UTC(),
				GoOnly:     true,
			})
		}

		if len(resp.Jobs) == 0 || resp.Offset+len(resp.Jobs) >= resp.TotalCount {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- Jobicy

type Jobicy struct{ http Getter }

func NewJobicy(http Getter) *Jobicy { return &Jobicy{http: http} }

func (s *Jobicy) Name() string { return "jobicy" }

func (s *Jobicy) Fetch(ctx context.Context) ([]models.Posting, error) {
	var resp struct {
		Jobs []struct {
			ID          flexInt     `json:"id"`
			URL         string      `json:"url"`
			Title       string      `json:"jobTitle"`
			Company     string      `json:"companyName"`
			Industry    flexStrings `json:"jobIndustry"`
			JobType     flexStrings `json:"jobType"`
			Geo         string      `json:"jobGeo"`
			Level       string      `json:"jobLevel"`
			Description string      `json:"jobDescription"`
			PubDate     string      `json:"pubDate"`
			SalaryMin   flexInt     `json:"annualSalaryMin"`
			SalaryMax   flexInt     `json:"annualSalaryMax"`
			Currency    string      `json:"salaryCurrency"`
		} `json:"jobs"`
	}
	if err := s.http.GetJSON(ctx, "https://jobicy.com/api/v2/remote-jobs?tag=golang&count=100", &resp); err != nil {
		return nil, fmt.Errorf("jobicy.Fetch: %w", err)
	}

	out := make([]models.Posting, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		text, links := htmlToText(j.Description)
		posted, _ := time.Parse(time.RFC3339, j.PubDate)
		level := j.Level
		if strings.EqualFold(level, "any") {
			level = ""
		}
		out = append(out, models.Posting{
			Source:     "jobicy",
			SourceName: "Jobicy",
			ExternalID: strconv.Itoa(int(j.ID)),
			URL:        j.URL,
			Title:      html.UnescapeString(j.Title),
			Company:    html.UnescapeString(j.Company),
			Location:   j.Geo,
			Text:       text,
			Links:      links,
			Tags:       j.Industry,
			Hints:      hints(level, strings.Join(j.JobType, ", ")),
			Salary:     models.Salary{Min: int(j.SalaryMin), Max: int(j.SalaryMax), Currency: strings.ToUpper(j.Currency), Period: models.PeriodYear},
			Remote:     true,
			ApplyURL:   j.URL, // their terms: apply buttons go to the original listing
			PostedAt:   posted,
			GoOnly:     true,
		})
	}
	return out, nil
}
