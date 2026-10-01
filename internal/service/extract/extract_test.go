package extract

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/models"
)

func TestContacts_Kinds_Extracted(t *testing.T) {
	text := "Пишите @hr_anna или на jobs@acme.io\nСайт: https://acme.io/careers\nOur bot: @acme_jobs_bot"
	links := []string{"https://t.me/recruiter_bob", "mailto:cv@acme.io?subject=Go", "https://acme.io/careers", "https://t.me/addlist/xyz"}

	got := Contacts(text, links, nil, true)

	assert.Equal(t, []models.Contact{
		{Kind: models.ContactEmail, Value: "cv@acme.io"},
		{Kind: models.ContactEmail, Value: "jobs@acme.io"},
		{Kind: models.ContactTelegram, Value: "hr_anna"},
		{Kind: models.ContactTelegram, Value: "recruiter_bob"},
		{Kind: models.ContactURL, Value: "https://acme.io/careers"},
	}, got)
}

func TestContacts_IgnoreAndNoURLs_Skipped(t *testing.T) {
	ignore := map[string]bool{ContactKey(models.ContactTelegram, "rabota_golang"): true}
	got := Contacts("Канал @rabota_golang, резюме @hr_anna", []string{"https://acme.io"}, ignore, false)
	assert.Equal(t, []models.Contact{{Kind: models.ContactTelegram, Value: "hr_anna"}}, got)
}

func TestEmails_Obfuscated_Decoded(t *testing.T) {
	tests := []struct{ in, want string }{
		{"email jobs [at] acme [dot] com", "jobs@acme.com"},
		{"write to hiring (at) acme.io", "hiring@acme.io"},
		{"ping careers at acme dot dev", "careers@acme.dev"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, []string{tt.want}, Emails(tt.in, nil))
		})
	}
}

func TestEmails_ImagesAndExamples_Dropped(t *testing.T) {
	assert.Empty(t, Emails("logo@2x.png you@example.com", nil))
}

func TestIsGo_Sources_Decided(t *testing.T) {
	tests := []struct {
		name  string
		post  models.Posting
		title string
		want  bool
	}{
		{"golang in body, open channel", models.Posting{Source: "telegram", Text: "Ищем бэкенд на Golang"}, "Backend Developer", true},
		{"go developer phrase", models.Posting{Source: "telegram", Text: "Ищем Go-разработчика в команду"}, "Разработчик", true},
		{"python role in open channel", models.Posting{Source: "telegram", Text: "Python, Django, немного Go-скриптов не нужно"}, "Python Developer", false},
		{"go-to-market is not go", models.Posting{Source: "greenhouse", Text: "Drive our Go-to-market. Go to the moon."}, "Software Engineer", false},
		{"ats needs two mentions", models.Posting{Source: "greenhouse", Text: "We use Go, Python and Java."}, "Backend Engineer", false},
		{"ats with real go", models.Posting{Source: "greenhouse", Text: "Services are written in Go. You know Go well."}, "Backend Engineer", true},
		{"ats non engineering title", models.Posting{Source: "greenhouse", Text: "Our stack is Golang"}, "Account Executive", false},
		{"go-only channel without mention", models.Posting{Source: "telegram", GoOnly: true, Text: "Ищем бэкендера в финтех"}, "Backend Developer", true},
		{"go-only channel other language title", models.Posting{Source: "telegram", GoOnly: true, Text: "Ищем фронтендера"}, "Frontend Developer", false},
		{"tag golang", models.Posting{Source: "remoteok", Tags: []string{"golang"}}, "Software Engineer", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsGo(tt.post, tt.title))
		})
	}
}

func TestIsVacancy_TelegramNoise_Rejected(t *testing.T) {
	long := " Требования: опыт от 3 лет, знание PostgreSQL, Kafka, Kubernetes. Условия: удалёнка, белая зарплата, ДМС. Пишите в личку."
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"vacancy", "#vacancy Senior Go Developer." + long, true},
		{"resume", "#резюме Ищу работу Go-разработчиком." + long, false},
		{"ad", "Курс по Go со скидкой! erid: 2Vtzqx." + long, false},
		{"too short", "Go meetup в пятницу", false},
		{"course promo", "Как не потратить недельный лимит AI-кодинга за три дня? Бесплатный вебинар для Go-разработчиков: разберём агентов, промокод на курс внутри. Регистрация по ссылке, эфир в четверг, будет запись для всех участников.", false},
		{"course without vacancy bones", "3 курса по цене одного: соберите стек для оффера в топовую IT-компанию. Для Go-разработчиков, которые хотят расти до senior: практика, ревью кода, карьерные консультации от наставников из бигтеха.", false},
		{"greeting without a role", "Коллеги, доброго дня! Напоминаем, что в канале действуют правила: не публикуем вакансии без зарплатной вилки, а сообщения со ссылками на сторонние ресурсы проходят модерацию. Спасибо, что вы с нами!", false},
		{"vacancy with learning perks", "Ищем Go-разработчика в команду облака. Требования: Go от 3 лет. Мы предлагаем: обучение за счёт компании, курсы английского, бесплатные обеды, ДМС. Резюме: hr@acme.ru", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsVacancy(models.Posting{Source: "telegram", Text: tt.text}))
		})
	}
}

