package extract

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Beknur1003/gojobs/internal/models"
)

// usdPerUnit is a fixed, approximate rate table. It only feeds the "from N $"
// filter, and the site labels every converted figure as approximate. Refresh
// it when a currency moves enough to matter.
var usdPerUnit = map[string]float64{
	"USD": 1, "USDT": 1, "BRL": 0.18, "SGD": 0.74, "DKK": 0.145, "NOK": 0.093, "JPY": 0.0067, "BDT": 0.0083,
	"MXN": 0.055, "ZAR": 0.055, "HKD": 0.128, "NZD": 0.6, "KRW": 0.00073, "HUF": 0.0027, "RON": 0.22, "EUR": 1.08, "GBP": 1.27, "CHF": 1.12, "CAD": 0.73, "AUD": 0.66,
	"RUB": 1.0 / 82, "KZT": 1.0 / 510, "UAH": 1.0 / 41, "BYN": 1.0 / 3.3, "PLN": 0.25,
	"AMD": 1.0 / 390, "GEL": 0.37, "AED": 0.27, "TRY": 1.0 / 34, "RSD": 1.0 / 108, "UZS": 1.0 / 12700,
	"ILS": 0.27, "CZK": 0.043, "SEK": 0.095, "INR": 0.012,
}

// Currency markers, longest first so "USDT" is not read as "USD".
var currencyWords = []struct {
	re   *regexp.Regexp
	code string
}{
	{regexp.MustCompile(`(?i)usdt`), "USDT"},
	// Dollar-sign currencies other than USD come before "$" is read as USD.
	{regexp.MustCompile(`(?i)r\$|\bbrl\b|reais`), "BRL"},
	{regexp.MustCompile(`(?i)a\$|au\$`), "AUD"},
	{regexp.MustCompile(`(?i)s\$|\bsgd\b`), "SGD"},
	{regexp.MustCompile(`(?i)\bdkk\b`), "DKK"},
	{regexp.MustCompile(`(?i)\bnok\b`), "NOK"},
	{regexp.MustCompile(`(?i)\bsek\b`), "SEK"},
	{regexp.MustCompile(`(?i)\bjpy\b|¥|yen\b`), "JPY"},
	{regexp.MustCompile(`(?i)\bbdt\b|৳`), "BDT"},
	{regexp.MustCompile(`(?i)\bmxn\b`), "MXN"},
	{regexp.MustCompile(`(?i)\bzar\b`), "ZAR"},
	{regexp.MustCompile(`(?i)\bhkd\b|hk\$`), "HKD"},
	{regexp.MustCompile(`(?i)\bnzd\b|nz\$`), "NZD"},
	{regexp.MustCompile(`(?i)\bkrw\b|₩`), "KRW"},
	{regexp.MustCompile(`(?i)\bhuf\b`), "HUF"},
	{regexp.MustCompile(`(?i)\bron\b`), "RON"},
	{regexp.MustCompile(`(?i)\binr\b|₹`), "INR"},
	{regexp.MustCompile(`(?i)\$|usd\b|долл\p{L}*|бакс\p{L}*`), "USD"},
	{regexp.MustCompile(`(?i)€|eur\b|euro\b|евро`), "EUR"},
	{regexp.MustCompile(`(?i)£|gbp\b`), "GBP"},
	{regexp.MustCompile(`(?i)₽|rub\b|руб\p{L}*|р\.|rur\b`), "RUB"},
	{regexp.MustCompile(`(?i)₸|kzt\b|тенге|тг\.?(?:\s|$)`), "KZT"},
	{regexp.MustCompile(`(?i)₴|uah\b|грн`), "UAH"},
	{regexp.MustCompile(`(?i)byn\b|бел\.?\s*руб`), "BYN"},
	{regexp.MustCompile(`(?i)pln\b|zł|zl\b`), "PLN"},
	{regexp.MustCompile(`(?i)chf\b`), "CHF"},
	{regexp.MustCompile(`(?i)cad\b|c\$`), "CAD"},
	{regexp.MustCompile(`(?i)aed\b|dirham`), "AED"},
	{regexp.MustCompile(`(?i)gel\b|₾|лари`), "GEL"},
	{regexp.MustCompile(`(?i)amd\b|драм`), "AMD"},
	{regexp.MustCompile(`(?i)rsd\b|динар`), "RSD"},
}

