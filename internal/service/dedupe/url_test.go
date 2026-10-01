package dedupe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Beknur1003/gojobs/internal/models"
)

func TestURLKey_Shapes_Canonical(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://boards.greenhouse.io/acme/jobs/6675504", "greenhouse:6675504"},
		{"https://job-boards.greenhouse.io/clearstreet/jobs/6675504?utm_source=freehire.me", "greenhouse:6675504"},
		{"https://job-boards.eu.greenhouse.io/acme/jobs/6675504/", "greenhouse:6675504"},
		{"https://acme.com/careers/open-roles?gh_jid=6675504", "greenhouse:6675504"},
		{"https://jobs.lever.co/neon/2193db3f-77c5-43b8-b030-8f92c9882bf1/apply", "lever:2193db3f-77c5-43b8-b030-8f92c9882bf1"},
		{"https://jobs.ashbyhq.com/ramp/34413f8d-26bf-4bbc-8ade-eb309a0e2245/application", "ashby:34413f8d-26bf-4bbc-8ade-eb309a0e2245"},
		{"https://crowdstrike.wd5.myworkdayjobs.com/crowdstrikecareers/job/USA---Redmond-WA/Engineer-III_R24950", "workday:crowdstrike:r24950"},
		{"https://crowdstrike.wd5.myworkdayjobs.com/en-US/crowdstrikecareers/job/Remote/Engineer-III_R24950", "workday:crowdstrike:r24950"},
		{"https://www.HH.ru/vacancy/133975805?query=golang", "hh:133975805"},
		{"https://spb.hh.ru/vacancy/133975805", "hh:133975805"},
		{"https://hh.kz/vacancy/133975805", "hh:133975805"},
		{"https://ru.linkedin.com/jobs/view/senior-backend-developer-go-at-acme-4468202495", "linkedin:4468202495"},
		{"https://www.linkedin.com/jobs/view/4466210012/", "linkedin:4466210012"},
		{"https://recruiting.ultipro.com/PRE1019PRSD/JobBoard/4c05f321-903e-43d5-a4c0-5d9dd55dc34d/OpportunityDetail?opportunityId=D10FADC2-1", "ukg:pre1019prsd:d10fadc2-1"},
		{"https://recruiting.ultipro.com/pre1019prsd/JobBoard/4c05f321-903e-43d5-a4c0-5d9dd55dc34d/OpportunityDetail", ""},
		{"https://careers.acme.com/acme1019/board/view?jobId=77", "careers.acme.com/acme1019/board/view?jobid=77"},
		{"https://careers.acme.com/acme1019/board/view", ""},
		{"https://t.me/young_relocate/2130?utm_source=freehire.me", ""},
		{"https://acme.wd5.myworkdayjobs.com/careers/job/Remote/Engineer_R1/apply", "workday:acme:r1"},
		{"https://hpe.wd5.myworkdayjobs.com/WFMathpe/job/Houston/Cloud-Developer_1212916-2", "workday:hpe:1212916"},
		{"https://hpe.wd5.myworkdayjobs.com/Jobsathpe/job/Houston/Cloud-Developer_1212916", "workday:hpe:1212916"},
		{"https://astrazeneca.wd3.myworkdayjobs.com/Careers/job/Gothenburg/Engineer_R-256711-1", "workday:astrazeneca:r-256711"},
		{"https://acme.wd5.myworkdayjobs.com/External_Careers", ""},
		{"https://acme.wd5.myworkdayjobs.com/en-US/External_Careers?q=golang", ""},
		{"https://geekjob.ru/vacancy/6a3bb1e89a773e5ce80ef379", "geekjob.ru/vacancy/6a3bb1e89a773e5ce80ef379"},
		{"https://ozon.tech/careers", ""},
		{"https://jobs.apple.com/en-us/details/200674160/software-engineer", "jobs.apple.com/en-us/details/200674160"},
		{"https://careers.hpe.com/us/en/job/1200151/SRE-Staff", "careers.hpe.com/us/en/job/1200151"},
		{"https://acme.io", ""},
		{"not a url", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, URLKey(tt.in))
		})
	}
}

