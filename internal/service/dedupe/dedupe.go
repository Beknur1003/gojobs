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
)

// Index holds the known jobs and the lookup tables used to match new ones.
type Index struct {
	Jobs []models.Job

	shingles  [][]uint64
	bySource  map[string]int
	byContact map[string][]int
	byRole    map[string][]int
}

func NewIndex(jobs []models.Job) *Index {
	ix := &Index{
		bySource:  map[string]int{},
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

func (ix *Index) indexKeys(i int) {
	j := ix.Jobs[i]
	for _, s := range j.Sources {
		ix.bySource[sourceKey(s)] = i
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
func (ix *Index) FindDuplicate(j models.Job) (int, bool) {
	sh := Shingles(j.Text)

	related := map[int]bool{}
	for _, c := range j.Contacts {
		for _, i := range ix.byContact[contactKey(c)] {
			related[i] = true
		}
	}
	if k := roleKey(j); k != "" {
		for _, i := range ix.byRole[k] {
			if absDuration(ix.Jobs[i].PostedAt.Sub(j.PostedAt)) <= sameRoleWindow {
				return i, true
			}
		}
	}

	best, bestScore := -1, 0.0
	for i := range ix.Jobs {
		// Reposts happen within weeks; skip far-apart jobs cheaply.
		if absDuration(ix.Jobs[i].PostedAt.Sub(j.PostedAt)) > 60*24*time.Hour {
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

// Merge folds a duplicate posting into the job that was seen first.
func Merge(dst, src models.Job) models.Job {
	for _, s := range src.Sources {
		i := slices.IndexFunc(dst.Sources, func(d models.SourceRef) bool { return sourceKey(d) == sourceKey(s) })
		switch {
		case i < 0:
			dst.Sources = append(dst.Sources, s)
		case s.LastSeen.After(dst.Sources[i].LastSeen):
			dst.Sources[i].LastSeen = s.LastSeen
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
