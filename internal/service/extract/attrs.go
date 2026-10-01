package extract

import (
	"regexp"
	"strings"

	"github.com/Beknur1003/gojobs/internal/models"
)

// Every rule in this file runs on lowercased text without (?i): Go's regexp
// case-folds each rune through unicode tables, which made (?i) over long ATS
// descriptions the single biggest cost of a run.
var (
	remoteRe = regexp.MustCompile(`удал[её]н|удаленк|удалёнк|дистанц|remote|из любой (?:точки|страны)|work from anywhere|wfh|home[- ]based|#удаленка|#remote`)
	hybridRe = regexp.MustCompile(`гибрид|hybrid`)
	officeRe = regexp.MustCompile(`в офисе|офис(?:е|а|[^\p{L}]|$)|#офис|on-?site|in[- ]office|office-based|from the office`)
	// "не удалённо", "no remote", "без удалёнки" flip the meaning of a match.
	// RE2's \b is ASCII-only, so Cyrillic words get explicit [^\p{L}] edges.
	negationRe = regexp.MustCompile(`(?:^|[^\p{L}])(?:не|no|not|без|non)[\s-]*$`)
	// "удалёнки нет", "remote is not possible": negation after the word.
	negationAfterRe = regexp.MustCompile(`^\p{L}*\s*(?:нет|не\s+предусмотр|невозможн|не\s+рассматрива|(?:is\s+)?not\s+(?:possible|available|an option))`)
)

// Formats reads the work format. Structured remote boards pass remote=true.
// head is the start of the post (where formats are stated), not the body,
// which mentions "our office" in benefits even for remote roles.
func Formats(head string, remote bool) []models.WorkFormat {
	head = strings.ToLower(head)
	var out []models.WorkFormat
	if remote || matchNotNegated(remoteRe, head) {
		out = append(out, models.FormatRemote)
	}
	if matchNotNegated(hybridRe, head) {
		out = append(out, models.FormatHybrid)
	}
	if matchNotNegated(officeRe, head) && !(remote && len(out) == 1) {
		out = append(out, models.FormatOffice)
	}
	return out
}

func matchNotNegated(re *regexp.Regexp, s string) bool {
	for _, loc := range re.FindAllStringIndex(s, -1) {
		before := s[max(0, loc[0]-12):loc[0]]
		if !negationRe.MatchString(before) && !negationAfterRe.MatchString(s[loc[1]:]) {
			return true
		}
	}
	return false
}

var gradeRules = []struct {
	grade models.Grade
	re    *regexp.Regexp
}{
	{models.GradeIntern, regexp.MustCompile(`стаж[её]р|стажировк|\bintern(?:ship)?\b|\btrainee\b`)},
	{models.GradeJunior, regexp.MustCompile(`\bjunior|\bjun\b|джун|младш|entry[- ]level`)},
	{models.GradeMiddle, regexp.MustCompile(`\bmiddle|\bmid\b|mid[- ]level|мидл|миддл`)},
	{models.GradeSenior, regexp.MustCompile(`\bsenior|\bsr\.?\s|сень[её]р|синь[её]р|старш|\bsnr\b`)},
	{models.GradeLead, regexp.MustCompile(`team\s?lead|tech\s?lead|teamlead|techlead|тимлид|техлид|\blead\b|(?:^|[^\p{L}])лид(?:[^\p{L}]|$)|head of|principal|\bstaff\b|руководител`)},
}

// Grades reads seniority from the title first; the body is only consulted
// when the title says nothing, because bodies say "mentor junior engineers".
func Grades(title, head string) []models.Grade {
	if g := gradesIn(title); len(g) > 0 {
		return g
	}
	return gradesIn(head)
}

func gradesIn(s string) []models.Grade {
	s = strings.ToLower(s)
	var out []models.Grade
	for _, r := range gradeRules {
		if r.re.MatchString(s) {
			out = append(out, r.grade)
		}
	}
	return out
}

var (
	englishCtx   = regexp.MustCompile(`англ|english|eng\b`)
	cefrRe       = regexp.MustCompile(`\b([abc][12])\b\+?`)
	englishWords = []struct {
		re    *regexp.Regexp
		level string
	}{
		{regexp.MustCompile(`pre[- ]?intermediate`), "a2"},
		{regexp.MustCompile(`upper[- ]?intermediate`), "b2"},
		{regexp.MustCompile(`intermediate`), "b1"},
		{regexp.MustCompile(`advanced|fluent|свободн|native|proficien`), "c1"},
	}
)

// English returns the CEFR level a post asks for, or "" if it does not say.
func English(text string) string {
	for _, line := range strings.Split(strings.ToLower(text), "\n") {
		loc := englishCtx.FindStringIndex(line)
		if loc == nil {
			continue
		}
		// Look at a window around the word "English", not the whole line.
		window := line[max(0, loc[0]-40):min(len(line), loc[1]+60)]
		if m := cefrRe.FindStringSubmatch(window); m != nil {
			return m[1]
		}
		for _, w := range englishWords {
			if w.re.MatchString(window) {
				return w.level
			}
		}
	}
	return ""
}