func TestIsVacancyKey_PostLinks_OnlyVacancyPages(t *testing.T) {
	assert.True(t, IsVacancyKey("hh.ru/vacancy/133975805"))
	assert.True(t, IsVacancyKey("greenhouse:123"))
	assert.True(t, IsVacancyKey("career.habr.com/vacancies/1000165676"))
	assert.False(t, IsVacancyKey("habr.com/ru/companies/ozon/articles/123456"))
	assert.True(t, IsVacancyKey(URLKey("https://career.ozon.ru/fintech/vacancy?id=3db63893-0000-4000-8000-000000000000")))
}

func TestFindDuplicate_HHRecreatedVacancy_StillMerges(t *testing.T) {
	day := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	text := "Junior+ golang разработчик в команду платежей. Go, PostgreSQL, Kafka. Удалёнка, белая зарплата, ДМС."
	a := models.Job{ID: "a", Title: "Junior+ golang разработчик", Text: text, PostedAt: day,
		Contacts: []models.Contact{{Kind: models.ContactURL, Value: "https://hh.ru/vacancy/133299381"}}}
	b := models.Job{ID: "b", Title: "Junior+ golang разработчик", Text: text, PostedAt: day.Add(40 * 24 * time.Hour),
		Contacts: []models.Contact{{Kind: models.ContactURL, Value: "https://hh.ru/vacancy/135436529"}}}

	_, ok := NewIndex([]models.Job{a}).FindDuplicate(b)

	assert.True(t, ok, "an hh vacancy re-created under a new id is still the same role")
}

func TestFindDuplicate_SamePostingURL_Merged(t *testing.T) {
	day := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	direct := models.Job{
		ID: "a", Title: "Senior Go Engineer", Company: "Clear Street", Text: "Long description from the board.",
		ApplyURL: "https://job-boards.greenhouse.io/clearstreet/jobs/6675504", PostedAt: day,
		Sources: []models.SourceRef{{Source: "greenhouse", ExternalID: "clearstreet/6675504", URL: "https://job-boards.greenhouse.io/clearstreet/jobs/6675504"}},
	}
	viaFreehire := models.Job{
		ID: "b", Title: "Senior Golang Engineer (Trading)", Company: "ClearStreet LLC", Text: "Different wording entirely.",
		ApplyURL: "https://boards.greenhouse.io/clearstreet/jobs/6675504?utm_source=freehire.me", PostedAt: day.Add(90 * 24 * time.Hour),
		Sources: []models.SourceRef{{Source: "freehire", ExternalID: "senior-go-xyz", URL: "https://freehire.me/jobs/senior-go-xyz"}},
	}

	ix := NewIndex([]models.Job{direct})
	i, ok := ix.FindDuplicate(viaFreehire)

	require.True(t, ok, "same posting, though title, company, text and date all differ")
	assert.Equal(t, 0, i)
}

func TestFindDuplicate_PostCitingArticle_NotMerged(t *testing.T) {
	article := models.Contact{Kind: models.ContactURL, Value: "https://habr.com/ru/companies/ozon/articles/123456/"}
	a := models.Job{ID: "a", Title: "Go-разработчик в Платежи", Text: "Платежи, Kafka, PostgreSQL.", Contacts: []models.Contact{article}}
	b := models.Job{ID: "b", Title: "Go-разработчик в Логистику", Text: "Логистика, ClickHouse, k8s.", Contacts: []models.Contact{article}}

	_, ok := NewIndex([]models.Job{a}).FindDuplicate(b)

	assert.False(t, ok)
}

func TestFindDuplicate_SameBoardDifferentIDs_Distinct(t *testing.T) {
	day := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	mk := func(id, req string) models.Job {
		return models.Job{
			ID: id, Title: "Software Engineer", Company: "Acme", Text: "Same boilerplate about Acme and Go services. " + id,
			ApplyURL: "https://acme.wd5.myworkdayjobs.com/careers/job/Remote/Software-Engineer_" + req, PostedAt: day,
			Sources: []models.SourceRef{{Source: "workday", Feed: "workday:acme|wd5|careers", ExternalID: "acme/" + req}},
		}
	}
	ix := NewIndex([]models.Job{mk("a", "R2023800")})

	_, ok := ix.FindDuplicate(mk("b", "R2024102"))

	assert.False(t, ok, "two requisitions of one tenant are two vacancies, however alike")
}

