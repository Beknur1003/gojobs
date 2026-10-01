// Package store keeps the board's state in one JSON file inside the repo.
// A file instead of a database: it diffs in git, needs no server, and makes a
// later move from the laptop to a CI cron a matter of copying the repo.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
)

const version = 1

// State is everything one run hands to the next.
type State struct {
	UpdatedAt time.Time
	Feeds     map[string]models.FeedStatus
	Jobs      []models.Job
}

func Load(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Feeds: map[string]models.FeedStatus{}}, nil // first run
	}
	if err != nil {
		return State{}, fmt.Errorf("store.Load: %w", err)
	}

	var f fileDTO
	if err := json.Unmarshal(raw, &f); err != nil {
		return State{}, fmt.Errorf("store.Load: parse %s: %w", path, err)
	}
	if f.Version != version {
		return State{}, fmt.Errorf("store.Load: %s has version %d, this build reads %d", path, f.Version, version)
	}

	st := State{UpdatedAt: f.UpdatedAt, Feeds: map[string]models.FeedStatus{}}
	for name, fs := range f.Feeds {
		st.Feeds[name] = models.FeedStatus{LastOK: fs.LastOK, LastRun: fs.LastRun, Count: fs.Count, Error: fs.Error}
	}
	for _, j := range f.Jobs {
		st.Jobs = append(st.Jobs, j.toModel())
	}
	return st, nil
}

