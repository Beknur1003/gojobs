package extract

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/Beknur1003/gojobs/internal/models"
)

// RulesVersion names the current classification rules. Bump it whenever a
// rule in this package changes what counts as a Go vacancy: caches of roles
// judged under other rules are then dropped and the roles judged again.
const RulesVersion = "2026-10-01.6"

// Patterns marked "lowercase" run on strings.ToLower(text) with case-sensitive
// syntax: RE2 case-folds every rune for case-insensitive patterns, which is
// slow over long company descriptions.
var (
	// Unambiguous: nobody writes "golang" or "Go-разработчик" by accident. Lowercase.
	goStrong = regexp.MustCompile(`golang|#go\b|\bgo[\s-]?(?:developer|engineer|dev\b|backend|programmer)|(?:^|[^\p{L}])go[\s-]?(?:разработчик|программист|бэкенд|бекенд|разработк)|(?:developer|engineer|разработчик\p{L}*|программист\p{L}*)\s+(?:на\s+|in\s+)?go(?:[^\p{L}-]|$)|(?:^|[^\p{L}])на\s+go(?:[^\p{L}-]|$)|\(go\)`)

	// "Go" as a capitalized word: the language in "Go, Python, k8s", but also
	// "Go live" or "go-to-market", which the follow check rules out.
	goWord  = regexp.MustCompile(`\bGo\b`)
	goUpper = regexp.MustCompile(`\bGO\b`)
	// Phrases that are never the language, in any case: titles are Title Case
	// ("Go To Market Manager", "Go-Live Coordinator"). Matched lowercased.
	goPhrase   = regexp.MustCompile(`^(?:-to\b|-live\b|-getter\b|-go\b|\s*/\s*no\b|\s+(?:to|live|ahead)\b)`)
	goGoBefore = regexp.MustCompile(`(?i)(?:go|no)-$`) // "Go-Go", "No-Go"
	// Ordinary verbs after a sentence-initial "Go" ("Go beyond", "Go for it").
	// Matched case-sensitively: in a Title Case title "Go Back-End Developer"
	// and "Go Team Lead" are the language.
	goNotLang = regexp.MustCompile(`^\s+(?:through|beyond|back|out|on|deep|further|above|for|from|with you|get|big|fast|far|wrong|global|into|over|public|after)\b`)
	// Games named "... Go" ("Monopoly Go!", "Pokémon Go").
	goGameBefore = regexp.MustCompile(`(?i)(?:monopoly|pok[eé]mon)\s*$`)
	letsBefore   = regexp.MustCompile(`(?i)let'?s\s*$`) // 8 bytes of input: (?i) is fine here

	// Another language as the subject of the role ("Python Developer"). Titles are short.
	otherLangTitle = regexp.MustCompile(`(?i)python|java\b|php|rust|c#|c\+\+|salesforce|sales engineer|pre-?sales|support engineer|technical support|customer success|solutions? (?:architect|engineer|consultant)|mechanical|electrical|hardware engineer|people manager|\.net|node|frontend|front-end|фронтенд|ios|android|1c|1с|qa\b|тестировщик|ruby|scala|kotlin|swift|flutter|react|vue|angular|data scientist|analyst|аналитик|designer|дизайнер`)

	engineeringTitle = regexp.MustCompile(`(?i)engineer|developer|programmer|desarrollador|programador|desenvolvedor|ingeniero|engenheiro|разработчик|программист|инженер|\bsre\b|devops|architect|архитектор|lead|лид|\bcto\b|backend|back-end|бэкенд|бекенд|fullstack|full-stack|platform|infrastructure|software|\bswe\b|golang`)
)

// normalizeDashes maps the Unicode hyphens titles use ("Go‑To‑Market") to "-".
func normalizeDashes(s string) string {
	return strings.NewReplacer("\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-").Replace(s)
}

// goMentions counts places where s names the Go language.
func goMentions(s string) (strong bool, words int) {
	if goStrong.MatchString(strings.ToLower(s)) {
		return true, 0
	}
	// An all-caps title ("SENIOR GO", "DESARROLLADOR GO") writes the
	// language as "GO".
	if !strings.ContainsFunc(s, unicode.IsLower) && goUpper.MatchString(s) {
		words++
	}
	for _, loc := range goWord.FindAllStringIndex(s, -1) {
		rest := s[loc[1]:min(len(s), loc[1]+16)]
		before := s[max(0, loc[0]-10):loc[0]]
		if goPhrase.MatchString(strings.ToLower(normalizeDashes(rest))) || goNotLang.MatchString(rest) ||
			letsBefore.MatchString(before) || goGameBefore.MatchString(before) || goGoBefore.MatchString(before) {
			continue
		}
		words++
	}
	return false, words
}

// MayBeGo is a cheap pre-check before full extraction: company boards return
// thousands of roles and most never mention Go at all.
func MayBeGo(p models.Posting) bool {
	if p.GoOnly {
		return true
	}
	for _, t := range p.Tags {
		if strings.Contains(strings.ToLower(t), "go") {
			return true
		}
	}
	s := p.Title + "\n" + p.Text
	return goWord.MatchString(s) || goStrong.MatchString(strings.ToLower(s))
}

// CandidateTitle is the title-only check a company board gets before its
// descriptions are downloaded: a role that names Go, or an engineering role
// that is not about another language. IsGo applies the same title rules.
func CandidateTitle(title string) bool {
	if strong, words := goMentions(title); strong || words > 0 {
		return true
	}
	return engineeringTitle.MatchString(title) && !otherLangTitle.MatchString(title)
}