func TestMerge_AggregatorFirst_EmployerFieldsWin(t *testing.T) {
	aggregator := models.Job{ID: "a", Company: "Blockchain Association", Title: "Senior Engineer, Go",
		ApplyURL: "https://jobstash.xyz/x", Sources: []models.SourceRef{{Source: "freehire-board", ExternalID: "x"}}}
	employer := models.Job{ID: "b", Company: "Coinbase", Title: "Senior Software Engineer, Backend (Go)",
		ApplyURL: "https://www.coinbase.com/careers/positions/8003605?gh_jid=8003605", Sources: []models.SourceRef{{Source: "freehire", ExternalID: "y"}}}

	got := Merge(aggregator, employer)

	assert.Equal(t, "a", got.ID, "the page keeps its identity")
	assert.Equal(t, "Coinbase", got.Company)
	assert.Equal(t, employer.ApplyURL, got.ApplyURL)
}

func TestMerge_LeverSlug_DoesNotOverrideDisplayName(t *testing.T) {
	board := models.Job{ID: "a", Company: "Mattermost", Sources: []models.SourceRef{{Source: "himalayas", ExternalID: "x"}}}
	lever := models.Job{ID: "b", Company: "mattermost", ApplyURL: "https://jobs.lever.co/mattermost/2193db3f-77c5-43b8-b030-8f92c9882bf1",
		Sources: []models.SourceRef{{Source: "lever", ExternalID: "mattermost/2193db3f"}}}

	got := Merge(board, lever)

	assert.Equal(t, "Mattermost", got.Company)
	assert.Equal(t, lever.ApplyURL, got.ApplyURL, "the apply link still goes to the employer")
}

func TestFindDuplicate_FreehireCopiesFromTwoBoards_Merged(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	text := "Go-разработчик в команду платежей. Go, PostgreSQL, Kafka, gRPC. Удалёнка по РФ, белая зарплата, ДМС, обучение."
	viaHH := models.Job{ID: "a", Title: "Go-разработчик", Company: "Acme", Text: text, PostedAt: day,
		Sources: []models.SourceRef{{Source: "freehire-board", Feed: "freehire", ExternalID: "go-dev-hh"}}}
	viaHabr := models.Job{ID: "b", Title: "Go-разработчик", Company: "Acme", Text: text, PostedAt: day.Add(24 * time.Hour),
		Sources: []models.SourceRef{{Source: "freehire-board", Feed: "freehire", ExternalID: "go-dev-habr"}}}

	_, ok := NewIndex([]models.Job{viaHH}).FindDuplicate(viaHabr)

	assert.True(t, ok, "freehire is one feed of many boards; two of its slugs can be one vacancy")
}

func TestConsolidate_PostLinkingTwoVacancies_KeptApart(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	payments := models.Job{ID: "p", FirstSeen: t0, ApplyURL: "https://jobs.lever.co/acme/2193db3f-77c5-43b8-b030-8f92c9882bf1",
		Sources: []models.SourceRef{{Source: "lever", ExternalID: "acme/2193db3f"}}}
	platform := models.Job{ID: "q", FirstSeen: t0.Add(time.Hour), ApplyURL: "https://jobs.lever.co/acme/8a1c0e2b-0000-4000-8000-000000000000",
		Sources: []models.SourceRef{{Source: "lever", ExternalID: "acme/8a1c0e2b"}}}
	hn := models.Job{ID: "h", FirstSeen: t0.Add(2 * time.Hour), Sources: []models.SourceRef{{Source: "hn", ExternalID: "9"}},
		Contacts: []models.Contact{
			{Kind: models.ContactURL, Value: payments.ApplyURL},
			{Kind: models.ContactURL, Value: platform.ApplyURL},
		}}

	out := Consolidate([]models.Job{payments, platform, hn})

	assert.Len(t, out, 3, "links inside a post do not fuse two stored vacancies")
}

