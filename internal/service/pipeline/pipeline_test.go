package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/httpx"
	"github.com/Beknur1003/gojobs/internal/service/extract"
)

type fakeSource struct {
	name  string
	posts []models.Posting
	err   error
}

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Fetch(context.Context) ([]models.Posting, error) {
	return f.posts, f.err
}

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newPipeline() *Pipeline {
	p := New(slog.New(slog.NewTextHandler(io.Discard, nil)), 180)
	p.now = func() time.Time { return now }
	return p
}

func tgPost(channel string, id int, body string) models.Posting {
	return models.Posting{
		Source: "telegram", SourceName: "@" + channel, GoOnly: true,
		ExternalID: fmt.Sprintf("%s/%d", channel, id), URL: fmt.Sprintf("https://t.me/%s/%d", channel, id),
		Text:     body + "\n\n🔥 Больше вакансий в @" + channel + "\nРазмещение: @lena_admin",
		Links:    []string{"https://t.me/addlist/abc", "https://t.me/lena_admin"},
		PostedAt: now.Add(-time.Duration(id) * time.Hour),
	}
}

const body = "Senior Go Developer\nКомпания делает платежи для маркетплейсов. Требования: Go от 4 лет, PostgreSQL, Kafka, опыт с highload. Условия: удалёнка, зарплата 5000-7000$. Пишите @hr_%d"

func TestRun_TelegramChannel_FooterIgnoredAndRepostsMerged(t *testing.T) {
	var posts []models.Posting
	for i := 1; i <= 6; i++ {
		posts = append(posts, tgPost("rabota_golang", i, fmt.Sprintf(body, i)+fmt.Sprintf("\nПроект номер %d, команда %d человек, домен %d.", i, i*3, i*7)))
	}
	// The same vacancy reposted by another channel.
	repost := tgPost("godevjob", 50, fmt.Sprintf(body, 1)+"\nПроект номер 1, команда 3 человек, домен 7.")

	sources := []Source{
		fakeSource{name: "telegram:rabota_golang", posts: posts},
		fakeSource{name: "telegram:godevjob", posts: []models.Posting{repost}},
	}

	jobs, feeds, stats := newPipeline().Run(context.Background(), sources, nil, nil)

	assert.Equal(t, 7, stats.Accepted)
	assert.Equal(t, 6, stats.New)
	assert.Equal(t, 1, stats.Merged)
	require.Len(t, jobs, 6)
	for _, j := range jobs {
		for _, c := range j.Contacts {
			assert.NotEqual(t, "lena_admin", c.Value, "footer handle must not become a contact")
			assert.NotEqual(t, "rabota_golang", c.Value)
		}
		assert.NotContains(t, j.Text, "Больше вакансий")
	}
	assert.Equal(t, now, feeds["telegram:rabota_golang"].LastOK)
}

func TestRun_FailedFeed_RecordedAndOthersKept(t *testing.T) {
	sources := []Source{
		fakeSource{name: "remoteok", err: errors.New("status 503")},
		fakeSource{name: "telegram:rabota_golang", posts: []models.Posting{tgPost("rabota_golang", 1, fmt.Sprintf(body, 1))}},
	}
	prev := map[string]models.FeedStatus{"remoteok": {LastOK: now.Add(-24 * time.Hour)}}

	jobs, feeds, stats := newPipeline().Run(context.Background(), sources, nil, prev)

	assert.Len(t, jobs, 1)
	assert.Equal(t, []string{"remoteok"}, stats.Failed)
	assert.Equal(t, "status 503", feeds["remoteok"].Error)
	assert.Equal(t, now.Add(-24*time.Hour), feeds["remoteok"].LastOK, "a failure must not advance LastOK")
}

func TestClosed_ListingGone_Closed(t *testing.T) {
	seen := now.Add(-48 * time.Hour)
	board := models.Job{Sources: []models.SourceRef{{Source: "greenhouse", Feed: "greenhouse:acme", LastSeen: seen}}}
	channel := models.Job{Sources: []models.SourceRef{{Source: "telegram", Feed: "telegram:x", LastSeen: seen}}}

	fresh := map[string]models.FeedStatus{"greenhouse:acme": {LastOK: now}, "telegram:x": {LastOK: now}}
	stale := map[string]models.FeedStatus{"greenhouse:acme": {LastOK: seen}}

	assert.True(t, Closed(board, fresh), "board fetched since and the job was not in it")
	assert.False(t, Closed(board, stale), "board not fetched since: unknown, keep it open")
	assert.False(t, Closed(channel, fresh), "channel posts never close")
}

func TestRun_RefetchedPost_KeepsIdentity(t *testing.T) {
	post := tgPost("rabota_golang", 1, fmt.Sprintf(body, 1))
	src := []Source{fakeSource{name: "telegram:rabota_golang", posts: []models.Posting{post}}}

	first, feeds, _ := newPipeline().Run(context.Background(), src, nil, nil)
	require.Len(t, first, 1)

	post.Text = "Senior Golang Engineer\n" + post.Text // edited post
	src = []Source{fakeSource{name: "telegram:rabota_golang", posts: []models.Posting{post}}}
	second, _, stats := newPipeline().Run(context.Background(), src, first, feeds)

	require.Len(t, second, 1)
	assert.Equal(t, 1, stats.Updated)
	assert.Equal(t, first[0].ID, second[0].ID)
	assert.Equal(t, first[0].Slug, second[0].Slug, "URLs never change")
	assert.Equal(t, "Senior Golang Engineer", second[0].Title)
}

