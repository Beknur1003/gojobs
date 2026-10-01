// Package telegram reads public channels through their web preview at
// t.me/s/<channel>. No account, no API key, no MTProto session: the same page
// anyone sees in a browser, so nothing here can get a personal account limited.
package telegram

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/sources/htmltext"
)

const baseURL = "https://t.me/s/"

type Getter interface {
	Get(ctx context.Context, rawURL string) ([]byte, error)
}

type Channel struct {
	Name   string
	GoOnly bool
}

// Source is one channel. Each channel is its own feed, so a dead or renamed
// channel shows up as one failed source instead of hiding inside a batch.
type Source struct {
	http    Getter
	channel Channel
	pages   int
}

// New reads ch newest-first, up to pages pages of ~20 posts each.
func New(http Getter, ch Channel, pages int) *Source {
	return &Source{http: http, channel: ch, pages: pages}
}

func (s *Source) Name() string { return "telegram:" + s.channel.Name }

func (s *Source) Fetch(ctx context.Context) ([]models.Posting, error) {
	posts, err := s.fetchChannel(ctx, s.channel, s.pages)
	if err != nil {
		return posts, fmt.Errorf("telegram.Fetch @%s: %w", s.channel.Name, err)
	}
	return posts, nil
}

func (s *Source) fetchChannel(ctx context.Context, ch Channel, pages int) ([]models.Posting, error) {
	var (
		out    []models.Posting
		before int
	)
	for page := 0; page < pages; page++ {
		pageURL := baseURL + url.PathEscape(ch.Name)
		if before > 0 {
			pageURL += "?before=" + strconv.Itoa(before)
		}

		body, err := s.http.Get(ctx, pageURL)
		if err != nil {
			return out, err
		}

		posts, oldest, err := ParsePage(body, ch.Name, ch.GoOnly)
		if err != nil {
			return out, err
		}
		out = append(out, posts...)

		// The preview has no "next" when the channel's beginning is reached.
		if oldest <= 1 || len(posts) == 0 {
			break
		}
		before = oldest
	}
	return out, nil
}

// ParsePage extracts the posts of one preview page and the smallest message id
// on it, which is the cursor for the next (older) page.
func ParsePage(body []byte, channel string, goOnly bool) ([]models.Posting, int, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("telegram.ParsePage: %w", err)
	}

	var (
		posts  []models.Posting
		oldest int
	)
	forEach(doc, func(n *html.Node) bool {
		if n.DataAtom != atom.Div || !hasClass(n, "js-widget_message") {
			return true
		}
		if hasClass(n, "service_message") {
			return false // "channel pinned a message": a quote of another post
		}
		post, id, ok := parseMessage(n, channel, goOnly)
		if id > 0 && (oldest == 0 || id < oldest) {
			oldest = id
		}
		if ok {
			posts = append(posts, post)
		}
		return false // a message never nests another message
	})
	return posts, oldest, nil
}

func parseMessage(msg *html.Node, channel string, goOnly bool) (models.Posting, int, bool) {
	dataPost := attr(msg, "data-post") // "rabota_golang/1274"
	_, idStr, found := strings.Cut(dataPost, "/")
	if !found {
		return models.Posting{}, 0, false
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return models.Posting{}, 0, false
	}

	var (
		textNode *html.Node
		postedAt time.Time
	)
	forEach(msg, func(n *html.Node) bool {
		switch {
		case n.DataAtom == atom.Div && hasClass(n, "js-message_text") && textNode == nil:
			textNode = n
			return false
		case hasClass(n, "tgme_widget_message_reply"):
			return false // quoted text of another message, not this post
		case n.DataAtom == atom.Time && postedAt.IsZero():
			postedAt, _ = time.Parse(time.RFC3339, attr(n, "datetime"))
		}
		return true
	})
	if textNode == nil {
		return models.Posting{}, id, false // photo or sticker without a caption
	}

	var raw bytes.Buffer
	for c := textNode.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&raw, c)
	}
	text, links := htmltext.Convert(raw.String())

	return models.Posting{
		Source:     "telegram",
		SourceName: "@" + channel,
		ExternalID: dataPost,
		URL:        "https://t.me/" + dataPost,
		Text:       text,
		Links:      absoluteLinks(links),
		PostedAt:   postedAt,
		GoOnly:     goOnly,
	}, id, true
}

// absoluteLinks drops the preview's own hashtag search links ("?q=%23go").
func absoluteLinks(links []string) []string {
	out := links[:0]
	for _, l := range links {
		l = html.UnescapeString(l) // some hrefs arrive escaped twice: "&amp;amp;"
		if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "mailto:") || strings.HasPrefix(l, "tg://") {
			out = append(out, l)
		}
	}
	return out
}

// forEach walks the tree depth-first; visit returns false to skip children.
func forEach(n *html.Node, visit func(*html.Node) bool) {
	if n.Type == html.ElementNode && !visit(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		forEach(c, visit)
	}
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