func TestConsolidate_StoredJobsSharingURL_Merged(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	hn := models.Job{ID: "hn", Title: "Backend Engineer", FirstSeen: t0,
		Contacts: []models.Contact{{Kind: models.ContactURL, Value: "https://jobs.lever.co/mattermost/2193db3f-77c5-43b8-b030-8f92c9882bf1"}},
		Sources:  []models.SourceRef{{Source: "hn", ExternalID: "1"}}}
	other := models.Job{ID: "other", Title: "Unrelated", FirstSeen: t0.Add(time.Hour),
		Sources: []models.SourceRef{{Source: "telegram", ExternalID: "c/1"}}}
	lever := models.Job{ID: "lever", Title: "Backend Engineer (Go)", FirstSeen: t0.Add(48 * time.Hour),
		ApplyURL: "https://jobs.lever.co/mattermost/2193db3f-77c5-43b8-b030-8f92c9882bf1/apply",
		Sources:  []models.SourceRef{{Source: "lever", ExternalID: "mattermost/2193db3f"}}}

	out := Consolidate([]models.Job{lever, hn, other})

	require.Len(t, out, 2)
	var merged models.Job
	for _, j := range out {
		if j.ID == "hn" {
			merged = j
		}
	}
	assert.Len(t, merged.Sources, 2, "the later Lever listing folds into the HN job seen first")
}

func TestFindDuplicate_IDsKeptAfterMerge_OtherReqStaysApart(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	text := "Cloud Developer in Houston. Go services, Kubernetes, PostgreSQL. Hybrid, full time, great benefits."
	viaFreehire := models.Job{ID: "a", Title: "Cloud Developer", Company: "HPE", Text: text, PostedAt: day,
		ApplyURL: "https://careers.hpe.com/us/en/job/HPE1US1209943/Cloud-Developer",
		Sources: []models.SourceRef{{Source: "freehire", Feed: "freehire", Name: "freehire · phenom", ExternalID: "cloud-dev-x",
			URL: "https://freehire.me/jobs/cloud-dev-x", ApplyURL: "https://careers.hpe.com/us/en/job/HPE1US1209943/Cloud-Developer"}}}
	direct := models.Job{ID: "b", Title: "Cloud Developer", Company: "HPE", Text: text, PostedAt: day,
		ApplyURL: "https://hpe.wd5.myworkdayjobs.com/Jobsathpe/job/Houston/Cloud-Developer_1209943",
		Sources: []models.SourceRef{{Source: "workday", Feed: "workday:hpe|wd5|Jobsathpe", Name: "HPE (Workday)", ExternalID: "hpe/1209943",
			URL:      "https://hpe.wd5.myworkdayjobs.com/Jobsathpe/job/Houston/Cloud-Developer_1209943",
			ApplyURL: "https://hpe.wd5.myworkdayjobs.com/Jobsathpe/job/Houston/Cloud-Developer_1209943"}}}
	ix := NewIndex([]models.Job{viaFreehire})
	ix.Replace(0, Merge(ix.Jobs[0], direct)) // same text, same role: one card

	other := direct
	other.ID = "c"
	other.Text = "Cloud Developer for the storage team. Rust and Go, firmware tooling, on-site in Houston." // another team
	other.ApplyURL = "https://hpe.wd5.myworkdayjobs.com/Jobsathpe/job/Houston/Cloud-Developer_1206800"
	other.Sources = []models.SourceRef{{Source: "workday", Feed: "workday:hpe|wd5|Jobsathpe", Name: "HPE (Workday)", ExternalID: "hpe/1206800",
		URL: other.ApplyURL, ApplyURL: other.ApplyURL}}

	_, ok := ix.FindDuplicate(other)

	assert.False(t, ok, "the merged card still knows it is requisition 1209943, not 1206800")
}