var employmentRules = []struct {
	kind models.Employment
	re   *regexp.Regexp
}{
	{models.EmploymentFull, regexp.MustCompile(`full[- ]?time|полн(?:ая|ый) (?:занятость|день|рабочий)|#fulltime|фуллтайм|фулл-тайм`)},
	{models.EmploymentPart, regexp.MustCompile(`part[- ]?time|частичн|неполн|парт-?тайм|#parttime`)},
	{models.EmploymentContract, regexp.MustCompile(`\bcontract(?:or)?\b|\bb2b\b|(?:^|[^\p{L}])(?:ип|гпх)(?:[^\p{L}]|$)|самозанят|freelance|фриланс|договор подряда`)},
}

func Employment(s string) []models.Employment {
	s = strings.ToLower(s)
	var out []models.Employment
	for _, r := range employmentRules {
		if r.re.MatchString(s) {
			out = append(out, r.kind)
		}
	}
	return out
}

var relocationRe = regexp.MustCompile(`релок|relocat|переезд|visa sponsor|визов\p{L}* поддерж|помощь с визой`)

func Relocation(s string) bool { return relocationRe.MatchString(strings.ToLower(s)) }

// Lang reports the dominant script of the post: "ru" or "en".
func Lang(s string) string {
	var cyr, lat int
	for _, r := range s {
		switch {
		case r >= 'а' && r <= 'я', r >= 'А' && r <= 'Я', r == 'ё', r == 'Ё':
			cyr++
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			lat++
		}
	}
	if cyr*2 > lat {
		return "ru"
	}
	return "en"
}

// stack is the vocabulary of technologies shown as chips, in display order.
// Go itself is implied on this board and not listed. Rules with exact=true
// match the original case ("NATS", "AWS"); the rest match lowercased text.
var stack = []struct {
	name  string
	re    *regexp.Regexp
	exact bool
}{
	{"PostgreSQL", regexp.MustCompile(`postgre|\bpg\b|постгр`), false},
	{"MySQL", regexp.MustCompile(`\bmysql|mariadb`), false},
	{"ClickHouse", regexp.MustCompile(`clickhouse|кликхаус`), false},
	{"MongoDB", regexp.MustCompile(`mongo`), false},
	{"Redis", regexp.MustCompile(`\bredis|\bvalkey`), false},
	{"Kafka", regexp.MustCompile(`kafka|кафк`), false},
	{"RabbitMQ", regexp.MustCompile(`rabbit`), false},
	{"NATS", regexp.MustCompile(`\bNATS\b`), true},
	{"Elasticsearch", regexp.MustCompile(`elastic|opensearch`), false},
	{"Cassandra", regexp.MustCompile(`cassandra|scylla`), false},
	{"Tarantool", regexp.MustCompile(`tarantool`), false},
	{"gRPC", regexp.MustCompile(`\bgrpc|protobuf`), false},
	{"GraphQL", regexp.MustCompile(`graphql`), false},
	{"Kubernetes", regexp.MustCompile(`kubernetes|\bk8s|кубер`), false},
	{"Docker", regexp.MustCompile(`docker|докер`), false},
	{"Terraform", regexp.MustCompile(`terraform`), false},
	{"AWS", regexp.MustCompile(`\bAWS\b|Amazon Web Services`), true},
	{"GCP", regexp.MustCompile(`\bGCP\b|Google Cloud`), true},
	{"Azure", regexp.MustCompile(`\bazure`), false},
	{"Prometheus", regexp.MustCompile(`prometheus`), false},
	{"Grafana", regexp.MustCompile(`grafana`), false},
	{"OpenTelemetry", regexp.MustCompile(`opentelemetry|\botel\b|jaeger`), false},
	{"Microservices", regexp.MustCompile(`microservice|микросервис`), false},
	{"Highload", regexp.MustCompile(`high-?load|хайлоад|высоконагруж`), false},
	{"Linux", regexp.MustCompile(`\blinux`), false},
	{"CI/CD", regexp.MustCompile(`ci/cd|\bci\b|github actions|gitlab ci`), false},
	{"Python", regexp.MustCompile(`\bpython`), false},
	{"Rust", regexp.MustCompile(`\bRust\b|\brustlang\b`), true},
	{"Java", regexp.MustCompile(`\bjava\b`), false},
	{"Kotlin", regexp.MustCompile(`kotlin`), false},
	{"PHP", regexp.MustCompile(`\bphp`), false},
	{"C++", regexp.MustCompile(`c\+\+`), false},
	{"TypeScript", regexp.MustCompile(`typescript|\bts\b`), false},
	{"Node.js", regexp.MustCompile(`node\.?js`), false},
	{"React", regexp.MustCompile(`\breact\b`), false},
	{"Blockchain", regexp.MustCompile(`blockchain|блокчейн|web3|ethereum|solidity|crypto|крипт`), false},
	{"AI/LLM", regexp.MustCompile(`\bllm|\bai\b|machine learning|\bml\b|нейросет`), false},
	{"FinTech", regexp.MustCompile(`fintech|финтех|payment|платеж|банк|bank`), false},
}

// Stack returns the known technologies mentioned in s.
func Stack(s string) []string {
	lower := strings.ToLower(s)
	var out []string
	for _, t := range stack {
		in := lower
		if t.exact {
			in = s
		}
		if t.re.MatchString(in) {
			out = append(out, t.name)
		}
	}
	return out
}