const (
	// Thousands groups may carry cents: "$203,101.00".
	num = `\d{1,3}(?:[ \x{00A0}\x{202F}.,’']\d{3})+(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?`
	// Multipliers are validated after matching (see mult): RE2 has no
	// lookahead, and "300 команд" must not read as 300 thousand.
	mult = `(?:\s?(?:тыс\.?|тысяч\p{L}*|thousand|million|billion|млн\.?|млрд\.?|mln|mio|bn|mn|kk|кк|k|к|m))?`
	cur  = `(?:usdt|usd|r\$|a\$|au\$|s\$|hk\$|nz\$|brl|sgd|dkk|nok|sek|jpy|¥|bdt|৳|mxn|zar|hkd|nzd|krw|₩|huf|ron|inr|₹|eur|gbp|rub|rur|kzt|uah|byn|pln|chf|cad|aed|gel|amd|rsd|\$|€|£|₽|₸|₴|₾|руб\p{L}*|р\.|долл\p{L}*|бакс\p{L}*|евро|тенге|тг\.?|грн|лари|драм|динар\p{L}*)`
)

var (
	// [cur] num[mult] [cur] (- | – | to | до) [cur] num[mult] [cur]
	rangeRe = regexp.MustCompile(`(?i)(` + cur + `)?\s?(` + num + `)(` + mult + `)\s?(` + cur + `)?\s*(?:-|–|—|to|до|and|\.\.)\s*(` + cur + `)?\s?(` + num + `)(` + mult + `)\s?(` + cur + `)?`)
	// "от 300 000 ₽", "до $150k", "up to 6000 EUR", "from €4k", or a bare "5000$"
	singleRe = regexp.MustCompile(`(?i)(от|from|до|up to|upto|starting at|min\.?)?\s*(` + cur + `)?\s?(` + num + `)(` + mult + `)\s?(` + cur + `)?`)

	salaryContext = regexp.MustCompile(`(?i)зп|з/п|зарплат|оклад|вилк|доход|компенсац|оплат|ставк|salary|compensation|pay\b|paid|rate|budget|бюджет|gross|net\b|на руки|base`)
	perYear       = regexp.MustCompile(`(?i)в\s?год|годов|/\s?(?:год|year|yr|y)\b|per\s?(?:year|annum)|annual|a year|p\.?a\.?\b|yearly`)
	perHour       = regexp.MustCompile(`(?i)в\s?час|/\s?(?:час|hour|hr|h)\b|per\s?hour|hourly|an hour`)
	perMonth      = regexp.MustCompile(`(?i)в\s?мес|/\s?(?:мес|month|mo)\b|per\s?month|monthly|a month|ежемесяч`)
)

// Salary finds the first stated pay in s. It only trusts numbers next to a
// currency, or numbers inside a line that talks about pay.
func Salary(s string) models.Salary {
	for _, line := range strings.Split(s, "\n") {
		if sal, ok := salaryInLine(line); ok {
			return sal
		}
	}
	return models.Salary{}
}

// explicitCode returns an ISO code the line spells out next to "$" figures,
// for dollar-sign currencies other than USD.
func explicitCode(line string) string {
	if !strings.Contains(line, "$") {
		return ""
	}
	for _, code := range []string{"CAD", "AUD", "SGD", "NZD", "HKD", "MXN"} {
		if regexp.MustCompile(`\b` + code + `\b`).MatchString(strings.ToUpper(line)) {
			return code
		}
	}
	return ""
}

// moneyHints are cheap substring checks that gate the expensive regexes: a
// line with no digit, no currency mark and no pay word cannot hold a salary.
var moneyHints = []string{
	"$", "€", "£", "₽", "₸", "₴", "₾", "¥", "₩", "₹", "৳", "dkk", "nok", "sek", "jpy", "bdt", "usd", "eur", "gbp", "rub", "rur", "kzt", "uah", "byn", "pln", "chf", "cad", "aed",
	"gel", "amd", "rsd", "руб", "р.", "тенге", "тг", "грн", "долл", "бакс", "евро", "лари", "драм", "динар", "zł",
	"зп", "з/п", "зарплат", "оклад", "вилк", "доход", "компенсац", "оплат", "ставк", "salary", "compensation", "pay",
	"rate", "budget", "бюджет", "gross", "net", "на руки", "base",
}

