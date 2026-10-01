// Package dedupe recognizes the same vacancy published in several places: a
// recruiter posting to five channels, or a company listing that is also on a
// remote board. Hirify uses vectors plus an LLM check here; for one language
// and a few thousand jobs, shingle overlap plus shared contacts is enough.
package dedupe

import (
	"hash/fnv"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
)

const (
	shingleSize = 3
	maxWords    = 500

	// Near-identical text is the same post whatever else differs.
	sameText = 0.6
	// Loosely similar text counts when the posts also share a contact or
	// the same company and title.
	similarText = 0.3
	// Company boards and remote boards describe one role in different words.
	// Same company and same title within this window is the same vacancy.
	sameRoleWindow = 30 * 24 * time.Hour
	// A role cloned per location: same company and title, near-identical text.
	cloneText = 0.9
)

// Index holds the known jobs and the lookup tables used to match new ones.
type Index struct {
	Jobs []models.Job

	shingles  [][]uint64
	keys      [][]string // canonical URL keys per job
	bySource  map[string]int
	byURL     map[string]int // canonical posting URL -> job
	byContact map[string][]int
	byRole    map[string][]int
}

func NewIndex(jobs []models.Job) *Index {
	ix := &Index{
		bySource:  map[string]int{},
		byURL:     map[string]int{},
		byContact: map[string][]int{},
		byRole:    map[string][]int{},
	}
	for _, j := range jobs {
		ix.Add(j)
	}
	return ix
}

// Add appends j and indexes it. It returns the new job's position.
func (ix *Index) Add(j models.Job) int {
	i := len(ix.Jobs)
	ix.Jobs = append(ix.Jobs, j)
	ix.shingles = append(ix.shingles, Shingles(j.Text))
	ix.keys = append(ix.keys, nil)
	ix.indexKeys(i)
	return i
}

// Replace swaps job i for j (after a merge) and indexes the new keys. Stale
// keys of the old version stay: they still point at the same vacancy.
func (ix *Index) Replace(i int, j models.Job) {
	ix.Jobs[i] = j
	ix.shingles[i] = Shingles(j.Text)
	ix.indexKeys(i)
}

func (ix *Index) soleOwner(keys []string) (int, bool) {
	owner := -1
	for _, k := range keys {
		i, ok := ix.byURL[k]
		if !ok {
			continue
		}
		if owner >= 0 && owner != i {
			return 0, false
		}
		owner = i
	}
	return owner, owner >= 0
}

// URLOwner returns another job that already owns one of j's URL keys. After
// a re-fetched posting gains a URL that an older, separate job carries, the
// two are one vacancy.
func (ix *Index) URLOwner(i int) (int, bool) {
	for _, k := range ix.keys[i] {
		if owner, ok := ix.byURL[k]; ok && owner != i {
			return owner, true
		}
	}
	return 0, false
}

func (ix *Index) indexKeys(i int) {
	j := ix.Jobs[i]
	for _, s := range j.Sources {
		ix.bySource[sourceKey(s)] = i
	}
	ix.keys[i] = urlKeys(j)
	// Only a job's own posting URLs identify it. Links quoted in a post's
	// text can name several vacancies and must not make it their owner.
	for _, k := range ownURLKeys(j) {
		if _, taken := ix.byURL[k]; !taken {
			ix.byURL[k] = i
		}
	}
	for _, c := range j.Contacts {
		k := contactKey(c)
		if !slices.Contains(ix.byContact[k], i) {
			ix.byContact[k] = append(ix.byContact[k], i)
		}
	}
	if k := roleKey(j); k != "" && !slices.Contains(ix.byRole[k], i) {
		ix.byRole[k] = append(ix.byRole[k], i)
	}
}

// FindSource returns the job already built from this exact posting.
func (ix *Index) FindSource(ref models.SourceRef) (int, bool) {
	i, ok := ix.bySource[sourceKey(ref)]
	return i, ok
}

