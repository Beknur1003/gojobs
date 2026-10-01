package boards

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/Beknur1003/gojobs/internal/models"
)

// jobPosting is the schema.org JobPosting a vacancy page embeds for search
// engines (<script type="application/ld+json">). Boards that publish no feed
// still publish this, and it is the same shape everywhere.
type jobPosting struct {
	Type         string `json:"@type"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	DatePosted   string `json:"datePosted"`
	ValidThrough string `json:"validThrough"`
	Organization struct {
		Name string `json:"name"`
	} `json:"hiringOrganization"`
	Locations    flexPlaces `json:"jobLocation"`
	LocationType string     `json:"jobLocationType"` // "TELECOMMUTE" for remote roles
	Salary       struct {
		Currency string `json:"currency"`
		Value    struct {
			Min  float64 `json:"minValue"`
			Max  float64 `json:"maxValue"`
			Unit string  `json:"unitText"` // MONTH, YEAR, HOUR
		} `json:"value"`
	} `json:"baseSalary"`
}

type jobPlace struct {
	Address postalAddress `json:"address"`
}

// postalAddress is an address object, or a plain string some boards write
// instead ("address": "Москва").
type postalAddress struct {
	Locality string `json:"addressLocality"`
	Region   string `json:"addressRegion"`
	Country  string `json:"addressCountry"`
}

func (a *postalAddress) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*a = postalAddress{Locality: s}
		return nil
	}
	type plain postalAddress
	return json.Unmarshal(b, (*plain)(a))
}

// flexPlaces accepts one place or a list of them: pages write both.
type flexPlaces []jobPlace

func (f *flexPlaces) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("[")) {
		var many []jobPlace
		if err := json.Unmarshal(b, &many); err != nil {
			return err
		}
		*f = many
		return nil
	}
	var one jobPlace
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*f = flexPlaces{one}
	return nil
}

var ldJSON = regexp.MustCompile(`(?s)<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)

// findJobPosting returns the JobPosting embedded in a vacancy page. A page
// without one is a vacancy taken down: boards drop the markup when they
// archive it.
func findJobPosting(page []byte) (jobPosting, bool) {
	for _, m := range ldJSON.FindAllSubmatch(page, -1) {
		var jp jobPosting
		if json.Unmarshal(m[1], &jp) == nil && jp.Type == "JobPosting" && jp.Title != "" {
			return jp, true
		}
	}
	return jobPosting{}, false
}

// location joins the places as "Москва, Россия; Almaty".
func (jp jobPosting) location() string {
	var out []string
	for _, p := range jp.Locations {
		parts := []string{}
		for _, s := range []string{p.Address.Locality, p.Address.Region} {
			if s = strings.TrimSpace(s); s != "" && !strings.Contains(strings.Join(parts, ", "), s) {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 && p.Address.Country != "" {
			parts = append(parts, p.Address.Country)
		}
		if len(parts) > 0 {
			out = append(out, strings.Join(parts, ", "))
		}
	}
	return strings.Join(out, "; ")
}

func (jp jobPosting) salary() models.Salary {
	v := jp.Salary.Value
	if jp.Salary.Currency == "" || v.Min <= 0 && v.Max <= 0 {
		return models.Salary{}
	}
	period := map[string]models.Period{"MONTH": models.PeriodMonth, "YEAR": models.PeriodYear, "HOUR": models.PeriodHour}[strings.ToUpper(v.Unit)]
	if period == "" {
		period = models.PeriodMonth
	}
	return models.Salary{Min: int(v.Min), Max: int(v.Max), Currency: strings.ToUpper(jp.Salary.Currency), Period: period}
}

func (jp jobPosting) posted() time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, jp.DatePosted); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// sectionHTML returns the inner HTML of the first element whose class list
// has class, or "" when the page has none.
func sectionHTML(page []byte, class string) string {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return ""
	}
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "class" && hasToken(a.Val, class) {
					found = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if found == nil {
		return ""
	}
	var b bytes.Buffer
	for c := found.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&b, c)
	}
	return b.String()
}

func hasToken(list, token string) bool {
	for _, t := range strings.Fields(list) {
		if t == token {
			return true
		}
	}
	return false
}
