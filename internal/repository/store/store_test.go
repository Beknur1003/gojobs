package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/models"
)

func TestSaveLoad_State_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "jobs.json")
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	in := State{
		UpdatedAt: at,
		Feeds:     map[string]models.FeedStatus{"remoteok": {LastOK: at, LastRun: at, Count: 99}},
		Jobs: []models.Job{{
			ID: "abc", Slug: "go-dev-abc", Title: "Go Developer", Company: "Acme", Text: "text", Lang: "en",
			Salary:   models.Salary{Min: 5000, Max: 7000, Currency: "USD", Period: models.PeriodMonth, MonthlyUSDMin: 5000, MonthlyUSDMax: 7000},
			Formats:  []models.WorkFormat{models.FormatRemote},
			Grades:   []models.Grade{models.GradeSenior},
			Contacts: []models.Contact{{Kind: models.ContactTelegram, Value: "hr_anna"}},
			Sources:  []models.SourceRef{{Source: "telegram", Feed: "telegram:x", Name: "@x", ExternalID: "x/1", URL: "https://t.me/x/1", PostedAt: at, LastSeen: at}},
			PostedAt: at, FirstSeen: at, LastSeen: at,
			Closed: true, // derived, must not be stored
		}},
	}
	require.NoError(t, Save(path, in))

	out, err := Load(path)
	require.NoError(t, err)

	want := in.Jobs[0]
	want.Closed = false
	assert.Equal(t, []models.Job{want}, out.Jobs)
	assert.Equal(t, in.Feeds, out.Feeds)
	assert.Equal(t, at, out.UpdatedAt)
}

func TestLoad_MissingFile_EmptyState(t *testing.T) {
	st, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	require.NoError(t, err)
	assert.Empty(t, st.Jobs)
	assert.NotNil(t, st.Feeds)
}