// FindDuplicate returns a different posting of the same vacancy, if known.
// The same original posting URL is decisive; then company and title; then
// text similarity. Two postings that name different vacancies on the same
// board (two Greenhouse ids, two Workday requisitions of one tenant) are
// never the same vacancy, however alike they read.
func (ix *Index) FindDuplicate(j models.Job) (int, bool) {
	keys := urlKeys(j)
	for _, k := range ownURLKeys(j) {
		if i, ok := ix.byURL[k]; ok {
			return i, true
		}
	}
	// A post linking exactly one known vacancy page is that vacancy; one
	// linking several is ambiguous and is matched by text like any post.
	if owner, ok := ix.soleOwner(contactURLKeys(j)); ok && !ix.distinct(j, keys, owner) {
		return owner, true
	}

	sh := Shingles(j.Text)

	related := map[int]bool{}
	for _, c := range j.Contacts {
		for _, i := range ix.byContact[contactKey(c)] {
			related[i] = true
		}
	}
	if k := roleKey(j); k != "" {
		for _, i := range ix.byRole[k] {
			if absDuration(ix.Jobs[i].PostedAt.Sub(j.PostedAt)) > sameRoleWindow {
				continue
			}
			// Same title and the same text is one role posted per location
			// or per site: one card, several links, even across requisitions.
			if !ix.distinct(j, keys, i) || Jaccard(sh, ix.shingles[i]) >= cloneText {
				return i, true
			}
		}
	}

	best, bestScore := -1, 0.0
	for i := range ix.Jobs {
		// Reposts happen within weeks; skip far-apart jobs cheaply.
		if absDuration(ix.Jobs[i].PostedAt.Sub(j.PostedAt)) > 60*24*time.Hour || ix.distinct(j, keys, i) {
			continue
		}
		score := Jaccard(sh, ix.shingles[i])
		if score >= sameText || (score >= similarText && related[i]) {
			if score > bestScore {
				best, bestScore = i, score
			}
		}
	}
	return best, best >= 0
}

// distinct reports that j and job i provably name different vacancies:
// different ids in the same id space, or different listings of one board.
func (ix *Index) distinct(j models.Job, keys []string, i int) bool {
	theirs := ix.keys[i]
	spaces := map[string]bool{}
	for _, k := range theirs {
		if ns := keyNamespace(k); ns != "" {
			spaces[ns] = true
		}
	}
	for _, k := range keys {
		if ns := keyNamespace(k); ns != "" && spaces[ns] && !slices.Contains(theirs, k) {
			return true
		}
	}
	// Both name their own posting ids and none match: different requisitions,
	// even when one id comes from a Workday key and the other from the
	// employer's careers site.
	mineIDs, theirIDs := requisitionIDs(postingKeys(j)), requisitionIDs(postingKeys(ix.Jobs[i]))
	if len(mineIDs) > 0 && len(theirIDs) > 0 && !slices.ContainsFunc(mineIDs, func(id string) bool { return slices.Contains(theirIDs, id) }) {
		return true
	}
	for _, mine := range j.Sources {
		for _, other := range ix.Jobs[i].Sources {
			if singleEmployerFeed(mine.Feed) && other.Feed == mine.Feed && other.ExternalID != mine.ExternalID {
				return true
			}
			if sameOriginDifferentPosting(mine, other) {
				return true
			}
		}
	}
	return false
}

// sameOriginDifferentPosting reports two listings from the same origin (one
// board, or one freehire crawler such as "freehire · smartrecruiters") that
// link different postings. An origin lists each vacancy once, so different
// posting ids from it are different vacancies however alike they read.
// Job-board ids on hh and LinkedIn are exempt: employers re-create the same
// vacancy there under a new id.
//
// It is limited to employer ATS crawlers relayed by freehire (Source
// "freehire"): job boards such as Himalayas list one role under several
// slugs, and those reposts are one vacancy. Two copies whose keys end in the
// same requisition id (one Oracle or Taleo tenant served under two site
// aliases) are one vacancy too.
func sameOriginDifferentPosting(a, b models.SourceRef) bool {
	if a.Source != "freehire" || b.Source != "freehire" || a.Name != b.Name || a.ExternalID == b.ExternalID {
		return false
	}
	ka, kb := identityKeys(a), identityKeys(b)
	if len(ka) == 0 || len(kb) == 0 {
		return false
	}
	for _, x := range ka {
		for _, y := range kb {
			if x == y || (tailID(x) != "" && tailID(x) == tailID(y)) {
				return false
			}
		}
	}
	return true
}

// identityKeys are the posting keys of one copy. freehire's own page URL is
// left out (each of its slugs is unique, even for one vacancy), and so are
// hh and LinkedIn ids (see sameOriginDifferentPosting).
func identityKeys(s models.SourceRef) []string {
	raw := []string{s.ApplyURL}
	if !strings.HasPrefix(s.Source, "freehire") {
		raw = append(raw, s.URL)
	}
	var out []string
	for _, r := range raw {
		k := URLKey(r)
		if k == "" || strings.HasPrefix(k, "hh:") || strings.HasPrefix(k, "linkedin:") {
			continue
		}
		out = append(out, k)
	}
	return out
}