func TestFindDuplicate_SameOriginDifferentPostings_Distinct(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	text := "Grab is hiring for the Customer Data Platform. Go, Kafka, Kubernetes. Singapore, hybrid. We offer great benefits and growth."
	mk := func(id, title, apply string) models.Job {
		return models.Job{ID: id, Title: title, Company: "Grab", Text: text, PostedAt: day, ApplyURL: apply,
			Sources: []models.SourceRef{{Source: "freehire", Feed: "freehire", Name: "freehire · smartrecruiters", ExternalID: id,
				URL: "https://freehire.me/jobs/" + id, ApplyURL: apply}}}
	}
	lead := mk("lead", "Lead Software Engineer, Customer Data Platform", "https://jobs.smartrecruiters.com/Grab/744000080123456-lead-software-engineer")
	senior := mk("senior", "Senior Software Engineer, Customer Data Platform", "https://jobs.smartrecruiters.com/Grab/744000080654321-senior-software-engineer")

	_, ok := NewIndex([]models.Job{lead}).FindDuplicate(senior)

	assert.False(t, ok, "one origin lists each vacancy once; two postings from it are two vacancies")
}

func TestFindDuplicate_PostLinkingTwoVacancies_NotFoldedIntoEither(t *testing.T) {
	payments := models.Job{ID: "p", Title: "Senior Go Engineer, Payments", Company: "Acme", Text: "Payments.",
		ApplyURL: "https://jobs.lever.co/acme/2193db3f-77c5-43b8-b030-8f92c9882bf1",
		Sources:  []models.SourceRef{{Source: "lever", Feed: "lever:acme", ExternalID: "acme/2193db3f"}}}
	platform := models.Job{ID: "q", Title: "Staff Go Engineer, Platform", Company: "Acme", Text: "Platform.",
		ApplyURL: "https://jobs.lever.co/acme/8a1c0e2b-0000-4000-8000-000000000000",
		Sources:  []models.SourceRef{{Source: "lever", Feed: "lever:acme", ExternalID: "acme/8a1c0e2b"}}}
	post := models.Job{ID: "h", Title: "Acme is hiring", Text: "Two Go roles at Acme, see links.",
		Sources: []models.SourceRef{{Source: "hn", ExternalID: "9", URL: "https://news.ycombinator.com/item?id=9"}},
		Contacts: []models.Contact{
			{Kind: models.ContactURL, Value: payments.ApplyURL},
			{Kind: models.ContactURL, Value: platform.ApplyURL},
		}}

	_, ok := NewIndex([]models.Job{payments, platform}).FindDuplicate(post)
	assert.False(t, ok, "a post naming two vacancies is neither of them")

	single := post
	single.Contacts = post.Contacts[:1]
	i, ok := NewIndex([]models.Job{payments, platform}).FindDuplicate(single)
	require.True(t, ok, "a post naming one known vacancy is that vacancy")
	assert.Equal(t, 0, i)
}

func TestFindDuplicate_HimalayasRepostUnderTwoSlugs_Merged(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id, slug string, at time.Time) models.Job {
		u := "https://himalayas.app/companies/point-wild/jobs/" + slug
		return models.Job{ID: id, Title: "Backend Architect - Golang", Company: "Point Wild", PostedAt: at, ApplyURL: u,
			Text:    "Point Wild is hiring a backend architect. Golang, PostgreSQL, Kafka, Kubernetes. Remote in the US.",
			Sources: []models.SourceRef{{Source: "himalayas", Feed: "himalayas", Name: "Himalayas", ExternalID: u, URL: u, ApplyURL: u}}}
	}
	a := mk("a", "backend-architect-golang-2583443850", day)
	b := mk("b", "backend-architect-golang-4322929943", day.Add(48*time.Hour))

	_, ok := NewIndex([]models.Job{a}).FindDuplicate(b)

	assert.True(t, ok, "a job board lists one role under several slugs")
}

func TestFindDuplicate_OracleSiteAliases_SameRequisitionMerged(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	text := "Dell is hiring a Go engineer for the storage platform. Go, Kubernetes, Linux. Hybrid in Austin."
	mk := func(id, apply string) models.Job {
		return models.Job{ID: id, Title: "Senior Software Engineer", Company: "Dell", Text: text, PostedAt: day, ApplyURL: apply,
			Sources: []models.SourceRef{{Source: "freehire", Feed: "freehire", Name: "freehire · oracle", ExternalID: id,
				URL: "https://freehire.me/jobs/" + id, ApplyURL: apply}}}
	}
	a := mk("a", "https://enterpriseplatform.dell.com/hcmUI/CandidateExperience/en/sites/CX_1001/job/D11137")
	b := mk("b", "https://iawmqy.fa.ocs.oraclecloud.com/hcmUI/CandidateExperience/en/sites/careers/job/D11137")

	_, ok := NewIndex([]models.Job{a}).FindDuplicate(b)

	assert.True(t, ok, "one requisition served under two site aliases of one tenant")
}

