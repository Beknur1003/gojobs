// Package htmltext turns a fragment of HTML into readable plain text and the
// list of links it contained. Line structure matters to the extraction rules
// (the first line of a post is usually the title), so block tags become breaks.
package htmltext

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	blankRuns  = regexp.MustCompile(`\n{3,}`)
	spaceRuns  = regexp.MustCompile(`[ \t\x{00A0}]+`)
	spaceLines = regexp.MustCompile(` *\n *`)
)

var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Li: true, atom.Ul: true, atom.Ol: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Tr: true, atom.Table: true, atom.Blockquote: true, atom.Pre: true, atom.Section: true,
}

// Convert returns the text of fragment and the hrefs of its links, in order.
func Convert(fragment string) (text string, links []string) {
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{
		Type: html.ElementNode, Data: "body", DataAtom: atom.Body,
	})
	if err != nil {
		return strings.TrimSpace(fragment), nil
	}

	var b strings.Builder
	for _, n := range nodes {
		walk(n, &b, &links)
	}
	return Clean(b.String()), links
}

// Clean normalizes whitespace while keeping single and double line breaks.
func Clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = spaceRuns.ReplaceAllString(s, " ")
	s = spaceLines.ReplaceAllString(s, "\n")
	s = blankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func walk(n *html.Node, b *strings.Builder, links *[]string) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.ElementNode:
		switch n.DataAtom {
		case atom.Br:
			b.WriteByte('\n')
			return
		case atom.Script, atom.Style:
			return
		case atom.A:
			if href := attr(n, "href"); href != "" {
				*links = append(*links, href)
			}
		case atom.Li:
			b.WriteString("\n• ")
		}
		if blockTags[n.DataAtom] && n.DataAtom != atom.Li {
			b.WriteByte('\n')
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, b, links)
	}

	if n.Type == html.ElementNode && blockTags[n.DataAtom] {
		b.WriteByte('\n')
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