func mayHoldSalary(line string) bool {
	if !strings.ContainsAny(line, "0123456789") {
		return false
	}
	low := strings.ToLower(line)
	for _, h := range moneyHints {
		if strings.Contains(low, h) {
			return true
		}
	}
	return false
}

// notPay are lines with money that is not the salary: funding, perks and
// allowances. They are skipped unless they also name the pay itself.
var (
	notPay  = regexp.MustCompile(`(?i)funding|raised|valuation|revenue|\barr\b|series [a-e]\b|allowance|stipend|reimburs|perk|voucher|gift card|credit|learning budget|education budget|equipment|home office|daycare|childcare|gym|wellness|insurance|401\(?k|pension|bonus|equity|stock|опцион|бонус|компенсаци\p{L}* (?:спорт|обуч|питан|фитнес)`)
	payWord = regexp.MustCompile(`(?i)salary|base pay|pay range|compensation range|annual (?:base|pay)|wage|зарплат|вилк|оклад|(?:^|[^\p{L}])зп(?:[^\p{L}]|$)|з/п|на руки`)
)

func salaryInLine(line string) (models.Salary, bool) {
	if !mayHoldSalary(line) {
		return models.Salary{}, false
	}
	// A code stated once for the whole line ("$130,000-150,000 CAD").
	lineCode := explicitCode(line)
	if notPay.MatchString(line) && !payWord.MatchString(line) {
		return models.Salary{}, false
	}
	ctx := salaryContext.MatchString(line)
	defaultCur := ""
	if ctx && hasCyrillic(line) {
		defaultCur = "RUB" // "вилка 250-350к" in a Russian post
	}

	for _, m := range rangeRe.FindAllStringSubmatchIndex(line, -1) {
		g := groups(line, m)
		code := firstCurrency(g[1], g[4], g[5], g[8])
		if code == "USD" && lineCode != "" {
			code = lineCode
		}
		if code == "" && !ctx {
			continue
		}
		if code == "" {
			code = defaultCur
		}
		loMult, hiMult := multAt(line, m, 3), multAt(line, m, 7)
		lo := parseAmount(g[2], firstNonEmpty(loMult, hiMult))
		hi := parseAmount(g[6], firstNonEmpty(hiMult, loMult))
		if sal, ok := build(lo, hi, code, line); ok {
			return sal, true
		}
	}

	for _, m := range singleRe.FindAllStringSubmatchIndex(line, -1) {
		g := groups(line, m)
		code := firstCurrency(g[2], g[5])
		if code == "" && !(ctx && g[1] != "") {
			continue
		}
		if code == "" {
			code = defaultCur
		}
		v := parseAmount(g[3], multAt(line, m, 4))
		lo, hi := v, 0
		if p := strings.ToLower(g[1]); p == "до" || strings.HasPrefix(p, "up") {
			lo, hi = 0, v
		}
		if sal, ok := build(lo, hi, code, line); ok {
			return sal, true
		}
	}
	return models.Salary{}, false
}

func build(lo, hi int, code, line string) (models.Salary, bool) {
	if code == "" || (lo == 0 && hi == 0) {
		return models.Salary{}, false
	}
	if lo > 0 && hi > 0 && hi < lo {
		lo, hi = hi, lo
	}
	// "4-6k": the multiplier was written once, after the second number.
	if lo > 0 && hi >= 1000 && lo < 100 && hi/lo >= 100 {
		lo *= 1000
	}

	sal := models.Salary{Min: lo, Max: hi, Currency: code}
	sal.Period = period(line, sal)
	if !plausible(sal) {
		return models.Salary{}, false
	}
	return Monthly(sal), true
}

