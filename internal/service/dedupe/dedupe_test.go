package dedupe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/models"
)

const vacancy = `Senior Go Developer в команду платежей. Пишем микросервисы на Go,
PostgreSQL и Kafka, деплоим в Kubernetes. Требования: опыт коммерческой
разработки на Go от 4 лет, понимание конкурентности, профилирование.
Условия: удалёнка, белая зарплата, ДМС, техника.`

func job(id, text string, posted time.Time, contacts ...models.Contact) models.Job {
	return models.Job{
		ID: id, Title: "Senior Go Developer", Text: text, PostedAt: posted, Contacts: contacts,
		Sources: []models.SourceRef{{Source: "telegram", ExternalID: id}},
	}
}

func TestFindDuplicate_Cases_Matched(t *testing.T) {
	day := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	anna := models.Contact{Kind: models.ContactTelegram, Value: "hr_anna"}

	tests := []struct {
		name string
		new  models.Job
		want bool
	}{
		{"repost with a different footer", job("b", vacancy+"\nПодписывайтесь на @other_channel", day.Add(48*time.Hour)), true},
		{"edited repost sharing the contact", job("c", "Ищем Senior Go Developer в платежи: Go, PostgreSQL, Kafka, Kubernetes. Удалёнка, ДМС.", day, anna), false},
		{"unrelated vacancy", job("d", "Python-разработчик в команду аналитики. Django, Celery, Redis. Офис в Алматы.", day), false},
		{"same text months apart", job("e", vacancy, day.Add(90*24*time.Hour)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix := NewIndex([]models.Job{job("a", vacancy, day, anna)})
			_, got := ix.FindDuplicate(tt.new)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindDuplicate_SameCompanyAndTitle_Matched(t *testing.T) {
	day := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	a := job("a", "Long description on the company career page.", day)
	a.Company = "Acme Inc."
	b := job("b", "Short teaser on a remote board.", day.Add(5*24*time.Hour))
	b.Company = "ACME"

	ix := NewIndex([]models.Job{a})
	i, ok := ix.FindDuplicate(b)
	require.True(t, ok)
	assert.Equal(t, 0, i)
}

func TestMerge_Duplicate_KeepsEarliestAndUnion(t *testing.T) {
	day := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	dst := job("a", vacancy, day.Add(24*time.Hour), models.Contact{Kind: models.ContactEmail, Value: "a@acme.io"})
	src := job("b", vacancy, day, models.Contact{Kind: models.ContactTelegram, Value: "hr_anna"})
	src.Salary = models.Salary{Min: 5000, Currency: "USD", Period: models.PeriodMonth}
	src.Company = "Acme"

	got := Merge(dst, src)

	assert.Equal(t, day, got.PostedAt)
	assert.Len(t, got.Sources, 2)
	assert.Len(t, got.Contacts, 2)
	assert.Equal(t, "Acme", got.Company)
	assert.Equal(t, 5000, got.Salary.Min)
	assert.Equal(t, "a", got.ID, "the first-seen job keeps its identity")
}

func TestJaccard_Sets_Overlap(t *testing.T) {
	assert.InDelta(t, 1.0, Jaccard(Shingles(vacancy), Shingles(vacancy)), 1e-9)
	assert.Equal(t, 0.0, Jaccard(Shingles(vacancy), nil))
	assert.Less(t, Jaccard(Shingles(vacancy), Shingles("Python-разработчик в команду аналитики, Django и Celery")), 0.05)
}
