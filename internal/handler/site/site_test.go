package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/models"
)

func newBuilder(t *testing.T, out string) *Builder {
	t.Helper()
	b, err := New(Config{Title: "Go Jobs", BaseURL: "https://example.org/gojobs", BasePath: "/gojobs", FeedDays: 60, OutDir: out})
	require.NoError(t, err)
	b.now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	return b
}

func sampleJob(id, title string, posted time.Time) models.Job {
	return models.Job{
		ID: id, Slug: "go-" + id, Title: title, Company: "Acme", Lang: "ru",
		Text:     "Пишите @hr_anna или на jobs@acme.io <script>alert(1)</script>",
		Salary:   models.Salary{Min: 300000, Max: 400000, Currency: "RUB", Period: models.PeriodMonth, MonthlyUSDMin: 3659, MonthlyUSDMax: 4878},
		Contacts: []models.Contact{{Kind: models.ContactEmail, Value: "jobs@acme.io"}, {Kind: models.ContactTelegram, Value: "hr_anna"}},
		Sources:  []models.SourceRef{{Source: "telegram", Name: "@rabota_golang", URL: "https://t.me/rabota_golang/1", PostedAt: posted}},
		PostedAt: posted,
	}
}

func TestBuild_Jobs_FeedAndPages(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // the builder only writes to subdirectories of the working dir
	out := filepath.Join(dir, "docs")

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	fresh := sampleJob("a1b2c3", "Senior Go Developer", now.Add(-24*time.Hour))
	old := sampleJob("d4e5f6", "Old Go Developer", now.Add(-90*24*time.Hour))
	closed := sampleJob("aaa111", "Closed Go Developer", now.Add(-24*time.Hour))
	closed.Closed = true

	res, err := newBuilder(t, "docs").Build([]models.Job{fresh, old, closed}, nil, BoardsSummary{}, now)
	require.NoError(t, err)
	assert.Equal(t, Result{Feed: 1, Pages: 3}, res)

	var feed feedJSON
	raw, err := os.ReadFile(filepath.Join(out, "data", "jobs.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &feed))
	require.Len(t, feed.Jobs, 1)
	assert.Equal(t, "300 000 – 400 000 ₽/мес", feed.Jobs[0].Salary)
	assert.Equal(t, []string{"email", "telegram"}, feed.Jobs[0].Contacts)

	page, err := os.ReadFile(filepath.Join(out, "jobs", "go-a1b2c3", "index.html"))
	require.NoError(t, err)
	html := string(page)
	assert.Contains(t, html, `href="mailto:jobs@acme.io?subject=`)
	assert.Contains(t, html, `href="https://t.me/hr_anna"`)
	assert.Contains(t, html, "&lt;script&gt;", "post text is escaped")
	assert.NotContains(t, html, "<script>alert")

	oldPage, err := os.ReadFile(filepath.Join(out, "jobs", "go-d4e5f6", "index.html"))
	require.NoError(t, err)
	assert.Contains(t, string(oldPage), `name="robots" content="noindex"`)

	sitemap, err := os.ReadFile(filepath.Join(out, "sitemap.xml"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(sitemap), "/jobs/"), "only feed jobs are in the sitemap")
}

func TestBuild_ForeignDir_Refused(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	out := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(out, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(out, "important.md"), []byte("mine"), 0o644))

	_, err := newBuilder(t, "docs").Build(nil, nil, BoardsSummary{}, time.Now())

	require.Error(t, err)
	assert.FileExists(t, filepath.Join(out, "important.md"))
}

func TestCheckOutDir_DangerousPaths_Refused(t *testing.T) {
	for _, p := range []string{".", "", "/tmp/docs", "../docs"} {
		assert.Error(t, checkOutDir(filepath.Clean(p)), p)
	}
}

func TestLinkify_Text_LinksEscaped(t *testing.T) {
	got := string(linkify("Пишите (@hr_anna), a@b.io или https://acme.io/jobs?x=1&y=2. <b>"))
	assert.Contains(t, got, `(<a href="https://t.me/hr_anna" target="_blank" rel="noopener">@hr_anna</a>)`)
	assert.Contains(t, got, `<a href="mailto:a@b.io">a@b.io</a>`)
	assert.Contains(t, got, `href="https://acme.io/jobs?x=1&amp;y=2"`)
	assert.Contains(t, got, "&lt;b&gt;")
}

func TestPlural_Numbers_RussianForms(t *testing.T) {
	for n, want := range map[int]string{1: "вакансия", 2: "вакансии", 5: "вакансий", 11: "вакансий", 21: "вакансия", 283: "вакансии"} {
		assert.Equal(t, want, plural(n, "вакансия", "вакансии", "вакансий"), n)
	}
}