func hasListing(j models.Job) bool {
	for _, s := range j.Sources {
		if s.Listing() {
			return true
		}
	}
	return false
}

// singleEmployerFeed reports a feed that is one company's own board, where
// two listing ids are always two vacancies.
func singleEmployerFeed(feed string) bool {
	kind, _, ok := strings.Cut(feed, ":")
	return ok && employerSources[kind] && kind != "freehire"
}

// employerSources are where a vacancy comes from the employer itself; their
// apply link beats an aggregator's redirect.
var employerSources = map[string]bool{
	"greenhouse": true, "lever": true, "ashby": true, "workday": true, "freehire": true,
}

// namingSources give a clean display name for the company. Lever and Ashby
// only know the board slug ("mattermost"), Workday the legal entity
// ("94-1687665 Bank of America, National Association").
var namingSources = map[string]bool{"greenhouse": true, "freehire": true}

func fromSources(j models.Job, set map[string]bool) bool {
	for _, s := range j.Sources {
		if set[s.Source] {
			return true
		}
	}
	return false
}

// Merge folds a duplicate posting into the job that was seen first.
func Merge(dst, src models.Job) models.Job {
	if !fromSources(dst, employerSources) && fromSources(src, employerSources) && src.ApplyURL != "" {
		dst.ApplyURL = src.ApplyURL
	}
	// An aggregator can name the wrong company (an investor, a recruiting
	// agency); the employer's own board names it right.
	if !fromSources(dst, namingSources) && fromSources(src, namingSources) && src.Company != "" {
		dst.Company = src.Company
	}
	for _, s := range src.Sources {
		i := slices.IndexFunc(dst.Sources, func(d models.SourceRef) bool { return sourceKey(d) == sourceKey(s) })
		switch {
		case i < 0:
			dst.Sources = append(dst.Sources, s)
		default:
			if s.LastSeen.After(dst.Sources[i].LastSeen) {
				dst.Sources[i].LastSeen = s.LastSeen
			}
			if dst.Sources[i].ApplyURL == "" {
				dst.Sources[i].ApplyURL = s.ApplyURL // backfill cards saved before it was kept
			}
		}
	}
	for _, c := range src.Contacts {
		if !slices.ContainsFunc(dst.Contacts, func(d models.Contact) bool { return contactKey(d) == contactKey(c) }) {
			dst.Contacts = append(dst.Contacts, c)
		}
	}
	if !src.PostedAt.IsZero() && (dst.PostedAt.IsZero() || src.PostedAt.Before(dst.PostedAt)) {
		dst.PostedAt = src.PostedAt
	}
	if src.LastSeen.After(dst.LastSeen) {
		dst.LastSeen = src.LastSeen
	}
	if dst.Company == "" {
		dst.Company = src.Company
	}
	if !dst.Salary.Known() && src.Salary.Known() {
		dst.Salary = src.Salary
	}
	if dst.English == "" {
		dst.English = src.English
	}
	if dst.ApplyURL == "" {
		dst.ApplyURL = src.ApplyURL
	}
	if dst.Location == "" {
		dst.Location = src.Location
	}
	dst.Relocation = dst.Relocation || src.Relocation
	dst.Formats = union(dst.Formats, src.Formats)
	dst.Grades = union(dst.Grades, src.Grades)
	dst.Employment = union(dst.Employment, src.Employment)
	dst.Stack = union(dst.Stack, src.Stack)
	return dst
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+`)

// Shingles hashes every run of three consecutive words of the text.
func Shingles(text string) []uint64 {
	words := wordRe.FindAllString(strings.ToLower(text), maxWords)
	if len(words) < shingleSize {
		return nil
	}
	out := make([]uint64, 0, len(words))
	h := fnv.New64a()
	for i := 0; i+shingleSize <= len(words); i++ {
		h.Reset()
		for _, w := range words[i : i+shingleSize] {
			h.Write([]byte(w))
			h.Write([]byte{' '})
		}
		out = append(out, h.Sum64())
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Jaccard is |a∩b| / |a∪b| for two sorted, de-duplicated sets.
func Jaccard(a, b []uint64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var i, j, both int
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			both++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return float64(both) / float64(len(a)+len(b)-both)
}

var (
	// Explicit letter edges instead of \b, which is ASCII-only in RE2.
	companySuffix = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:inc|llc|ltd|gmbh|corp|co|ооо|тоо|ао|limited|group)(?:[^\p{L}]|$)`)
	nonAlnum      = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// roleKey is "company|title" normalized; empty when the company is unknown,
