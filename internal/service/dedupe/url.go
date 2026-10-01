package dedupe

import (
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	greenhouseHost = regexp.MustCompile(`^(?:job-)?boards(?:\.eu)?\.greenhouse\.io$`)
	greenhousePath = regexp.MustCompile(`^/[^/]+/jobs/(\d+)`)
	leverPath      = regexp.MustCompile(`^/[^/]+/([0-9a-f-]{36})`)
	ashbyPath      = regexp.MustCompile(`^/[^/]+/([0-9a-f-]{36})`)
	workdayHost    = regexp.MustCompile(`^([a-z0-9-]+)\.wd\d+\.myworkdayjobs\.com$`)
	// A Workday posting path is ".../job/<location>/<Title>_<REQ>", the same
	// requisition on another site of the tenant gets "_<REQ>-2". The site
	// root ("/External_Careers") is not a posting.
	workdayReqID = regexp.MustCompile(`/(?:job|details)/[^?]*_([A-Za-z0-9]+(?:-[A-Za-z0-9]+)*?)(?:-\d{1,2})?$`)
	ukgHost      = regexp.MustCompile(`(?:^|\.)(?:ultipro\.com|ukg\.net)$`)
	ukgTenant    = regexp.MustCompile(`^/([a-z0-9]+)/`)
	hhHost       = regexp.MustCompile(`(?:^|\.)(?:hh\.(?:ru|kz|uz|by)|headhunter\.(?:ge|kz))$`)
	hhPath       = regexp.MustCompile(`^/vacancy/(\d+)`)
	linkedinPath = regexp.MustCompile(`^/jobs/view/(?:[^/]*-)?(\d{6,})`)

	// A generic URL identifies one vacancy only if its last path segment
	// carries an id (a run of digits, a long hex string or a UUID), or a known
	// id query parameter does. "https://ozon.tech/careers" is a page every
	// post links, and in "/acme1019/jobboard/<board-uuid>/detail?jobId=7"
	// the numbers in the path name the board, not the vacancy.
	specificID = regexp.MustCompile(`\d{4,}|[0-9a-f]{16,}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

	// vacancyPage is how a link inside a post's text proves it points at a
	// vacancy, not at a blog article that several vacancies cite.
	vacancyPage = regexp.MustCompile(`^(?:greenhouse|lever|ashby|workday|ukg|hh|linkedin):|/(?:jobs?|vacanc[a-z]*|careers?|positions?|openings?|offers?|apply)(?:/|\?|$)`)
)

// idParams are query parameters that carry the vacancy id on ATSs that keep
// it out of the path. Matched case-insensitively.
var idParams = map[string]bool{
	"opportunityid": true, "jobid": true, "job_id": true, "job": true, "jid": true, "id": true,
	"req": true, "reqid": true, "requisitionid": true, "vacancyid": true, "positionid": true, "postingid": true,
}

// IsVacancyKey reports a URLKey that names a vacancy page by its shape.
func IsVacancyKey(key string) bool { return vacancyPage.MatchString(key) }

// URLKey is the identity of a vacancy's posting URL across the shapes the
// same posting takes on different aggregators: another host alias, tracking
// parameters, a trailing "/apply". It returns "" for URLs that do not name a
// single vacancy, and for Telegram posts: a channel post can carry several
// vacancies, and freehire splits such a digest into one posting each.
func URLKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/apply"), "/application")
	query := lowerKeys(u.Query())

	switch {
	case host == "t.me" || host == "telegram.me":
		return ""
	case greenhouseHost.MatchString(host):
		if m := greenhousePath.FindStringSubmatch(path); m != nil {
			return "greenhouse:" + m[1] // job ids are global across boards
		}
	case host == "jobs.lever.co" || host == "jobs.eu.lever.co":
		if m := leverPath.FindStringSubmatch(path); m != nil {
			return "lever:" + m[1]
		}
	case host == "jobs.ashbyhq.com":
		if m := ashbyPath.FindStringSubmatch(path); m != nil {
			return "ashby:" + m[1]
		}
	case workdayHost.MatchString(host):
		tenant := workdayHost.FindStringSubmatch(host)[1]
		if m := workdayReqID.FindStringSubmatch(path); m != nil {
			return "workday:" + tenant + ":" + strings.ToLower(m[1])
		}
		return ""
	case ukgHost.MatchString(host):
		if id := query["opportunityid"]; id != "" {
			tenant := ""
			if m := ukgTenant.FindStringSubmatch(strings.ToLower(path)); m != nil {
				tenant = m[1]
			}
			return "ukg:" + tenant + ":" + strings.ToLower(id)
		}
		return ""
	case hhHost.MatchString(host):
		if m := hhPath.FindStringSubmatch(path); m != nil {
			return "hh:" + m[1] // spb.hh.ru, hh.kz and hh.ru share vacancy ids
		}
	case host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com"):
		if m := linkedinPath.FindStringSubmatch(path); m != nil {
			return "linkedin:" + m[1]
		}
	}

	// Greenhouse boards embedded on a company site: "?gh_jid=123".
	if id := query["gh_jid"]; id != "" {
		return "greenhouse:" + id
	}

	key := host + strings.ToLower(path)
	if ids := idQuery(query); ids != "" {
		return key + "?" + ids
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	last := strings.ToLower(segs[len(segs)-1])
	if specificID.MatchString(last) {
		return key
	}
	// "/details/200674160/software-engineer": the id comes before a title
	// slug (Apple, Amazon, Microsoft, Phenom sites).
	if len(segs) >= 2 && strings.Contains(last, "-") && specificID.MatchString(strings.ToLower(segs[len(segs)-2])) {
		return host + strings.ToLower("/"+strings.Join(segs[:len(segs)-1], "/"))
	}
	return ""
}

func lowerKeys(q url.Values) map[string]string {
	out := make(map[string]string, len(q))
	for k, v := range q {
		if len(v) > 0 {
			out[strings.ToLower(k)] = v[0]
		}
	}
	return out
}

// idQuery renders the id-carrying query parameters, sorted, or "".
func idQuery(q map[string]string) string {
	var parts []string
	for k, v := range q {
		if idParams[k] && v != "" {
			parts = append(parts, k+"="+strings.ToLower(v))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

// keyNamespace is the id space a key belongs to, for ATSs where two
// different ids are two different vacancies ("greenhouse", "workday:acme").
// Job boards like hh are left out: an employer re-creates a vacancy there
// under a new id, and the repost is still the same role.
func keyNamespace(key string) string {
	kind, rest, _ := strings.Cut(key, ":")
	switch kind {
	case "greenhouse", "lever", "ashby":
		return kind
	case "workday", "ukg":
		tenant, _, _ := strings.Cut(rest, ":")
		return kind + ":" + tenant
	case "hh", "linkedin":
		return ""
	}
	// A company's own vacancy pages ("team.vk.company/vacancy/45427"): two
	// different ids on one careers host are two different vacancies.
	if IsVacancyKey(key) {
		host, _, _ := strings.Cut(key, "/")
		return "site:" + host
	}
	return ""
}

var tailIDRe = regexp.MustCompile(`(?:[/=])([a-z0-9-]*\d[a-z0-9-]*)$`)

// requisitionIDs are the posting ids a set of keys names, comparable across
// shapes: a Workday requisition and the same number on the employer's own
// careers site. hh and LinkedIn ids are left out (re-created vacancies).
func requisitionIDs(keys []string) []string {
	var out []string
	for _, k := range keys {
		if strings.HasPrefix(k, "hh:") || strings.HasPrefix(k, "linkedin:") {
			continue
		}
		if id := tailID(k); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// tailID is the requisition-like id a generic key ends with: the last path
// segment or id parameter value when it contains a digit ("d11137",
// "mer00045mu"). Two site aliases of one ATS tenant share it.
func tailID(key string) string {
	if kind, rest, ok := strings.Cut(key, ":"); ok && !strings.ContainsAny(kind, "./") {
		return rest[strings.LastIndex(rest, ":")+1:] // "workday:hpe:1209943" -> "1209943"
	}
	if m := tailIDRe.FindStringSubmatch(key); m != nil {
		return m[1]
	}
	return ""
}
