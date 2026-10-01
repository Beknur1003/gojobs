package ats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/repository/httpx"
)

// fakeHTTP serves canned JSON by URL and records what was requested.
type fakeHTTP struct {
	get   map[string]string
	post  map[string]string
	calls []string
	// postFailFrom makes the n-th POST (1-based) and later ones fail; 0: never.
	postFailFrom int
	posts        int
}

func (f *fakeHTTP) GetJSON(_ context.Context, u string, dst any) error {
	f.calls = append(f.calls, "GET "+u)
	body, ok := f.get[u]
	if !ok {
		return assert.AnError
	}
	if body == "404" {
		return fmt.Errorf("httpx GET %s: %w", u, httpx.ErrNotFound)
	}
	return json.Unmarshal([]byte(body), dst)
}

func (f *fakeHTTP) PostJSON(_ context.Context, u string, _, dst any) error {
	f.calls = append(f.calls, "POST "+u)
	f.posts++
	if f.postFailFrom > 0 && f.posts >= f.postFailFrom {
		return errors.New("status 503")
	}
	body, ok := f.post[u]
	if !ok {
		return assert.AnError
	}
	return json.Unmarshal([]byte(body), dst)
}

var goOnlyTitles = Filter{
	Title: func(t string) bool { return strings.Contains(t, "Engineer") },
	Keep:  func(_, text string) bool { return strings.Contains(text, "Go") },
}

const ghBase = "https://boards-api.greenhouse.io/v1/boards/acme/jobs"

func TestGreenhouse_LightListThenDetails_CachedOnSecondRun(t *testing.T) {
	http := &fakeHTTP{get: map[string]string{
		ghBase: `{"jobs":[
			{"id":1,"title":"Backend Engineer","updated_at":"v1","absolute_url":"https://x/1","company_name":"Acme"},
			{"id":2,"title":"Platform Engineer","updated_at":"v1","absolute_url":"https://x/2","company_name":"Acme"},
			{"id":3,"title":"Account Executive","updated_at":"v1","absolute_url":"https://x/3"}]}`,
		ghBase + "/1": `{"id":1,"title":"Backend Engineer","updated_at":"v1","absolute_url":"https://x/1","content":"&lt;p&gt;We write Go&lt;/p&gt;","first_published":"2026-09-01T00:00:00Z"}`,
		ghBase + "/2": `{"id":2,"title":"Platform Engineer","updated_at":"v1","absolute_url":"https://x/2","content":"&lt;p&gt;Rust only&lt;/p&gt;"}`,
	}}
	cache, err := LoadCache(filepath.Join(t.TempDir(), "ats.json"), "r1")
	require.NoError(t, err)
	src := NewGreenhouse(http, "acme", goOnlyTitles, cache)

	first, err := src.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, first, 1, "role 2 is fetched but not Go; role 3 is never fetched")
	assert.Equal(t, "acme/1", first[0].ExternalID)
	assert.Equal(t, "We write Go", first[0].Text)
	assert.Equal(t, "Acme", first[0].Company, "the light list's company fills a detail without one")
	assert.NotContains(t, http.calls, "GET "+ghBase+"/3")

	http.calls = nil
	second, err := src.Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"GET " + ghBase}, http.calls, "no detail downloads for unchanged roles")
	require.Len(t, second, 2)
	for _, p := range second {
		assert.True(t, p.Stub)
	}
}

func TestGreenhouse_ManyNewRoles_OneBulkDownload(t *testing.T) {
	var list, full []string
	for i := 1; i <= greenhouseBulk; i++ {
		list = append(list, `{"id":`+strconv.Itoa(i)+`,"title":"Engineer","updated_at":"v1"}`)
		full = append(full, `{"id":`+strconv.Itoa(i)+`,"title":"Engineer","updated_at":"v1","content":"Go"}`)
	}
	http := &fakeHTTP{get: map[string]string{
		ghBase:                   `{"jobs":[` + strings.Join(list, ",") + `]}`,
		ghBase + "?content=true": `{"jobs":[` + strings.Join(full, ",") + `]}`,
	}}

	posts, err := NewGreenhouse(http, "acme", goOnlyTitles, nil).Fetch(context.Background())

	require.NoError(t, err)
	assert.Len(t, posts, greenhouseBulk)
	assert.Len(t, http.calls, 2)
}

