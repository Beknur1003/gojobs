package telegram

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testdata/rabota_golang.html is a real t.me/s/rabota_golang page saved on
// 2026-10-01: 20 messages, ids 1274..1294 (one id is a deleted message).
func TestParsePage_RealChannel_PostsAndCursor(t *testing.T) {
	body, err := os.ReadFile("testdata/rabota_golang.html")
	require.NoError(t, err)

	posts, oldest, err := ParsePage(body, "rabota_golang", true)
	require.NoError(t, err)

	assert.Equal(t, 1274, oldest, "cursor for the next page is the smallest id")
	require.NotEmpty(t, posts)

	first := posts[0]
	assert.Equal(t, "rabota_golang/1274", first.ExternalID)
	assert.Equal(t, "https://t.me/rabota_golang/1274", first.URL)
	assert.Equal(t, "@rabota_golang", first.SourceName)
	assert.True(t, first.GoOnly)
	assert.False(t, first.PostedAt.IsZero())
	assert.True(t, strings.HasPrefix(first.Text, "#senior #удаленка #гибрид"), first.Text[:60])
	assert.Contains(t, first.Text, "Selectel\nTeamLead команды разработки Объектного хранилища")
	assert.Contains(t, first.Links, "https://selectel.ru/careers/all/vacancy/1572/")

	for _, p := range posts {
		for _, l := range p.Links {
			assert.False(t, strings.HasPrefix(l, "?q="), "hashtag search links are dropped")
		}
	}
}

func TestPageURL_SearchAndCursor_Encoded(t *testing.T) {
	assert.Equal(t, "https://t.me/s/rabota_golang", pageURL(Channel{Name: "rabota_golang"}, 0))
	assert.Equal(t, "https://t.me/s/rabota_golang?before=1274", pageURL(Channel{Name: "rabota_golang"}, 1274))
	assert.Equal(t, "https://t.me/s/devkz_jobs?before=13591&q=golang", pageURL(Channel{Name: "devkz_jobs", Search: "golang"}, 13591))
}
