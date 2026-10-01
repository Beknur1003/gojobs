package boards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
)

// Freehire reads the public, keyless API of freehire.me, an MIT-licensed
// open-source catalogue that crawls company career pages on ~90 ATS
// platforms (github.com/strelov1/freehire). It is how this board reaches
// employers it does not crawl itself: Oracle, iCIMS, SmartRecruiters,
// Yandex, Ozon, VK and the like. Every card links to the original posting and
// credits freehire as the source.
type Freehire struct {
	http Getter
	base string
}

func NewFreehire(http Getter) *Freehire {
	return &Freehire{http: http, base: "https://freehire.me"}
}

func (s *Freehire) Name() string { return "freehire" }

const (
	freehirePage = 100
	// freehireDepth is the API's cap on offset+limit for one query.
	freehireDepth = 10000
)

// freehireSkip are origins left out: affiliate feeds whose links are paid
// redirects, and boards this project already reads directly (a fresher copy
// with no middleman). Prefix match, so "whatjobs" covers every country site.
var freehireSkip = []string{
	"whatjobs", "adzuna", "jobleads",
	"jobstash", // re-posts ATS listings and names a portfolio's investor as the employer
	"arbeitnow", "djinni", "himalayas", "jobicy", "remoteok", "remotive",
	"weworkremotely", "workingnomads", "hackernews", "golangprojects",
}

// freehireEmployers are origins that read an employer's own career page: an
// ATS tenant or a company's careers site (from freehire's docs/sources.md,
// 2026-10-01). Everything else, including origins freehire adds later, is
// treated as a job board, which keeps recruiter contacts and never overrides
// the employer's name. Left out although freehire files them under ATS:
// trudvsem and arbeitsagentur (Russia's and Germany's public job portals)
// and workablemarketplace (Workable's job marketplace).
var freehireStreams = map[string]bool{"telegram": true}

var freehireEmployers = map[string]bool{
	// ATSes freehire serves that its docs did not list yet (seen live).
	"herp": true, "hrmos": true, "dayforce": true, "keka": true, "hrmdirect": true, "dover": true,
	"gusto": true, "paycor": true,

	"2gis": true, "adp": true, "alfabank": true, "alignerr": true, "amazon": true, "apple": true,
	"applicantpro": true, "apploi": true, "ashby": true, "ashbygraphql": true,
	"avature": true, "aviasales": true, "avito": true, "bairesdev": true, "bamboohr": true, "betterteam": true,
	"breezy": true, "briefhq": true, "bullhorn": true, "careerplug": true, "careerspage": true, "catsone": true,
	"cleverstaff": true, "clinch": true, "comeet": true, "compleo": true, "cornerstone": true, "dataart": true,
	"deel": true, "dodo": true, "domclick": true, "earcu": true, "eightfold": true, "emagine": true,
	"enlizt": true, "epam": true, "erecruiter": true, "factorial": true, "freshteam": true, "gem": true,
	"globalpayments": true, "google": true, "greenhouse": true, "gupy": true, "hibob": true, "hireology": true,
	"huntflow": true, "hurma": true, "icims": true, "inhire": true, "ismartrecruit": true, "isolvedhire": true,
	"jazzhr": true, "jibe": true, "jobscore": true, "jobvite": true, "join": true, "kuper": true,
	"lamoda": true, "lever": true, "likeit": true, "loxo": true, "lumenalta": true, "luxoft": true,
	"manatal": true, "meta": true, "micro1": true, "mindsight": true, "mts": true, "mtslink": true,
	"neogov": true, "northstone": true, "odoo": true, "onstrider": true, "opencats": true, "oracle": true,
	"ozon": true, "pageup": true, "paycom": true, "paylocity": true, "peopleforce": true, "personio": true,
	"phenom": true, "pinpoint": true, "quickin": true, "radancy": true, "rapyd": true, "recruitee": true,
	"recruitingsolutions": true, "rippling": true, "rwb": true, "sber": true, "senior": true,
	"smartrecruiters": true, "softgarden": true, "solides": true, "spark": true, "speedrun": true,
	"successfactors": true, "talentadore": true, "talenthr": true, "talentlyft": true, "taleo": true,
	"tbank": true, "teamtailor": true, "telegramcareers": true, "traffit": true, "trakstar": true, "uber": true,
	"ukg": true, "vention": true, "vk": true, "vouch": true, "workable": true,
	"workday": true, "wpyoast": true, "yandex": true, "yandexcrowd": true, "zohorecruit": true,
}