// Save writes atomically: a crash mid-write must not lose the history.
func Save(path string, st State) error {
	f := fileDTO{Version: version, UpdatedAt: st.UpdatedAt, Feeds: map[string]feedDTO{}}
	for name, fs := range st.Feeds {
		f.Feeds[name] = feedDTO{LastOK: fs.LastOK, LastRun: fs.LastRun, Count: fs.Count, Error: fs.Error}
	}

	jobs := append([]models.Job(nil), st.Jobs...)
	sort.SliceStable(jobs, func(a, b int) bool { return jobs[a].PostedAt.After(jobs[b].PostedAt) })
	for _, j := range jobs {
		f.Jobs = append(f.Jobs, jobFromModel(j))
	}

	raw, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return fmt.Errorf("store.Save: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("store.Save: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("store.Save: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("store.Save: %w", err)
	}
	return nil
}

type fileDTO struct {
	Version   int                `json:"version"`
	UpdatedAt time.Time          `json:"updated_at"`
	Feeds     map[string]feedDTO `json:"feeds"`
	Jobs      []jobDTO           `json:"jobs"`
}

type feedDTO struct {
	LastOK  time.Time `json:"last_ok,omitzero"`
	LastRun time.Time `json:"last_run"`
	Count   int       `json:"count"`
	Error   string    `json:"error,omitempty"`
}

type jobDTO struct {
	ID         string       `json:"id"`
	Slug       string       `json:"slug"`
	Title      string       `json:"title"`
	Company    string       `json:"company,omitempty"`
	Text       string       `json:"text"`
	Summary    string       `json:"summary,omitempty"`
	Salary     *salaryDTO   `json:"salary,omitempty"`
	Formats    []string     `json:"formats,omitempty"`
	Grades     []string     `json:"grades,omitempty"`
	Employment []string     `json:"employment,omitempty"`
	English    string       `json:"english,omitempty"`
	Stack      []string     `json:"stack,omitempty"`
	Location   string       `json:"location,omitempty"`
	Relocation bool         `json:"relocation,omitempty"`
	Lang       string       `json:"lang"`
	Contacts   []contactDTO `json:"contacts,omitempty"`
	ApplyURL   string       `json:"apply_url,omitempty"`
	Sources    []sourceDTO  `json:"sources"`
	PostedAt   time.Time    `json:"posted_at"`
	FirstSeen  time.Time    `json:"first_seen"`
	LastSeen   time.Time    `json:"last_seen"`
}

type salaryDTO struct {
	Min           int    `json:"min,omitempty"`
	Max           int    `json:"max,omitempty"`
	Currency      string `json:"currency"`
	Period        string `json:"period"`
	MonthlyUSDMin int    `json:"usd_month_min,omitempty"`
	MonthlyUSDMax int    `json:"usd_month_max,omitempty"`
}

type contactDTO struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type sourceDTO struct {
	Source     string    `json:"source"`
	Feed       string    `json:"feed"`
	Name       string    `json:"name"`
	ExternalID string    `json:"external_id"`
	URL        string    `json:"url"`
	PostedAt   time.Time `json:"posted_at"`
	LastSeen   time.Time `json:"last_seen"`
}

func jobFromModel(j models.Job) jobDTO {
	d := jobDTO{
		ID: j.ID, Slug: j.Slug, Title: j.Title, Company: j.Company, Text: j.Text, Summary: j.Summary,
		Formats: toStrings(j.Formats), Grades: toStrings(j.Grades), Employment: toStrings(j.Employment),
		English: j.English, Stack: j.Stack, Location: j.Location, Relocation: j.Relocation, Lang: j.Lang,
		ApplyURL: j.ApplyURL, PostedAt: j.PostedAt, FirstSeen: j.FirstSeen, LastSeen: j.LastSeen,
	}
	if j.Salary.Known() {
		d.Salary = &salaryDTO{
			Min: j.Salary.Min, Max: j.Salary.Max, Currency: j.Salary.Currency, Period: string(j.Salary.Period),
			MonthlyUSDMin: j.Salary.MonthlyUSDMin, MonthlyUSDMax: j.Salary.MonthlyUSDMax,
		}
	}
	for _, c := range j.Contacts {
		d.Contacts = append(d.Contacts, contactDTO{Kind: string(c.Kind), Value: c.Value})
	}
	for _, s := range j.Sources {
		d.Sources = append(d.Sources, sourceDTO{
			Source: s.Source, Feed: s.Feed, Name: s.Name, ExternalID: s.ExternalID, URL: s.URL, PostedAt: s.PostedAt, LastSeen: s.LastSeen,
		})
	}
	return d
}

func (d jobDTO) toModel() models.Job {
	j := models.Job{
		ID: d.ID, Slug: d.Slug, Title: d.Title, Company: d.Company, Text: d.Text, Summary: d.Summary,
		Formats: fromStrings[models.WorkFormat](d.Formats), Grades: fromStrings[models.Grade](d.Grades),
		Employment: fromStrings[models.Employment](d.Employment),
		English:    d.English, Stack: d.Stack, Location: d.Location, Relocation: d.Relocation, Lang: d.Lang,
		ApplyURL: d.ApplyURL, PostedAt: d.PostedAt, FirstSeen: d.FirstSeen, LastSeen: d.LastSeen,
	}
	if s := d.Salary; s != nil {
		j.Salary = models.Salary{
			Min: s.Min, Max: s.Max, Currency: s.Currency, Period: models.Period(s.Period),
			MonthlyUSDMin: s.MonthlyUSDMin, MonthlyUSDMax: s.MonthlyUSDMax,
		}
	}
	for _, c := range d.Contacts {
		j.Contacts = append(j.Contacts, models.Contact{Kind: models.ContactKind(c.Kind), Value: c.Value})
	}
	for _, s := range d.Sources {
		j.Sources = append(j.Sources, models.SourceRef{
			Source: s.Source, Feed: s.Feed, Name: s.Name, ExternalID: s.ExternalID, URL: s.URL, PostedAt: s.PostedAt, LastSeen: s.LastSeen,
		})
	}
	return j
}

func toStrings[T ~string](in []T) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, string(v))
	}
	return out
}

func fromStrings[T ~string](in []string) []T {
	if len(in) == 0 {
		return nil // what Normalize produces, so a reload compares equal
	}
	out := make([]T, 0, len(in))
	for _, v := range in {
		out = append(out, T(v))
	}
	return out
}
