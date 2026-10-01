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
		Text:     body + "\n\n🔥 Больше вакансий в @" + channel + "\nРазмещение: @channel_admin",
		Links:    []string{"https://t.me/addlist/abc", "https://t.me/channel_admin"},
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
			assert.NotEqual(t, "channel_admin", c.Value, "footer handle must not become a contact")
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
	board := models.Job{Sources: []models.SourceRef{{Source: "remoteok", Feed: "remoteok", LastSeen: seen}}}
	channel := models.Job{Sources: []models.SourceRef{{Source: "telegram", Feed: "telegram:x", LastSeen: seen}}}

	fresh := map[string]models.FeedStatus{"remoteok": {LastOK: now}, "telegram:x": {LastOK: now}}
	stale := map[string]models.FeedStatus{"remoteok": {LastOK: seen}}

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
	assert.True(t, noise.Contacts[extract.ContactKey(models.ContactTelegram, "channel_admin")], "in every post: footer")
	assert.False(t, noise.Contacts[extract.ContactKey(models.ContactTelegram, "hr_agency")], "in 40% of posts: still a person")
	assert.False(t, noise.Contacts[extract.ContactKey(models.ContactEmail, "jobs@agency.io")], "emails are never noise")
}