func TestFindDuplicate_SameRolePerLocation_OneCard(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	text := "Motive is hiring a Staff Platform Engineer. You will build our Go platform on Kubernetes and AWS. Hybrid. Great benefits and equity."
	mk := func(id, req string) models.Job {
		u := "https://job-boards.greenhouse.io/motive/jobs/" + req
		return models.Job{ID: id, Title: "Staff Platform Engineer", Company: "Motive", Text: text, PostedAt: day, ApplyURL: u,
			Sources: []models.SourceRef{{Source: "greenhouse", Feed: "greenhouse:motive", ExternalID: "motive/" + req, URL: u, ApplyURL: u}}}
	}

	_, ok := NewIndex([]models.Job{mk("a", "7001001")}).FindDuplicate(mk("b", "7001002"))

	assert.True(t, ok, "one role posted per location is one card with several links")
}

func TestFindDuplicate_TwoVacancyIDsOnOneCareersSite_Distinct(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	base := "VK Workspace ищет Go-разработчика в команду почты и календаря. Офис или удалёнка, ДМС, белая зарплата, обучение за счёт компании. "
	mk := func(id, vacancy, reqs string) models.Job {
		return models.Job{ID: id, Title: "Go-разработчик в VK Workspace", Company: "VK", Text: base + reqs, PostedAt: day,
			Contacts: []models.Contact{{Kind: models.ContactURL, Value: "https://team.vk.company/vacancy/" + vacancy + "/"}},
			Sources:  []models.SourceRef{{Source: "telegram", ExternalID: "rabota_golang/" + id}}}
	}

	a := mk("1140", "45427", "Требования: Go от 4 лет, PostgreSQL, Kafka.")
	b := mk("1149", "45372", "Требования: Go и Python от 3 лет, ClickHouse, gRPC.")

	_, ok := NewIndex([]models.Job{a}).FindDuplicate(b)

	assert.False(t, ok, "two vacancy ids on one careers site are two vacancies, even with template text")
}

func TestFindDuplicate_DifferentRequisitionsAcrossShapes_Distinct(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	text := "HPE is hiring an SRE for GreenLake. Python and/or Golang, Kubernetes, on-call. Hybrid in Houston with great benefits for the team."
	workday := models.Job{ID: "a", Title: "Sr. Staff SRE", Company: "HPE", Text: text + " 10+ years.", PostedAt: day,
		ApplyURL: "https://hpe.wd5.myworkdayjobs.com/Jobsathpe/job/Houston/Sr-Staff-SRE_1200147",
		Sources:  []models.SourceRef{{Source: "workday", Feed: "workday:hpe|wd5|Jobsathpe", ExternalID: "hpe/1200147"}}}
	phenom := models.Job{ID: "b", Title: "SRE Staff", Company: "HPE", Text: text + " 6+ years.", PostedAt: day,
		ApplyURL: "https://careers.hpe.com/us/en/job/1200151/SRE-Staff",
		Sources:  []models.SourceRef{{Source: "freehire", Feed: "freehire", Name: "freehire · phenom", ExternalID: "sre-staff-x", ApplyURL: "https://careers.hpe.com/us/en/job/1200151/SRE-Staff"}}}

	_, ok := NewIndex([]models.Job{workday}).FindDuplicate(phenom)
	assert.False(t, ok, "requisition 1200151 is not 1200147")

	same := phenom
	same.ApplyURL = "https://careers.hpe.com/us/en/job/1200147/Sr-Staff-SRE"
	same.Sources = []models.SourceRef{{Source: "freehire", Feed: "freehire", Name: "freehire · phenom", ExternalID: "sre-y", ApplyURL: same.ApplyURL}}
	_, ok = NewIndex([]models.Job{workday}).FindDuplicate(same)
	assert.True(t, ok, "the careers-site copy of requisition 1200147 is the Workday posting")
}
