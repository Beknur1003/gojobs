package extract

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/Beknur1003/gojobs/internal/models"
)

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._%+-]*@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)

	// HN posters hide addresses from scrapers: "jobs [at] acme [dot] com".
	obfAt  = regexp.MustCompile(`(?i)\s*[\[\(\{<]\s*(?:at|@)\s*[\]\)\}>]\s*`)
	obfDot = regexp.MustCompile(`(?i)\s*[\[\(\{<]\s*(?:dot|\.)\s*[\]\)\}>]\s*`)
	obfAll = regexp.MustCompile(`(?i)\b([a-z0-9][a-z0-9._+-]*)\s+at\s+([a-z0-9-]+)\s+dot\s+([a-z]{2,})\b`)

	// "@handle" with no letter or dot right before it, so emails do not match.
	handleRe = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_.@/])@([A-Za-z][A-Za-z0-9_]{3,31})\b`)

	tmeRe = regexp.MustCompile(`^https?://(?:www\.)?(?:t\.me|telegram\.me)/([A-Za-z][A-Za-z0-9_]{3,31})/?(?:\?.*)?$`)
)

// Service paths on t.me that look like usernames but are not people.
var tmeReserved = map[string]bool{
	"addlist": true, "joinchat": true, "share": true, "proxy": true, "socks": true,
	"setlanguage": true, "addstickers": true, "addemoji": true, "boost": true, "iv": true,
	"contact": true, "login": true, "confirmphone": true, "invoice": true,
}

// Address domains and parts that are never a hiring contact.
var junkEmail = []string{"example.com", "example.org", "domain.com", "email.com", "sentry.io", "noreply", "no-reply", "wixpress"}

// junkLocal are mailbox names that belong to a function, not to a recruiter:
// accessibility and privacy desks, anti-fraud notices, placeholders. Words
// like "security" or "support" count only as the whole name ("security@"),
// so a recruiter's "security-careers@" survives.
var junkLocal = regexp.MustCompile(`accommodat|accomodat|accessib|disabilit|privacy|dataprotection|data[._-]protection|donotreply|do[._-]not[._-]reply|phish|fraud|^(?:dpo|gdpr|security|abuse|legal|compliance|ethics|support|name|firstname|first[._]last|firstname[._]lastname|you|your[._]?name|user|email|john[._]?doe)$`)

var imageExt = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|svg)$`)

// channelHandle looks like a job channel or chat, not a person: channels
// promote each other under every post ("@job_python", "@devs_it").
var channelHandle = regexp.MustCompile(`(?:^|_)(?:jobs?|vacanc[a-z]*|vakans[a-z]*|rabota|careers?|channel|chat|news|digest|feed)(?:_|$)|^it_|_it$|^best_?it`)

// Contacts mines emails, Telegram handles and links from a post. Values in
// ignore (a channel's own footer, its name) are skipped. URL contacts are only
// taken when withURLs is set: on job boards every link is a company page, in a
// Telegram post a bare link is usually the application form.
func Contacts(text string, links []string, ignore map[string]bool, withURLs bool) []models.Contact {
	var (
		out  []models.Contact
		seen = map[string]bool{}
	)
	add := func(kind models.ContactKind, value string) {
		key := ContactKey(kind, value)
		if value == "" || seen[key] || ignore[key] {
			return
		}
		seen[key] = true
		out = append(out, models.Contact{Kind: kind, Value: value})
	}

	for _, e := range Emails(text, links) {
		add(models.ContactEmail, e)
	}
	for _, h := range Handles(text, links) {
		add(models.ContactTelegram, h)
	}
	if withURLs {
		urls := 0
		for _, l := range links {
			if urls == 3 {
				break
			}
			if u, ok := applyLink(l); ok {
				before := len(out)
				add(models.ContactURL, u)
				if len(out) > before {
					urls++
				}
			}
		}
	}
	return out
}

// ContactKey is the identity of a contact for deduplication and ignore lists.
func ContactKey(kind models.ContactKind, value string) string {
	return string(kind) + ":" + strings.ToLower(strings.TrimSuffix(value, "/"))
}

func Emails(text string, links []string) []string {
	text = obfAll.ReplaceAllString(text, "$1@$2.$3")
	text = obfDot.ReplaceAllString(obfAt.ReplaceAllString(text, "@"), ".")

	var out []string
	seen := map[string]bool{}
	add := func(e string) {
		e = strings.ToLower(strings.Trim(e, ".-_"))
		if seen[e] || !emailRe.MatchString(e) || imageExt.MatchString(e) {
			return
		}
		if local, _, _ := strings.Cut(e, "@"); junkLocal.MatchString(local) {
			return
		}
		for _, j := range junkEmail {
			if strings.Contains(e, j) {
				return
			}
		}
		seen[e] = true
		out = append(out, e)
	}

	for _, l := range links {
		if addr, ok := strings.CutPrefix(l, "mailto:"); ok {
			addr, _, _ = strings.Cut(addr, "?")
			if u, err := url.PathUnescape(addr); err == nil {
				addr = u
			}
			add(addr)
		}
	}
	for _, e := range emailRe.FindAllString(text, -1) {
		add(e)
	}
	return out
}

// Handles returns Telegram usernames without "@", lowercased for comparison.
// Bots are dropped: the point is a person to write to.
func Handles(text string, links []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(h string) {
		lh := strings.ToLower(h)
		if seen[lh] || tmeReserved[lh] || strings.HasSuffix(lh, "bot") || channelHandle.MatchString(lh) {
			return
		}
		seen[lh] = true
		out = append(out, h)
	}

	for _, m := range handleRe.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, l := range links {
		if m := tmeRe.FindStringSubmatch(l); m != nil {
			add(m[1])
			continue
		}
		if strings.HasPrefix(l, "tg://resolve") {
			if u, err := url.Parse(l); err == nil {
				if d := u.Query().Get("domain"); d != "" {
					add(d)
				}
			}
		}
	}
	return out
}

// Links never worth showing as "where to apply".
var junkHosts = []string{
	"t.me", "telegram.me", "telegram.org", "telesco.pe", "youtube.com", "youtu.be",
	"instagram.com", "facebook.com", "vk.com", "twitter.com", "x.com", "tiktok.com",
	"wa.me", "whatsapp.com", "max.ru", "vk.me", "ord.vk.com", "dzen.ru",
}

func applyLink(l string) (string, bool) {
	u, err := url.Parse(l)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !strings.Contains(u.Host, ".") {
		return "", false // auto-linked junk like "http://промо@bg"
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	for _, j := range junkHosts {
		if host == j || strings.HasSuffix(host, "."+j) {
			return "", false
		}
	}
	return l, true
}
