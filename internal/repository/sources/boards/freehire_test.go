package boards

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/models"
)

// pagedGetter serves freehire pages by offset and fails where told to.
type pagedGetter struct {
	pages  map[string]string // offset -> body
	failAt string
	calls  []string
}

func (g *pagedGetter) Get(context.Context, string) ([]byte, error) { return nil, errors.New("unused") }

func (g *pagedGetter) GetJSON(_ context.Context, u string, dst any) error {
	g.calls = append(g.calls, u)
	offset := u[strings.Index(u, "offset=")+len("offset="):]
	offset, _, _ = strings.Cut(offset, "&")
	if offset == g.failAt {
		return errors.New("status 502")
	}
	body, ok := g.pages[offset]
	if !ok {
		return errors.New("unexpected offset " + offset)
	}
	return json.Unmarshal([]byte(body), dst)
}

func freehireJobJSON(slug, source, extra string) string {
	return `{"public_slug":"` + slug + `","source":"` + source + `","url":"https://job-boards.greenhouse.io/acme/jobs/1?utm_source=freehire.me",
		"title":"Senior Go Engineer","company":"Acme","location":"Remote","description":"<p>We write Go. Go is our language.</p>",
		"work_mode":"remote","posted_at":"2026-09-20T10:00:00Z","closed_at":null` + extra + `}`
}

func TestFreehire_Page_FiltersAndMaps(t *testing.T) {
	page := `{"meta":{"total":5},"data":[` + strings.Join([]string{
		freehireJobJSON("ok-1", "greenhouse", `,"enrichment":{"salary_min":120000,"salary_max":150000,"salary_currency":"usd","salary_period":"year","seniority":"senior","relocation":"yes","english_level":"B2"}`),
		freehireJobJSON("paid-redirect", "whatjobs-sg", ""),
		freehireJobJSON("direct-already", "djinni", ""),
		freehireJobJSON("fake-fresh", "lever", `,"reality":{"class":"fresh","fake_freshness":true}`),
		freehireJobJSON("hh-1", "hh", `,"enrichment":{"relocation":false}`),
	}, ",") + `]}`
	g := &pagedGetter{pages: map[string]string{"0": page}}

	posts, err := NewFreehire(g).Fetch(context.Background())

	require.NoError(t, err)
	require.Len(t, posts, 3)
	assert.Len(t, g.calls, 1, "a short page ends the read")
	stub := posts[1]
	assert.True(t, stub.Stub, "a reposted role keeps a known job alive instead of vanishing")
	assert.Equal(t, "fake-fresh", stub.ExternalID)
	posts = append(posts[:1], posts[2:]...)
	assert.Contains(t, g.calls[0], "/api/v1/agent/jobs/search?", "the agent endpoint has untruncated descriptions")

	p := posts[0]
	assert.Equal(t, "freehire", p.Source, "a career-page origin counts as a company board")
	assert.Equal(t, "freehire · greenhouse", p.SourceName)
	assert.Equal(t, "https://freehire.me/jobs/ok-1", p.URL, "the source link credits freehire")
	assert.Equal(t, "https://job-boards.greenhouse.io/acme/jobs/1?utm_source=freehire.me", p.ApplyURL, "applying goes to the employer")
	assert.Equal(t, "We write Go. Go is our language.", p.Text)
	assert.True(t, p.Remote)
	assert.Equal(t, models.Salary{Min: 120000, Max: 150000, Currency: "USD", Period: models.PeriodYear}, p.Salary)
	assert.Contains(t, p.Hints, "senior")
	assert.Contains(t, p.Hints, "relocation")
	assert.Contains(t, p.Hints, "English B2")

	assert.Equal(t, "freehire-board", posts[1].Source, "hh is a job board, not a career page")
	assert.NotContains(t, posts[1].Hints, "relocation")
}

func TestFreehire_FailedPage_ErrorWithPartial(t *testing.T) {
	var full []string
	for i := 0; i < freehirePage; i++ {
		full = append(full, freehireJobJSON("job-"+strings.Repeat("x", i+1), "lever", ""))
	}
	g := &pagedGetter{
		pages:  map[string]string{"0": `{"meta":{"total":250},"data":[` + strings.Join(full, ",") + `]}`},
		failAt: "100",
	}

	posts, err := NewFreehire(g).Fetch(context.Background())

	require.Error(t, err, "a partial list must not let missing roles look closed")
	assert.Len(t, posts, freehirePage)
}

func TestGolangProjects_NonBreakingSpaces_Split(t *testing.T) {
	role, company := NewGolangProjects(nil).split("Sr. Software Engineer\u00a0@\u00a0Prenosis")
	assert.Equal(t, "Sr. Software Engineer", role)
	assert.Equal(t, "Prenosis", company)
}

func TestFreehire_UnknownAndPublicBoards_AreBoards(t *testing.T) {
	f := NewFreehire(nil)
	for origin, want := range map[string]string{
		"greenhouse": "freehire", "yandex": "freehire", "workday": "freehire",
		"trudvsem": "freehire-board", "arbeitsagentur": "freehire-board", "some-new-origin": "freehire-board", "hh": "freehire-board",
	} {
		var j freehireJob
		require.NoError(t, json.Unmarshal([]byte(freehireJobJSON("s-"+origin, origin, "")), &j))
		p, ok := f.posting(j)
		require.True(t, ok, origin)
		assert.Equal(t, want, p.Source, origin)
	}
}

func TestFreehire_TelegramOrigin_IsStream(t *testing.T) {
	var j freehireJob
	require.NoError(t, json.Unmarshal([]byte(freehireJobJSON("tg-1", "telegram", "")), &j))
	p, ok := NewFreehire(nil).posting(j)
	require.True(t, ok)
	assert.Equal(t, "freehire-stream", p.Source)
	assert.False(t, models.SourceRef{Source: p.Source}.Listing(), "a relayed channel post cannot close a job")
}