func period(line string, s models.Salary) models.Period {
	switch {
	case perHour.MatchString(line):
		return models.PeriodHour
	case perYear.MatchString(line):
		return models.PeriodYear
	case perMonth.MatchString(line):
		return models.PeriodMonth
	}
	// Unstated: a figure above ~$25k is a year's pay. A small one is not
	// assumed to be hourly ("$10M raised", "$10 credit"): no period, no salary.
	usd := float64(max(s.Min, s.Max)) * usdPerUnit[s.Currency]
	switch {
	case usd >= 25000:
		return models.PeriodYear
	case usd < 250:
		return ""
	default:
		return models.PeriodMonth
	}
}

// plausible rejects years, phone numbers, headcounts and the like.
func plausible(s models.Salary) bool {
	rate, ok := usdPerUnit[s.Currency]
	if !ok || s.Period == "" {
		return false
	}
	monthly := float64(max(s.Min, s.Max)) * rate * periodFactor(s.Period)
	return monthly >= 300 && monthly <= 60000
}

// Monthly fills the monthly USD estimate from the stated figures.
func Monthly(s models.Salary) models.Salary {
	rate, ok := usdPerUnit[s.Currency]
	if !ok || !s.Known() {
		return s
	}
	if s.Period == "" {
		if s.Period = period("", s); s.Period == "" {
			return s
		}
	}
	f := rate * periodFactor(s.Period)
	s.MonthlyUSDMin = int(math.Round(float64(s.Min) * f))
	s.MonthlyUSDMax = int(math.Round(float64(s.Max) * f))
	return s
}

func periodFactor(p models.Period) float64 {
	switch p {
	case models.PeriodYear:
		return 1.0 / 12
	case models.PeriodHour:
		return 160
	default:
		return 1
	}
}

func parseAmount(n, multiplier string) int {
	if n == "" {
		return 0
	}
	n = strings.NewReplacer(" ", "", " ", "", " ", "", "’", "", "'", "").Replace(n)
	// "250.000" and "250,000" are thousands, "2.5" and "2,5" are decimals.
	if i := strings.LastIndexAny(n, ".,"); i >= 0 {
		if len(n)-i-1 == 3 {
			n = strings.NewReplacer(".", "", ",", "").Replace(n)
		} else {
			n = strings.ReplaceAll(strings.ReplaceAll(n[:i], ".", ""), ",", "") + "." + n[i+1:]
		}
	}
	v, err := strconv.ParseFloat(n, 64)
	if err != nil {
		return 0
	}
	m := strings.ToLower(strings.TrimSpace(multiplier))
	switch {
	case m == "":
	case m == "billion" || m == "bn" || strings.HasPrefix(m, "млрд"):
		v *= 1_000_000_000 // "$2 billion raised": implausible, so dropped
	case strings.HasPrefix(m, "млн") || m == "mln" || m == "m" || m == "mn" || m == "mio" || m == "million" || m == "kk" || m == "кк":
		v *= 1_000_000
	default:
		v *= 1000
	}
	return int(math.Round(v))
}

// firstCurrency names the currency of a figure. An explicit code anywhere
// around it beats a bare "$": "$130,000-150,000 CAD" is Canadian dollars.
func firstCurrency(parts ...string) string {
	dollar := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "$" {
			dollar = true
			continue
		}
		for _, c := range currencyWords {
			if c.re.MatchString(p) {
				return c.code
			}
		}
	}
	if dollar {
		return "USD"
	}
	return ""
}

func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			return p
		}
	}
	return ""
}

// multAt returns the multiplier captured by group, or "" when it is really
// the first letter of a word ("300 команд", "5 months").
func multAt(line string, idx []int, group int) string {
	start, end := idx[2*group], idx[2*group+1]
	if start < 0 || start == end {
		return ""
	}
	if end < len(line) {
		if r, _ := utf8.DecodeRuneInString(line[end:]); unicode.IsLetter(r) {
			return ""
		}
	}
	return line[start:end]
}

// groups returns submatch strings, "" for groups that did not take part.
func groups(s string, idx []int) []string {
	out := make([]string, len(idx)/2)
	for i := range out {
		if idx[2*i] >= 0 {
			out[i] = s[idx[2*i]:idx[2*i+1]]
		}
	}
	return out
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if r >= 'а' && r <= 'я' || r >= 'А' && r <= 'Я' || r == 'ё' || r == 'Ё' {
			return true
		}
	}
	return false
}