// Candidate is CandidateTitle plus a cheap look at the text, for sources that
// return full descriptions in one response: what fails it is dropped before
// it costs memory.
func Candidate(title, text string) bool {
	return CandidateTitle(title) && MayBeGo(models.Posting{Title: title, Text: text})
}

// IsGo decides whether the posting is a Go role. Sources that already
// filtered to Go get the benefit of the doubt; open sources must show it.
func IsGo(p models.Posting, title string) bool {
	for _, t := range p.Tags {
		if lt := strings.ToLower(t); lt == "go" || lt == "golang" || strings.Contains(lt, "golang") {
			return true
		}
	}

	titleStrong, titleWords := goMentions(title)
	if titleStrong || titleWords > 0 {
		return true
	}
	bodyStrong, bodyWords := goMentions(p.Text + "\n" + p.Hints)

	switch {
	case p.GoOnly:
		// A Go channel's post that does not name Go is still Go, unless its
		// title is plainly about another language.
		return bodyStrong || bodyWords > 0 || !otherLangTitle.MatchString(title)
	case p.Source == "telegram":
		return bodyStrong || (bodyWords > 0 && !otherLangTitle.MatchString(title))
	default:
		// Company boards list every role. Require an engineering title and a
		// real Go signal, not one "Go" lost in a list of ten languages.
		if !engineeringTitle.MatchString(title) || otherLangTitle.MatchString(title) {
			return false
		}
		return bodyStrong || bodyWords >= 2
	}
}

// Lowercase.
var (
	// Course and webinar promos that channels run between vacancies. Words a
	// vacancy's perks also use ("обучение", "бесплатные обеды") are left out.
	promoWords = []string{"вебинар", "интенсив", "скидк", "промокод", "эфир", "зарегистр", "регистрац", "мастер-класс",
		"буткемп", "webinar", "discount", "sign up", "успей", "по цене", "токен"}
	vacancyWords = []string{"ваканси", "требован", "обязанност", "зарплат", "вилк", "оклад", "откликн", "резюме",
		"responsibilit", "requirement", "salary", "мы предлагаем", "we offer", "условия", "ищем", "в команду"}
	adHosts = []string{"clc.to", "ord.vk.com"}

	resumeRe  = regexp.MustCompile(`#резюме|#resume|#cv\b|#ищуработу|#lookingforjob|ищу работу|ищу вакансию|looking for a job|open to work|#opentowork`)
	adRe      = regexp.MustCompile(`(?m)#реклама|erid[:=\s]|#промо|#promo|#ad\b|на правах рекламы|о рекламодателе|^\s*реклама[.\s]`)
	vacancyRe = regexp.MustCompile(`ваканси|vacancy|#job|hiring|ищем|ищу\s|в поиске|требует|требован|обязанност|responsibilit|requirement|looking for|зарплат|(?:^|[^\p{L}])зп(?:[^\p{L}]|$)|з/п|salary|вилк|оклад|откликн|присылай|пишите|apply|резюме|cv\b|задачи|tasks|мы предлагаем|we offer|условия`)
)

// IsVacancy filters Telegram channel noise: résumés, ads, digests and chat.
// Board and ATS postings are vacancies by construction.
func IsVacancy(p models.Posting) bool {
	if p.Source != "telegram" {
		return true
	}
	if len([]rune(p.Text)) < 150 {
		return false
	}
	text := strings.ToLower(p.Text)
	if resumeRe.MatchString(firstRunes(text, 300)) || adRe.MatchString(text) {
		return false
	}
	promo := 0
	for _, l := range p.Links {
		if strings.Contains(l, "erid=") {
			return false // labelled advertising: Russian ad law marks every ad link
		}
		for _, h := range adHosts {
			if strings.Contains(l, "//"+h+"/") {
				promo += 2 // channels hide ad links behind their own shorteners
			}
		}
	}
	// A vacancy names a role near the top; digests, warnings and greetings
	// ("Коллеги, доброго дня!") do not.
	if !hasRoleLine(p.Text, roleLines) {
		return false
	}
	// A digest lists many roles, each a short title line; one card for it
	// would mix companies, contacts and links.
	if shortRoleLines(p.Text) >= digestRoles {
		return false
	}
	// A course promo talks about the job market too. Without the bones of a
	// vacancy one promo word is enough; with them it has to clearly win.
	promo += countAny(text, promoWords)
	if !countsAsVacancy(text) {
		return promo == 0 && !strings.Contains(text, "курс") && vacancyRe.MatchString(text)
	}
	if promo >= 3 && promo > countAny(text, vacancyWords) {
		return false
	}
	return true
}

// structureWords are what every real vacancy has at least one of: duties,
// requirements, pay, or "we are looking for".
var structureWords = []string{"требован", "обязанност", "задачи", "responsibilit", "requirement", "you will", "ищем",
	"looking for", "зарплат", "вилк", "оклад", "salary", "мы предлагаем", "we offer", "опыт ", "experience"}

func countsAsVacancy(lower string) bool { return countAny(lower, structureWords) > 0 }

// digestRoles is how many distinct short role lines make a post a digest.
const digestRoles = 5

func shortRoleLines(text string) int {
	seen := map[string]bool{}
	for _, l := range cleanLines(text, 200) {
		if len([]rune(l)) <= 90 && isRoleLine(l) {
			seen[strings.ToLower(l)] = true
		}
	}
	return len(seen)
}

func hasRoleLine(text string, n int) bool {
	for _, l := range cleanLines(text, n) {
		if isRoleLine(l) {
			return true
		}
	}
	return false
}

// countAny counts how many of words occur in s.
func countAny(s string, words []string) int {
	n := 0
	for _, w := range words {
		if strings.Contains(s, w) {
			n++
		}
	}
	return n
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
