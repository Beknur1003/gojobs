package extract

import (
	"regexp"
	"strings"
)

// footerMax is how many lines from the bottom can be a channel footer.
const footerMax = 4

var (
	promoRe   = regexp.MustCompile(`(?i)размещени|разместить|реклам|по вопросам|подпис|subscribe|наш канал|our channel|больше ваканси|more jobs|канал с ваканси|чат для|join us|присоединяйся|все вакансии`)
	mentionRe = regexp.MustCompile(`@[A-Za-z][A-Za-z0-9_]{3,31}|t\.me/`)
)

// StripFooter removes a channel's promo block from the bottom of one post:
// lines that name the channel itself, or advertise something with a handle
// or link. It works on a single post, so it covers quiet channels where
// LearnNoise has too few posts to learn the footer from.
func StripFooter(text, channel string) (kept, removed string) {
	lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
	self := strings.ToLower("@" + channel)
	selfLink := strings.ToLower("t.me/" + channel)

	cut := len(lines)
	checked := 0
	for i := len(lines) - 1; i >= 0 && checked < footerMax; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			cut = i
			continue
		}
		checked++
		low := strings.ToLower(l)
		isSelf := strings.Contains(low, self) || strings.Contains(low, selfLink)
		isPromo := promoRe.MatchString(l) && mentionRe.MatchString(l)
		if !isSelf && !isPromo {
			break
		}
		cut = i
	}
	return strings.TrimSpace(strings.Join(lines[:cut], "\n")), strings.Join(lines[cut:], "\n")
}

// DropFooterLinks removes Telegram links whose handle appears only in the
// removed footer: the href of "Размещение: @admin" outlives the text.
func DropFooterLinks(links []string, removed, kept string) []string {
	if removed == "" {
		return links
	}
	removedLow, keptLow := strings.ToLower(removed), strings.ToLower(kept)
	out := make([]string, 0, len(links))
	for _, l := range links {
		if m := tmeRe.FindStringSubmatch(l); m != nil {
			h := strings.ToLower(m[1])
			if strings.Contains(removedLow, h) && !strings.Contains(keptLow, h) {
				continue
			}
		}
		out = append(out, l)
	}
	return out
}
