// Package ats reads company career pages hosted on Greenhouse, Lever and
// Ashby through their public job-board APIs. These are vacancies straight from
// the employer; a board lists every role, so rules keep only the Go ones.
package ats

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/sources/htmltext"
)

type Getter interface {
	GetJSON(ctx context.Context, rawURL string, dst any) error
}

// ---------------------------------------------------------------- Greenhouse

// Greenhouse is one company's board; each board is its own feed.
type Greenhouse struct {
	http  Getter
	board string
}

func NewGreenhouse(http Getter, board string) *Greenhouse {
	return &Greenhouse{http: http, board: board}
}

func (s *Greenhouse) Name() string { return "greenhouse:" + s.board }

func (s *Greenhouse) Fetch(ctx context.Context) ([]models.Posting, error) {
	posts, err := s.fetchBoard(ctx, s.board)
	if err != nil {
		return nil, fmt.Errorf("greenhouse.Fetch %s: %w", s.board, err)
	}
	return posts, nil
}

func (s *Greenhouse) fetchBoard(ctx context.Context, board string) ([]models.Posting, error) {
	var resp struct {
		Jobs []struct {
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
		} `json:"jobs"`
	}
	u := "https://boards-api.greenhouse.io/v1/boards/" + url.PathEscape(board) + "/jobs?content=true"
	if err := s.http.GetJSON(ctx, u, &resp); err != nil {
		return nil, err
	}

	out := make([]models.Posting, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		text, links := htmltext.Convert(html.UnescapeString(j.Content))
		posted, err := time.Parse(time.RFC3339, j.FirstPublished)
		if err != nil {
			posted, _ = time.Parse(time.RFC3339, j.UpdatedAt)
		}
		company := j.Company
		if company == "" {
			company = board
		}
		out = append(out, models.Posting{
			Source:     "greenhouse",
			SourceName: company + " (Greenhouse)",
			ExternalID: fmt.Sprintf("%s/%d", board, j.ID),
			URL:        j.URL,
			Title:      j.Title,
			Company:    company,
			Location:   j.Location.Name,
			Text:       text,
			Links:      links,
			ApplyURL:   j.URL,
			PostedAt:   posted,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------- Lever

// Lever is one company's board; each board is its own feed.
type Lever struct {
	http  Getter
	board string
}

func NewLever(http Getter, board string) *Lever {
	return &Lever{http: http, board: board}
}

func (s *Lever) Name() string { return "lever:" + s.board }

func (s *Lever) Fetch(ctx context.Context) ([]models.Posting, error) {
	posts, err := s.fetchBoard(ctx, s.board)
	if err != nil {
		return nil, fmt.Errorf("lever.Fetch %s: %w", s.board, err)
	}
	return posts, nil
}

func (s *Lever) fetchBoard(ctx context.Context, company string) ([]models.Posting, error) {
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
			Min      int    `json:"min"`
			Max      int    `json:"max"`
			Currency string `json:"currency"`
			Interval string `json:"interval"`
		} `json:"salaryRange"`
	}
	u := "https://api.lever.co/v0/postings/" + url.PathEscape(company) + "?mode=json"
	if err := s.http.GetJSON(ctx, u, &jobs); err != nil {
		return nil, err
	}

	out := make([]models.Posting, 0, len(jobs))
	for _, j := range jobs {
		var body strings.Builder
		body.WriteString(j.Description)
		for _, l := range j.Lists {
			body.WriteString("<h3>" + l.Text + "</h3><ul>" + l.Content + "</ul>")
		}
		body.WriteString(j.Additional)
		text, links := htmltext.Convert(body.String())

		var salary models.Salary
		if r := j.SalaryRange; r != nil {
			salary = models.Salary{Min: r.Min, Max: r.Max, Currency: strings.ToUpper(r.Currency), Period: leverPeriod(r.Interval)}
		}
		out = append(out, models.Posting{
			Source:     "lever",
			SourceName: company + " (Lever)",
			ExternalID: company + "/" + j.ID,
			URL:        j.HostedURL,
			Title:      j.Text,
			Company:    company,
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

// Ashby is one company's board; each board is its own feed.
type Ashby struct {
	http  Getter
	board string
}

func NewAshby(http Getter, board string) *Ashby {
	return &Ashby{http: http, board: board}
}

func (s *Ashby) Name() string { return "ashby:" + s.board }

func (s *Ashby) Fetch(ctx context.Context) ([]models.Posting, error) {
	posts, err := s.fetchBoard(ctx, s.board)
	if err != nil {
		return nil, fmt.Errorf("ashby.Fetch %s: %w", s.board, err)
	}
	return posts, nil
}

func (s *Ashby) fetchBoard(ctx context.Context, org string) ([]models.Posting, error) {
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
	u := "https://api.ashbyhq.com/posting-api/job-board/" + url.PathEscape(org) + "?includeCompensation=true"
	if err := s.http.GetJSON(ctx, u, &resp); err != nil {
		return nil, err
	}

	out := make([]models.Posting, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		if !j.IsListed {
			continue
		}
		text, links := htmltext.Convert(j.Description)
		posted, _ := time.Parse(time.RFC3339, j.PublishedAt)
		var comp string
		if j.Compensation != nil {
			comp = "Salary: " + j.Compensation.Summary
		}
		out = append(out, models.Posting{
			Source:     "ashby",
			SourceName: org + " (Ashby)",
			ExternalID: org + "/" + j.ID,
			URL:        j.JobURL,
			Title:      strings.TrimSpace(j.Title),
			Company:    org,
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