type freehireJob struct {
	Slug        string   `json:"public_slug"`
	Source      string   `json:"source"`
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Company     string   `json:"company"`
	Location    string   `json:"location"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	WorkMode    string   `json:"work_mode"`
	PostedAt    string   `json:"posted_at"`
	CreatedAt   string   `json:"created_at"`
	ClosedAt    *string  `json:"closed_at"`
	// Enrichment is LLM output with loose types ("relocation": true or
	// "yes"), so it is read field by field and a surprise only loses a hint.
	Enrichment map[string]json.RawMessage `json:"enrichment"`
	Reality    *struct {
		Class         string `json:"class"`
		FakeFreshness bool   `json:"fake_freshness"`
	} `json:"reality"`
}

type freehirePageResp struct {
	Data []freehireJob `json:"data"`
	Meta struct {
		Total int `json:"total"`
	} `json:"meta"`
}

// Fetch pages through every Go posting via the agent search endpoint: the
// plain search truncates descriptions to ~1000 characters, which hides the
// requirements section where Go is usually named.
//
// Fetch pages through every Go posting. The API stops at 10 000 deep, so a
// larger result is read from both ends (newest first, then oldest first).
// A failed page makes the whole fetch an error: a partial list must not look
// like the missing roles were closed.
func (s *Freehire) Fetch(ctx context.Context) ([]models.Posting, error) {
	var (
		out  []models.Posting
		seen = map[string]bool{}
	)
	read := func(order string, limit int) (total int, err error) {
		for offset := 0; offset < limit; offset += freehirePage {
			var resp freehirePageResp
			q := url.Values{
				"skills": {"go"}, "limit": {strconv.Itoa(freehirePage)}, "offset": {strconv.Itoa(offset)},
				"sort": {"posted_at"}, "order": {order},
			}
			if err := s.http.GetJSON(ctx, s.base+"/api/v1/agent/jobs/search?"+q.Encode(), &resp); err != nil {
				return total, err
			}
			total = resp.Meta.Total
			for _, j := range resp.Data {
				if seen[j.Slug] {
					continue
				}
				seen[j.Slug] = true
				if p, ok := s.posting(j); ok {
					out = append(out, p)
				}
			}
			if len(resp.Data) < freehirePage || offset+freehirePage >= total {
				break
			}
		}
		return total, nil
	}

	total, err := read("desc", freehireDepth)
	if err == nil && total > freehireDepth {
		_, err = read("asc", min(total-freehireDepth, freehireDepth))
	}
	if err != nil {
		return out, fmt.Errorf("freehire.Fetch: %w", err)
	}
	return out, nil
}

func (s *Freehire) posting(j freehireJob) (models.Posting, bool) {
	if j.Slug == "" || j.URL == "" || j.ClosedAt != nil {
		return models.Posting{}, false
	}
	origin := strings.ToLower(j.Source)
	for _, skip := range freehireSkip {
		if strings.HasPrefix(origin, skip) {
			return models.Posting{}, false
		}
	}

	source := "freehire-board"
	switch {
	case freehireEmployers[origin]:
		source = "freehire"
	case freehireStreams[origin]:
		source = "freehire-stream" // a relayed channel post: ages out, never closes
	}

	if j.Reality != nil && j.Reality.FakeFreshness {
		// Reposted to look new: not worth a new card, but the role is still
		// open, so a job already built from it is kept alive.
		return models.Posting{Source: source, ExternalID: j.Slug, Title: strings.TrimSpace(j.Title), Stub: true}, true
	}

	text, links := htmlToText(j.Description)
	posted, err := time.Parse(time.RFC3339, j.PostedAt)
	if err != nil {
		posted, _ = time.Parse(time.RFC3339, j.CreatedAt)
	}

	p := models.Posting{
		Source:     source,
		SourceName: "freehire · " + origin,
		ExternalID: j.Slug,
		URL:        s.base + "/jobs/" + url.PathEscape(j.Slug),
		Title:      strings.TrimSpace(j.Title),
		Company:    strings.TrimSpace(j.Company),
		Location:   j.Location,
		Text:       text,
		Links:      links,
		ApplyURL:   j.URL,
		Remote:     j.WorkMode == "remote",
		PostedAt:   posted.UTC(),
	}

	e := j.Enrichment
	parts := []string{
		workModeHint(j.WorkMode),
		looseString(e["seniority"]),
		strings.ReplaceAll(looseString(e["employment_type"]), "_", " "),
	}
	if lvl := looseString(e["english_level"]); lvl != "" {
		parts = append(parts, "English "+lvl)
	}
	if looseTrue(e["relocation"]) {
		parts = append(parts, "relocation")
	}
	lo, hi := looseInt(e["salary_min"]), looseInt(e["salary_max"])
	if lo > 0 || hi > 0 {
		p.Salary = models.Salary{
			Min: lo, Max: hi,
			Currency: strings.ToUpper(looseString(e["salary_currency"])),
			Period:   freehirePeriod(looseString(e["salary_period"])),
		}
	}
	p.Hints = hints(parts...)
	return p, true
}

// looseString reads a string, or the first element of a list of strings.
func looseString(raw json.RawMessage) string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0]
	}
	return ""
}

func looseInt(raw json.RawMessage) int {
	var f flexInt
	if len(raw) == 0 || f.UnmarshalJSON(raw) != nil {
		return 0
	}
	return int(f)
}

// looseTrue accepts true and the words an LLM uses for yes.
func looseTrue(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	switch strings.ToLower(looseString(raw)) {
	case "yes", "true", "provided", "available", "offered", "supported", "possible":
		return true
	}
	return false
}

func workModeHint(mode string) string {
	switch mode {
	case "hybrid":
		return "hybrid"
	case "onsite":
		return "on-site"
	default:
		return "" // "remote" is carried by Posting.Remote
	}
}

func freehirePeriod(p string) models.Period {
	switch p {
	case "month":
		return models.PeriodMonth
	case "hour":
		return models.PeriodHour
	case "year":
		return models.PeriodYear
	default:
		return "" // unknown: extraction infers it from the amount
	}
}