func TestLearnNoise_Channel_FrequentHandlesAndEmailsKept(t *testing.T) {
	var posts []models.Posting
	for i := 1; i <= 10; i++ {
		text := fmt.Sprintf("Вакансия %d. Резюме на jobs@agency.io", i)
		if i <= 4 {
			text += " или @hr_agency" // an active recruiter: four posts of ten
		}
		posts = append(posts, tgPost("golang_job", i, text))
	}
	noise := learnNoise("telegram:golang_job", posts)

	assert.True(t, noise.Contacts[extract.ContactKey(models.ContactTelegram, "golang_job")])
	assert.True(t, noise.Contacts[extract.ContactKey(models.ContactTelegram, "lena_admin")], "in every post: footer")
	assert.False(t, noise.Contacts[extract.ContactKey(models.ContactTelegram, "hr_agency")], "in 40% of posts: still a person")
	assert.False(t, noise.Contacts[extract.ContactKey(models.ContactEmail, "jobs@agency.io")], "emails are never noise")
}

func TestRun_BoardStub_KeepsKnownJobOpen(t *testing.T) {
	board := models.Posting{
		Source: "greenhouse", SourceName: "Acme (Greenhouse)", ExternalID: "acme/1", URL: "https://x/1",
		Title: "Senior Go Engineer", Company: "Acme", PostedAt: now.Add(-100 * 24 * time.Hour),
		Text: "We build services in Go. Go experience required, plus Kafka and PostgreSQL.",
	}
	src := []Source{fakeSource{name: "greenhouse:acme", posts: []models.Posting{board}}}
	first, feeds, stats := newPipeline().Run(context.Background(), src, nil, nil)
	require.Len(t, first, 1)
	assert.Equal(t, 1, stats.Feeds["greenhouse:acme"].Go)

	later := newPipeline()
	later.now = func() time.Time { return now.Add(24 * time.Hour) }
	stubs := []models.Posting{
		{Source: "greenhouse", ExternalID: "acme/1", Title: "Senior Go Engineer", Stub: true},
		{Source: "greenhouse", ExternalID: "acme/2", Title: "Platform Engineer", Stub: true}, // checked before, not Go
	}
	second, _, stats := later.Run(context.Background(), []Source{fakeSource{name: "greenhouse:acme", posts: stubs}}, first, feeds)

	require.Len(t, second, 1)
	assert.Equal(t, 1, stats.Seen)
	assert.Equal(t, 1, stats.Feeds["greenhouse:acme"].Go)
	assert.Equal(t, now.Add(24*time.Hour), second[0].Sources[0].LastSeen)
	assert.False(t, second[0].Closed, "the board still lists it")
	assert.Equal(t, first[0].Text, second[0].Text, "a stub never replaces the description")
}

func TestRun_BoardGone_ListingsClosed(t *testing.T) {
	board := models.Posting{
		Source: "greenhouse", SourceName: "Acme (Greenhouse)", ExternalID: "acme/1", URL: "https://job-boards.greenhouse.io/acme/jobs/1",
		Title: "Senior Go Engineer", Company: "Acme", PostedAt: now.Add(-5 * 24 * time.Hour),
		Text: "We build services in Go. Go experience required, plus Kafka and PostgreSQL.",
	}
	first, feeds, _ := newPipeline().Run(context.Background(), []Source{fakeSource{name: "greenhouse:acme", posts: []models.Posting{board}}}, nil, nil)
	require.Len(t, first, 1)

	later := newPipeline()
	later.now = func() time.Time { return now.Add(48 * time.Hour) }
	gone := fakeSource{name: "greenhouse:acme", err: fmt.Errorf("greenhouse.Fetch acme: %w", httpx.ErrNotFound)}
	second, feeds, _ := later.Run(context.Background(), []Source{gone}, first, feeds)

	require.Len(t, second, 1)
	assert.True(t, second[0].Closed, "a board that is gone closes its listings")
	assert.Equal(t, "not found", feeds["greenhouse:acme"].Error)
}

func TestClosed_StreamCopyOfRemovedRole_Closed(t *testing.T) {
	seen := now.Add(-48 * time.Hour)
	j := models.Job{Sources: []models.SourceRef{
		{Source: "greenhouse", Feed: "greenhouse:acme", LastSeen: seen},
		{Source: "telegram", Feed: "telegram:x", LastSeen: seen},
	}}
	assert.True(t, Closed(j, map[string]models.FeedStatus{"greenhouse:acme": {LastOK: now}}),
		"the employer removed it; a channel repost cannot keep it open")
}

func TestRun_AggregatorNotFound_FailureNotClosure(t *testing.T) {
	post := models.Posting{Source: "himalayas", SourceName: "Himalayas", ExternalID: "h1", URL: "https://himalayas.app/x",
		Title: "Senior Go Engineer", Company: "Acme", PostedAt: now.Add(-24 * time.Hour), Remote: true,
		Text: "We build services in Go. Go experience required, plus Kafka and PostgreSQL."}
	first, feeds, _ := newPipeline().Run(context.Background(), []Source{fakeSource{name: "himalayas", posts: []models.Posting{post}}}, nil, nil)
	require.Len(t, first, 1)

	later := newPipeline()
	later.now = func() time.Time { return now.Add(48 * time.Hour) }
	moved := fakeSource{name: "himalayas", err: fmt.Errorf("himalayas.Fetch: %w", httpx.ErrNotFound)}
	second, _, stats := later.Run(context.Background(), []Source{moved}, first, feeds)

	assert.False(t, second[0].Closed, "a 404 from an aggregator API is a failure, not proof every role closed")
	assert.Equal(t, []string{"himalayas"}, stats.Failed)
}
