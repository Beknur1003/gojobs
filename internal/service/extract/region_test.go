package extract

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Beknur1003/gojobs/internal/models"
)

func TestRegions_RealCases_Placed(t *testing.T) {
	remote := []models.WorkFormat{models.FormatRemote}
	tests := []struct {
		name string
		job  models.Job
		want []models.Region
	}{
		{"city and state code", models.Job{Location: "San Jose, CA"}, []models.Region{models.RegionNA}},
		{"board country code", models.Job{Location: "Petaling Jaya, my"}, []models.Region{models.RegionAsia}},
		{"state before country code", models.Job{Location: "Bengaluru, KA, in"}, []models.Region{models.RegionAsia}},
		{"remote with country", models.Job{Location: "Remote - US", Formats: remote}, []models.Region{models.RegionNA}},
		{"latin america is not the usa", models.Job{Location: "Remote, Latin America"}, []models.Region{models.RegionLatAm}},
		{"porto alegre is not portugal", models.Job{Location: "Porto Alegre"}, []models.Region{models.RegionLatAm}},
		{"several countries", models.Job{Location: "Cyprus, Kazakhstan, Georgia, Serbia"},
			[]models.Region{models.RegionKZ, models.RegionCIS, models.RegionEurope}},
		{"russian city declined in the post", models.Job{Lang: "ru", Text: "Go-разработчик\nГибрид (Москва)\nдо 200 000 ₽"},
			[]models.Region{models.RegionCIS}},
		{"kazakh office in the post", models.Job{Lang: "ru", Text: "Go-разработчик\nот 3 500 $\nОфис (Астана)"},
			[]models.Region{models.RegionKZ, models.RegionCIS}},
		{"outside russia is not russia", models.Job{Location: "вне РФ", Formats: remote}, nil},
		{"worldwide location", models.Job{Location: "Home based - Worldwide", Formats: remote}, []models.Region{models.RegionWorld}},
		{"anywhere in the world", models.Job{Location: "Anywhere in the World", Formats: remote}, []models.Region{models.RegionWorld}},
		{"anywhere in a country is that country", models.Job{Location: "Anywhere in the United States", Formats: remote},
			[]models.Region{models.RegionNA}},
		{"international label over a country title",
			models.Job{Title: "Senior Software Engineer, Applied AI (Spain)", Location: "Remote - International", Formats: remote},
			[]models.Region{models.RegionEurope}},
		{"perk is not worldwide", models.Job{Location: "Remote", Formats: remote,
			Text: "Benefits: work from anywhere for four weeks a year, remote, global culture"}, nil},
		{"explicit worldwide in the post", models.Job{Formats: remote,
			Text: "Senior Backend Engineer (Go)\nRemote - Global\nWe are building a wallet."}, []models.Region{models.RegionWorld}},
		{"salary currency", models.Job{Location: "Remote", Salary: models.Salary{Min: 1, Currency: "EUR"}},
			[]models.Region{models.RegionEurope}},
		{"russian post with no place", models.Job{Lang: "ru", Text: "Golang-разработчик\n#удаленка\nКомпания: Альфа-Банк"},
			[]models.Region{models.RegionCIS}},
		{"djinni post with no place", models.Job{Lang: "ru", Text: "Own the authorization platform end-to-end.",
			Sources: []models.SourceRef{{Source: "djinni"}}}, []models.Region{models.RegionEurope}},
		{"nothing known", models.Job{Location: "Hybrid", Lang: "en", Text: "At Acme we build the internet."}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Regions(tt.job))
		})
	}
}

func TestGoMain_RealCases_Decided(t *testing.T) {
	tests := []struct {
		title, text string
		want        bool
	}{
		{"Senior Golang Developer", "", true},
		{"Go/Python Developer", "", true},
		{"Тим лид разработки (python/go)", "", false},
		{"Backend Engineer", "Design and implement APIs in Go.\n5–8 years of experience in golang.", true},
		{"Backend Engineer", "Proficiency in Python, Golang, Scala, or NodeJS.", false},
		{"Software Engineer", "Fluency in Go, Java, C++ or Python; willingness to work primarily in Go.", true},
		{"Бэкенд-разработчик", "Стек: go, PostgreSQL, Kafka. Опыт разработки на go от 3 лет.", true},
		{"Python Developer", "Python, Django; Go is a plus.", false},
		{"Go-to-Market Manager", "Own the go-to-market plan.", false},
		{"Software Engineer", "Our stack: Kubernetes, Terraform.", false},
	}
	for _, tt := range tests {
		t.Run(tt.title+"|"+tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, GoMain(tt.title, tt.text))
		})
	}
}