func TestWorkday_SearchThenDetail_Posting(t *testing.T) {
	base := "https://acme.wd5.myworkdayjobs.com/wday/cxs/acme/careers"
	http := &fakeHTTP{
		post: map[string]string{base + "/jobs": `{"total":1,"jobPostings":[{"title":"Senior Engineer","externalPath":"/job/Remote/Senior-Engineer_R1","locationsText":"Remote"}]}`},
		get: map[string]string{base + "/job/Remote/Senior-Engineer_R1": `{"jobPostingInfo":{"title":"Senior Engineer","jobDescription":"<p>Golang services. Go!</p>",
			"startDate":"2026-09-20","remoteType":"Fully Remote","externalUrl":"https://acme.wd5.myworkdayjobs.com/careers/job/Remote/Senior-Engineer_R1"},
			"hiringOrganization":{"name":"Acme Corp"}}`},
	}
	src, err := NewWorkday(http, "acme|wd5|careers", "golang", goOnlyTitles, nil)
	require.NoError(t, err)
	assert.Equal(t, "workday:acme|wd5|careers", src.Name())

	posts, err := src.Fetch(context.Background())

	require.NoError(t, err)
	require.Len(t, posts, 1)
	p := posts[0]
	assert.Equal(t, "acme/careers/job/Remote/Senior-Engineer_R1", p.ExternalID)
	assert.Equal(t, "Acme Corp", p.Company)
	assert.True(t, p.Remote)
	assert.Equal(t, "2026-09-20", p.PostedAt.Format("2006-01-02"))
}

func TestNewWorkday_BadBoard_Error(t *testing.T) {
	_, err := NewWorkday(&fakeHTTP{}, "acme|wd5", "golang", Filter{}, nil)
	assert.Error(t, err)
}

func TestGreenhouse_RoleGoneBeforeDetail_BoardStillHealthy(t *testing.T) {
	http := &fakeHTTP{get: map[string]string{
		ghBase:        `{"jobs":[{"id":1,"title":"Backend Engineer","updated_at":"v1"},{"id":2,"title":"Platform Engineer","updated_at":"v1"}]}`,
		ghBase + "/1": `{"id":1,"title":"Backend Engineer","updated_at":"v1","content":"Go and more Go"}`,
		ghBase + "/2": "404",
	}}

	posts, err := NewGreenhouse(http, "acme", goOnlyTitles, nil).Fetch(context.Background())

	require.NoError(t, err, "one closed role is not a failure of the board")
	assert.Len(t, posts, 1)
}

func TestGreenhouse_RoleFails_ErrorIsNotBoardGone(t *testing.T) {
	http := &fakeHTTP{get: map[string]string{
		ghBase: `{"jobs":[{"id":1,"title":"Backend Engineer","updated_at":"v1"}]}`,
	}}

	_, err := NewGreenhouse(http, "acme", goOnlyTitles, nil).Fetch(context.Background())

	require.Error(t, err)
	assert.False(t, errors.Is(err, httpx.ErrNotFound))
}

func TestWorkday_TruncatedSearch_Error(t *testing.T) {
	base := "https://acme.wd5.myworkdayjobs.com/wday/cxs/acme/careers"
	var hits []string
	for i := 0; i < 20; i++ {
		hits = append(hits, `{"title":"Engineer","externalPath":"/job/x/E_R`+strconv.Itoa(i)+`"}`)
	}
	http := &fakeHTTP{
		post:         map[string]string{base + "/jobs": `{"total":35,"jobPostings":[` + strings.Join(hits, ",") + `]}`},
		get:          map[string]string{},
		postFailFrom: 2, // the second page fails after retries
	}
	src, err := NewWorkday(http, "acme|wd5|careers", "golang", Filter{Title: func(string) bool { return false }}, nil)
	require.NoError(t, err)

	_, err = src.Fetch(context.Background())

	require.Error(t, err, "20 of 35 hits read: unread roles must not look closed")
	assert.False(t, errors.Is(err, httpx.ErrNotFound))
}

func TestLever_FractionalSalary_BoardStillReads(t *testing.T) {
	http := &fakeHTTP{get: map[string]string{
		"https://api.lever.co/v0/postings/acme?mode=json": `[
			{"id":"1","text":"Support Agent","salaryRange":{"min":32.34,"max":40.5,"currency":"usd","interval":"per-hour-wage"}},
			{"id":"2","text":"Backend Engineer","description":"We use Go. Go everywhere.","salaryRange":{"min":150000.0,"max":180000,"currency":"usd","interval":"per-year-salary"}}]`,
	}}

	posts, err := NewLever(http, "acme", goOnlyTitles).Fetch(context.Background())

	require.NoError(t, err)
	require.Len(t, posts, 1)
	assert.Equal(t, 150000, posts[0].Salary.Min)
}

func TestLoadCache_OtherRules_StartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ats.json")
	c, err := LoadCache(path, "r1")
	require.NoError(t, err)
	c.Replace("greenhouse:acme", map[string]string{"1": "v1"})
	require.NoError(t, c.Save())

	same, err := LoadCache(path, "r1")
	require.NoError(t, err)
	assert.True(t, same.Has("greenhouse:acme", "1", "v1"))

	newer, err := LoadCache(path, "r2")
	require.NoError(t, err)
	assert.False(t, newer.Has("greenhouse:acme", "1", "v1"), "roles judged by old rules are judged again")
}
