package extract

import (
	"regexp"
	"strings"
)

var (
	// Backend languages a Go role is weighed against, each with a plain
	// substring that must occur before the regexp is worth running over a
	// long description. Lowercase. JavaScript and TypeScript are left out: a
	// Go backend with a React frontend is still a Go role.
	otherLangs = []struct {
		needle string
		re     *regexp.Regexp
	}{
		{"python", regexp.MustCompile(`\bpython`)},
		{"java", regexp.MustCompile(`\bjava\b`)},
		{"kotlin", regexp.MustCompile(`\bkotlin`)},
		{"php", regexp.MustCompile(`\bphp`)},
		{"ruby", regexp.MustCompile(`\bruby\b`)},
		{"rust", regexp.MustCompile(`\brust\b`)},
		{"c#", regexp.MustCompile(`c#`)},
		{"net", regexp.MustCompile(`\.net\b|\bdotnet`)},
		{"c++", regexp.MustCompile(`c\+\+`)},
		{"scala", regexp.MustCompile(`\bscala\b`)},
		{"elixir", regexp.MustCompile(`\belixir`)},
		{"node", regexp.MustCompile(`node\.?js`)},
	}
	// The text saying it outright: "work primarily in Go", "Go (preferably)",
	// "основной язык — Go". Lowercase.
	goPrimary = regexp.MustCompile(`(?:primarily|mainly|mostly|predominantly|preferably)\s+(?:in\s+|with\s+|using\s+)?(?:go|golang)(?:[^\p{L}]|$)|(?:go|golang)\s*\(?preferabl|(?:primary|main|core)\s+(?:programming\s+|backend\s+)?language\s+(?:is\s+)?(?:go|golang)(?:[^\p{L}]|$)|основн\p{L}*\s+(?:язык\p{L}*\s+)?(?:—|-|:)?\s*(?:go|golang)(?:[^\p{L}]|$)`)

	// A language in a title, to see whether the title names Go first:
	// "Go/Python Developer" is a Go role, "Тимлид (python/go)" is not. Lowercase.
	titleLang = regexp.MustCompile(`python|\bjava\b|kotlin|php|ruby|rust|c#|c\+\+|\.net|node|scala|elixir|typescript|javascript`)
)

// GoMain reports whether Go is the main language of a role rather than one
// of several: the title names Go before any other language, or the text
// names Go far more often than any other backend language. It is a label
// readers filter by, so when in doubt it says no.
func GoMain(title, text string) bool {
	if at := goAt(title); at >= 0 {
		other := titleLang.FindStringIndex(strings.ToLower(title))
		return other == nil || at < other[0]
	}
	if otherLangTitle.MatchString(title) {
		return false
	}
	g := goCount(text)
	if g == 0 {
		return false
	}
	lower := strings.ToLower(text)
	if mayBePrimary(lower) && goPrimary.MatchString(lower) {
		return true
	}
	most := 0
	for _, l := range otherLangs {
		if strings.Contains(lower, l.needle) {
			most = max(most, len(l.re.FindAllStringIndex(lower, -1)))
		}
	}
	return most == 0 || g >= 3 && g >= 3*most
}

// mayBePrimary is a substring check that spares most descriptions the
// goPrimary regexp, which has no literal prefix to search for.
func mayBePrimary(lower string) bool {
	for _, w := range []string{"primarily", "mainly", "mostly", "predominantly", "preferabl", "language is go", "основн"} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// goAt is the offset of the first place s names Go, or -1.
func goAt(s string) int {
	at := -1
	first := func(i int) {
		if i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	if words := goWords(s); len(words) > 0 {
		first(words[0])
	}
	lower := strings.ToLower(s)
	if len(lower) == len(s) {
		if loc := goStrong.FindStringIndex(lower); loc != nil {
			first(loc[0])
		}
	}
	return at
}

// goCount counts the places s names Go: "golang", the capitalized word, and
// the lowercase "go" of Russian posts ("опыт разработки на go").
func goCount(s string) int {
	lower := strings.ToLower(s)
	n := strings.Count(lower, "golang") + len(goWords(s))
	// English posts write "Go"; scanning them for a lowercase "go" costs the
	// most and finds nothing. Offsets below must also line up with s.
	if len(lower) != len(s) || !strings.ContainsFunc(s, isCyrillic) {
		return n
	}
	for _, loc := range goStrong.FindAllStringIndex(lower, -1) {
		m := lower[loc[0]:loc[1]]
		if strings.Contains(m, "golang") {
			continue
		}
		if i := strings.Index(m, "go"); i >= 0 && s[loc[0]+i:loc[0]+i+2] == "go" {
			n++ // written in lowercase, so goWords did not see it
		}
	}
	return n
}

func isCyrillic(r rune) bool { return r >= 'а' && r <= 'я' || r >= 'А' && r <= 'Я' }