// because "Senior Go Developer" alone matches half the board.
func roleKey(j models.Job) string {
	company := nonAlnum.ReplaceAllString(companySuffix.ReplaceAllString(strings.ToLower(j.Company), ""), "")
	title := nonAlnum.ReplaceAllString(strings.ToLower(j.Title), "")
	if company == "" || title == "" {
		return ""
	}
	return company + "|" + title
}

func sourceKey(s models.SourceRef) string { return s.Source + "|" + s.ExternalID }

// ownURLKeys are the canonical URLs of the posting itself: where to apply
// and where it was published.
func ownURLKeys(j models.Job) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range append([]string{j.ApplyURL}, sourceURLs(j)...) {
		if k := URLKey(raw); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// postingKeys are the keys of a job's listings only: a channel post or an HN
// comment has an id of its own, but it is not a requisition.
func postingKeys(j models.Job) []string {
	var out []string
	for _, s := range j.Sources {
		if !s.Listing() {
			continue
		}
		for _, raw := range []string{s.URL, s.ApplyURL} {
			if k := URLKey(raw); k != "" && !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	if k := URLKey(j.ApplyURL); k != "" && hasListing(j) && !slices.Contains(out, k) {
		out = append(out, k)
	}
	return out
}

func sourceURLs(j models.Job) []string {
	out := make([]string, 0, 2*len(j.Sources))
	for _, s := range j.Sources {
		out = append(out, s.URL, s.ApplyURL)
	}
	return out
}

// contactURLKeys are the vacancy pages a post links in its text.
func contactURLKeys(j models.Job) []string {
	var out []string
	for _, c := range j.Contacts {
		if c.Kind != models.ContactURL {
			continue
		}
		if k := URLKey(c.Value); k != "" && IsVacancyKey(k) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// urlKeys are the canonical URLs that identify the posting itself: where to
// apply and where it was published, plus links in a channel post that point
// at a vacancy page (not at an article several vacancies cite).
func urlKeys(j models.Job) []string {
	var out []string
	seen := map[string]bool{}
	add := func(raw string, needVacancyShape bool) {
		k := URLKey(raw)
		if k == "" || seen[k] || (needVacancyShape && !IsVacancyKey(k)) {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	add(j.ApplyURL, false)
	for _, s := range j.Sources {
		add(s.URL, false)
		add(s.ApplyURL, false)
	}
	for _, c := range j.Contacts {
		if c.Kind == models.ContactURL {
			add(c.Value, true)
		}
	}
	return out
}

func contactKey(c models.Contact) string {
	return string(c.Kind) + ":" + strings.ToLower(strings.TrimSuffix(c.Value, "/"))
}

func union[T comparable](a, b []T) []T {
	for _, v := range b {
		if !slices.Contains(a, v) {
			a = append(a, v)
		}
	}
	return a
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// Consolidate merges stored jobs that turn out to be one vacancy: they share
// a posting URL of their own, or a channel post links exactly one known
// vacancy page. A post linking two vacancies is ambiguous and stays apart.
// The job seen first keeps its page.
func Consolidate(jobs []models.Job) []models.Job {
	order := make([]int, len(jobs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return jobs[a].FirstSeen.Compare(jobs[b].FirstSeen) })

	merged := make([]bool, len(jobs))
	fold := func(into, from int) {
		if jobs[from].FirstSeen.Before(jobs[into].FirstSeen) {
			into, from = from, into
		}
		jobs[into] = Merge(jobs[into], jobs[from])
		merged[from] = true
	}

	// Pass 1: the same own posting URL.
	owner := map[string]int{}
	for _, i := range order {
		keys := ownURLKeys(jobs[i])
		into := -1
		for _, k := range keys {
			if o, ok := owner[k]; ok && !merged[o] {
				into = o
				break
			}
		}
		if into >= 0 {
			fold(into, i)
			i = into
		}
		for _, k := range keys {
			if _, ok := owner[k]; !ok {
				owner[k] = i
			}
		}
	}

	// Pass 2: a channel or thread post that links one known vacancy page is
	// that vacancy. Only posts fold this way: two listings never merge
	// through links someone quoted next to them.
	for _, i := range order {
		if merged[i] || hasListing(jobs[i]) {
			continue
		}
		targets := map[int]bool{}
		for _, k := range contactURLKeys(jobs[i]) {
			if o, ok := owner[k]; ok && o != i && !merged[o] {
				targets[o] = true
			}
		}
		if len(targets) == 1 {
			for o := range targets {
				fold(o, i)
			}
		}
	}

	out := make([]models.Job, 0, len(jobs))
	for i, j := range jobs {
		if !merged[i] {
			out = append(out, j)
		}
	}
	return out
}
