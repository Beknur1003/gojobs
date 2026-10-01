package boards

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/models"
)

// The fixtures are real pages saved on 2026-10-01, cut down to their
// JobPosting markup and description.

func TestFindJobPosting_HabrPage_StringAddress(t *testing.T) {
	page, err := os.ReadFile("testdata/habr_vacancy.html")
	require.NoError(t, err)

	jp, ok := findJobPosting(page)
	require.True(t, ok)
	assert.Equal(t, "Senior разработчик Golang", strings.TrimSpace(jp.Title))
	assert.Equal(t, "BETBOOM", jp.Organization.Name)
	assert.Equal(t, "Москва", jp.location(), `"address": "Москва" is a plain string`)
	assert.Equal(t, 2026, jp.posted().Year())
	text, _ := htmlToText(jp.Description)
	assert.Contains(t, text, "НАШ СТЕК")
}

func TestFindJobPosting_GetMatchPage_SalaryAndSection(t *testing.T) {
	page, err := os.ReadFile("testdata/getmatch_vacancy.html")
	require.NoError(t, err)

	jp, ok := findJobPosting(page)
	require.True(t, ok)
	assert.Equal(t, "Go-разработчик (Design Time)", jp.Title)
	assert.Equal(t, "Сбер", jp.Organization.Name)
	assert.Equal(t, "Москва, Россия", jp.location())
	assert.Equal(t, models.Salary{Min: 300000, Max: 400000, Currency: "RUB", Period: models.PeriodMonth}, jp.salary())

	text, _ := htmlToText(sectionHTML(page, "b-vacancy-description"))
	assert.True(t, strings.HasPrefix(text, "Наша команда создает линейку продуктов"), text[:80])
}

func TestFindJobPosting_ArchivedPage_None(t *testing.T) {
	page, err := os.ReadFile("testdata/getmatch_archived.html")
	require.NoError(t, err)
	_, ok := findJobPosting(page)
	assert.False(t, ok, "an archived vacancy has no JobPosting")
}

// pageGetter serves fixed pages by URL.
type pageGetter map[string]string

func (g pageGetter) Get(_ context.Context, u string) ([]byte, error) {
	if body, ok := g[u]; ok {
		return []byte(body), nil
	}
	return nil, errors.New("status 404")
}

func (g pageGetter) GetJSON(ctx context.Context, u string, dst any) error {
	body, err := g.Get(ctx, u)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

func TestGetMatch_Sitemap_NewestGoPagesOnly(t *testing.T) {
	vacancy, err := os.ReadFile("testdata/getmatch_vacancy.html")
	require.NoError(t, err)
	archived, err := os.ReadFile("testdata/getmatch_archived.html")
	require.NoError(t, err)

	g := pageGetter{
		"https://getmatch.ru/sitemap.xml": `<urlset>
<url><loc>https://getmatch.ru/vacancies/golang</loc></url>
<url><loc>https://getmatch.ru/vacancies/36021-go-razrabotchik-design-time</loc></url>
<url><loc>https://getmatch.ru/vacancies/16065-go-razrabotchik-produkty-dlia-biznesa</loc></url>
<url><loc>https://getmatch.ru/vacancies/36000-python-developer</loc></url>
<url><loc>https://getmatch.ru/vacancies/35999-google-ads-manager</loc></url>
</urlset>`,
		"https://getmatch.ru/vacancies/36021-go-razrabotchik-design-time":           string(vacancy),
		"https://getmatch.ru/vacancies/16065-go-razrabotchik-produkty-dlia-biznesa": string(archived),
	}

	posts, err := NewGetMatch(g).Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, posts, 1, "archived and non-Go addresses are skipped")
	p := posts[0]
	assert.Equal(t, "36021", p.ExternalID)
	assert.Equal(t, "Сбер", p.Company)
	assert.Equal(t, "https://getmatch.ru/vacancies/36021-go-razrabotchik-design-time", p.ApplyURL)
	assert.Equal(t, 300000, p.Salary.Min)
}
