package extract

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	hashtagRe   = regexp.MustCompile(`#[\p{L}\p{N}_]+`)
	titleLabel  = regexp.MustCompile(`(?i)^(?:вакансия|позиция|должность|роль|position|role|job title|title|vacancy|open position)\s*[:—–-]\s*(.+)$`)
	companyLbl  = regexp.MustCompile(`(?i)^(?:компания|company|работодатель|employer|наниматель)\s*[:—–-]\s*(.+)$`)
	roleLine    = regexp.MustCompile(`(?i)developer|разработчик|engineer|инженер|программист|golang|\bgo\b|lead|лид|backend|бэкенд|бекенд|architect|архитектор|\bsre\b|devops|\bcto\b|head of|руководител|стаж[её]р|intern`)
	leadingJunk = regexp.MustCompile(`^[\s•·*\-–—>|:,!#]+`) // not "." : ".NET"

	// "Title - Company" on one line: the tail is a short name.
	titleCompany = regexp.MustCompile(`^(.{6,}?)\s+[-–—]\s+([^-–—]{2,30})$`)
	// The line after the title often opens with the company:
	// "в Джум — международная…", "Rocket Tech — уже 13 лет…", "CodiLime is an IT company…".
	companyLead = []*regexp.Regexp{
		regexp.MustCompile(`^(?:в|во|at)\s+(.{2,40}?)\s+[—–-]\s`),
		regexp.MustCompile(`^([\p{Lu}0-9][^—–:.!?]{1,38}?)\s+[—–]\s`),
		regexp.MustCompile(`^([\p{Lu}][\p{L}0-9.&' ]{1,38}?)\s+(?:is|are)\s+(?:an?|the|one of)\s`),
	}
	spaces     = regexp.MustCompile(`\s+`)
	mentionsRe = regexp.MustCompile(`@[A-Za-z][A-Za-z0-9_]{3,31}|https?://\S+|t\.me/\S+`)
)

// TitleAndCompany guesses the role and the employer from a free-form post.
// Telegram posts have no fields, but most follow the same few layouts:
// "Вакансия: X", or a company line followed by a role line, under hashtags.
func TitleAndCompany(text string) (title, company string) {
	lines := cleanLines(text, 14)

	for _, l := range lines {
		if m := companyLbl.FindStringSubmatch(l); m != nil && company == "" {
			company = shorten(cutCompany(m[1]), 60)
		}
		if m := titleLabel.FindStringSubmatch(l); m != nil && title == "" {
			title = shorten(m[1], 120)
		}
	}
	if title != "" {
		return title, company
	}

	for i, l := range lines {
		if i >= roleLines {
			break
		}
		if !isRoleLine(l) || len([]rune(l)) > 140 || companyLbl.MatchString(l) {
			continue
		}
		title = l
		// "Golang-разработчик | Офис (Алматы) | до 350 000 ₽": the role is the
		// first segment, the rest is format, place and pay.
		if head, _, found := strings.Cut(title, " | "); found && roleLine.MatchString(head) {
			title = head
		}
		if m := titleCompany.FindStringSubmatch(title); m != nil && company == "" && looksLikeName(m[2]) && len(strings.Fields(m[2])) <= 3 {
			title, company = m[1], strings.TrimSpace(m[2])
		}
		// A short line right above the role is very often the company name.
		if company == "" && i > 0 && looksLikeName(lines[i-1]) {
			company = lines[i-1]
		}
		if company == "" && i+1 < len(lines) {
			company = companyFromLead(lines[i+1])
		}
		return shorten(title, 120), company
	}

	if len(lines) > 0 {
		title = shorten(lines[0], 100)
	}
	return title, company
}

// Summary is the opening of the post without hashtags and the title lines,
// for the card in the feed.
func Summary(text, title, company string, limit int) string {
	var parts []string
	for _, l := range cleanLines(text, 40) {
		if l == title || l == company || titleLabel.MatchString(l) || companyLbl.MatchString(l) {
			continue
		}
		parts = append(parts, l)
		if len([]rune(strings.Join(parts, " "))) > limit {
			break
		}
	}
	return shorten(strings.Join(parts, " "), limit)
}

// cleanLines returns up to n non-empty lines stripped of emoji, hashtags and
// bullet characters.
func cleanLines(text string, n int) []string {
	var out []string
	for _, raw := range strings.Split(text, "\n") {
		l := CleanLine(raw)
		if l == "" {
			continue
		}
		out = append(out, l)
		if len(out) == n {
			break
		}
	}
	return out
}

func CleanLine(s string) string {
	s = hashtagRe.ReplaceAllString(s, " ")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\u200d' || r == '\ufe0f' || r == '\u20e3': // ZWJ, emoji style, keycap
			return -1
		case unicode.Is(unicode.So, r) || unicode.Is(unicode.Co, r):
			return -1
		case r == '*' || r == '`': // keep '_': it is part of @handles
			return -1
		}
		return r
	}, s)
	s = leadingJunk.ReplaceAllString(s, "")
	s = spaces.ReplaceAllString(s, " ")
	return strings.TrimRight(strings.TrimSpace(s), ":;,—–- ")
}

// roleLines is how deep into a post the role is looked for.
const roleLines = 12

// isRoleLine ignores handles and links: "Обсуждение: @devops_jobs" is not a
// DevOps role.
func isRoleLine(l string) bool {
	return roleLine.MatchString(mentionsRe.ReplaceAllString(l, " "))
}

func companyFromLead(line string) string {
	for _, re := range companyLead {
		if m := re.FindStringSubmatch(line); m != nil && !roleLine.MatchString(m[1]) {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func looksLikeName(s string) bool {
	r := []rune(s)
	return len(r) >= 2 && len(r) <= 40 &&
		len(strings.Fields(s)) <= 5 &&
		!strings.ContainsAny(s, ":?!") &&
		!roleLine.MatchString(s) &&
		!unicode.IsDigit(r[0])
}

// cutCompany drops the description often glued to the name:
// "Acme — финтех-стартап" -> "Acme".
func cutCompany(s string) string {
	for _, sep := range []string{" — ", " – ", " - ", ", ", " (", ". "} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

func shorten(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= limit {
		return string(r)
	}
	cut := string(r[:limit])
	if i := strings.LastIndex(cut, " "); i > limit/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, ",.;:—– ") + "…"
}