func TestTitleAndCompany_Layouts_Recognized(t *testing.T) {
	tests := []struct {
		name, text, title, company string
	}{
		{
			"company line above role under hashtags",
			"#senior #удаленка\n\nSelectel\nTeamLead команды разработки Объектного хранилища\nОпыт от 2 лет",
			"TeamLead команды разработки Объектного хранилища", "Selectel",
		},
		{
			"labels",
			"🔥 Вакансия: Senior Golang Developer\nКомпания: Acme — финтех-стартап\nЗП: 5000$",
			"Senior Golang Developer", "Acme",
		},
		{
			"pipe-separated title, company in the next line",
			"⚡️ Golang-разработчик | Офис (Алматы)\n\nRocket Tech — уже 13 лет мы делаем продукты.",
			"Golang-разработчик", "Rocket Tech",
		},
		{
			"title dash company under hashtags",
			"#Go #Golang #Remote #Senior\n\nSenior Backend Go dev. - Playneta\nЗанятость: полная",
			"Senior Backend Go dev.", "Playneta",
		},
		{
			"company as 'в X —'",
			"Senior Backend Developer (Go)\nв Джум — международная группа компаний в e-commerce.",
			"Senior Backend Developer (Go)", "Джум",
		},
		{
			"company as 'X is an'",
			"Software Engineer – Go, Networking & Distributed Systems\nCodiLime is an IT company that builds networks.",
			"Software Engineer – Go, Networking & Distributed Systems", "CodiLime",
		},
		{
			"markdown heading",
			"### Сетевой инженер / Go-разработчик\nО нас: стартап",
			"Сетевой инженер / Go-разработчик", "",
		},
		{
			"channel header with a handle above the role",
			"Публикатор: Alina Romashka\nОбсуждение: @devops_jobs\n#вакансия #devops\n\nDevOps / Infrastructure Engineer (HFT)\nКомпания: Triple Lemniscate Research",
			"DevOps / Infrastructure Engineer (HFT)", "Triple Lemniscate Research",
		},
		{
			"english with emoji",
			"💼 Middle Go Engineer\n📍 Remote\nWe build payments.",
			"Middle Go Engineer", "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, company := TitleAndCompany(tt.text)
			assert.Equal(t, tt.title, title)
			assert.Equal(t, tt.company, company)
		})
	}
}

func TestFormatsGradesEnglish_Text_Read(t *testing.T) {
	assert.Equal(t, []models.WorkFormat{models.FormatRemote, models.FormatHybrid}, Formats("#удаленка #гибрид Москва", false))
	assert.Equal(t, []models.WorkFormat{models.FormatOffice}, Formats("Работа в офисе, удалёнки нет", false))
	assert.Equal(t, []models.Grade{models.GradeSenior}, Grades("Senior Go Developer", "you will mentor junior devs"))
	assert.Equal(t, []models.Grade{models.GradeLead}, Grades("Тимлид Go", ""))
	assert.Equal(t, "b2", English("Английский B2 и выше"))
	assert.Equal(t, "b2", English("English: Upper-Intermediate"))
	assert.Equal(t, "", English("Нужен опыт с Kafka"))
	assert.Equal(t, []models.Employment{models.EmploymentContract}, Employment("Оформление: ИП или самозанятость"))
}

func TestNormalize_TelegramPost_Structured(t *testing.T) {
	p := models.Posting{
		Source: "telegram", Feed: "telegram:rabota_golang", SourceName: "@rabota_golang",
		ExternalID: "rabota_golang/1", URL: "https://t.me/rabota_golang/1",
		Text:  "#middle #удаленка\n\nOzon\nGo-разработчик в команду платежей\nЗП: 300-400 тыс. руб.\nСтек: Go, PostgreSQL, Kafka, k8s\nПишите @hr_anna\n\nПодписывайтесь на @rabota_golang",
		Links: []string{"https://t.me/rabota_golang"},
	}
	noise := Noise{
		Lines:    map[string]bool{},
		Contacts: map[string]bool{ContactKey(models.ContactTelegram, "rabota_golang"): true},
	}

	j := Normalize(p, noise)

	assert.Equal(t, "Go-разработчик в команду платежей", j.Title)
	assert.Equal(t, "Ozon", j.Company)
	assert.Equal(t, 300000, j.Salary.Min)
	assert.Equal(t, "RUB", j.Salary.Currency)
	assert.Equal(t, []models.WorkFormat{models.FormatRemote}, j.Formats)
	assert.Equal(t, []models.Grade{models.GradeMiddle}, j.Grades)
	assert.Equal(t, []models.Contact{{Kind: models.ContactTelegram, Value: "hr_anna"}}, j.Contacts)
	assert.Equal(t, []string{"PostgreSQL", "Kafka", "Kubernetes", "FinTech"}, j.Stack)
	assert.NotContains(t, j.Text, "Подписывайтесь")
	assert.Equal(t, "ru", j.Lang)
	require.Len(t, j.Sources, 1)
	assert.Equal(t, "telegram:rabota_golang", j.Sources[0].Feed)
	assert.Equal(t, "go-razrabotchik-v-komandu-platezhey-"+j.ID[:6], j.Slug)
}
