// Package pipeline is one daily run: fetch every feed, keep the Go vacancies,
// fold duplicates into the jobs already known, and age out old ones.
package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/httpx"
	"github.com/Beknur1003/gojobs/internal/service/dedupe"
	"github.com/Beknur1003/gojobs/internal/service/extract"
)

// Source is one feed: a channel, a board, a company's career page.
type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]models.Posting, error)
}

type Pipeline struct {
	log      *slog.Logger
	now      func() time.Time
	keep     time.Duration
	parallel int
}

func New(log *slog.Logger, keepDays int) *Pipeline {
	return &Pipeline{log: log, now: time.Now, keep: time.Duration(keepDays) * 24 * time.Hour, parallel: 8}
}

// SetParallel changes how many feeds are fetched at once. Per-host pacing in
// the HTTP client still applies, so more workers only help across hosts.
func (p *Pipeline) SetParallel(n int) {
	if n > 0 {
		p.parallel = n
	}
}

type Stats struct {
	Fetched  int // postings returned by all feeds
	Accepted int // of those, Go vacancies
	New      int
	Merged   int // new postings of an already known vacancy
	Updated  int // postings seen again
	Seen     int // known listings a board confirmed without a new download
	Pruned   int
	Failed   []string

	// Feeds is the outcome per feed, for the discovery registry.
	Feeds map[string]FeedResult
}

type FeedResult struct {
	Postings int
	Go       int // Go vacancies this feed holds now
	Err      error
}

// Run merges a fresh fetch into jobs and returns the new state of both.
func (p *Pipeline) Run(ctx context.Context, sources []Source, jobs []models.Job, feeds map[string]models.FeedStatus) ([]models.Job, map[string]models.FeedStatus, Stats) {
	now := p.now().UTC()
	stats := Stats{Feeds: map[string]FeedResult{}}

	results := p.fetchAll(ctx, sources)

	var accepted []models.Job
	var stubs []models.Posting
	for _, r := range results {
		stats.Feeds[r.name] = FeedResult{Postings: len(r.posts), Err: r.err}
		status := feeds[r.name]
		status.LastRun = now
		if errors.Is(r.err, httpx.ErrNotFound) && len(r.posts) == 0 && companyBoard(r.name) {
			// The company left this ATS: an empty answer, so its listings
			// close instead of lingering as open. An aggregator's 404 is
			// more likely a moved API and stays an ordinary failure.
			p.log.Warn("board gone, closing its listings", "feed", r.name)
			status.Error = "not found"
			status.LastOK = now
			status.Count = 0
			feeds = withStatus(feeds, r.name, status)
			continue
		}
		if r.err != nil {
			status.Error = r.err.Error()
			stats.Failed = append(stats.Failed, r.name)
			p.log.Warn("feed failed", "feed", r.name, "err", r.err, "partial", len(r.posts))
		} else {
			status.Error = ""
			status.LastOK = now
			status.Count = len(r.posts)
		}
		feeds = withStatus(feeds, r.name, status)

		stats.Fetched += len(r.posts)
		noise := learnNoise(r.name, r.posts)
		for _, post := range r.posts {
			if post.Stub {
				stubs = append(stubs, post)
				continue
			}
			if job, ok := p.accept(post, noise, now); ok {
				accepted = append(accepted, job)
			}
		}
	}
	stats.Accepted = len(accepted)

	// Oldest first, so the earliest posting of a vacancy becomes its page.
	sort.SliceStable(accepted, func(a, b int) bool { return accepted[a].PostedAt.Before(accepted[b].PostedAt) })

	ix := dedupe.NewIndex(jobs)
	for _, j := range accepted {
		if i, ok := ix.FindSource(j.Sources[0]); ok {
			ix.Replace(i, refresh(ix.Jobs[i], j))
			stats.Updated++
			continue
		}
		if i, ok := ix.FindDuplicate(j); ok {
			ix.Replace(i, dedupe.Merge(ix.Jobs[i], j))
			stats.Merged++
			continue
		}
		ix.Add(j)
		stats.New++
	}

	goPerFeed := map[string]int{}
	for _, j := range accepted {
		goPerFeed[j.Sources[0].Feed]++
	}
	// A stub of a listing that is already a job keeps it open. A stub of
	// anything else was a role checked before and found not to be Go.
	for _, st := range stubs {
		if i, ok := ix.FindSource(models.SourceRef{Source: st.Source, ExternalID: st.ExternalID}); ok {
			ix.Jobs[i] = touch(ix.Jobs[i], st, now)
			goPerFeed[st.Feed]++
			stats.Seen++
		}
	}
	for name, fr := range stats.Feeds {
		fr.Go = goPerFeed[name]
		stats.Feeds[name] = fr
	}

	out := make([]models.Job, 0, len(ix.Jobs))
	// Jobs created apart can turn out to share a posting URL once a new
	// source links both; fold them before publishing.
	for _, j := range dedupe.Consolidate(ix.Jobs) {
		if now.Sub(postedOrSeen(j)) > p.keep {
			stats.Pruned++
			continue
		}
		j.Closed = Closed(j, feeds)
		out = append(out, j)
	}
	return out, feeds, stats
}

func (p *Pipeline) accept(post models.Posting, noise extract.Noise, now time.Time) (models.Job, bool) {
	if !post.PostedAt.IsZero() && now.Sub(post.PostedAt) > p.keep {
		return models.Job{}, false
	}
	// Cheap checks first: thousands of company-board roles are not Go, and
	// full extraction is the expensive part of a run.
	if !extract.MayBeGo(post) {
		return models.Job{}, false
	}
	if post.Title != "" && !extract.IsGo(post, post.Title) {
		return models.Job{}, false
	}

	job := extract.Normalize(post, noise)

	// Classify on the cleaned text: a channel footer full of #golang tags
	// must not make every post a Go vacancy.
	cleaned := post
	cleaned.Text = job.Text
	if !extract.IsVacancy(cleaned) || !extract.IsGo(cleaned, job.Title) {
		return models.Job{}, false
	}

	if job.PostedAt.IsZero() {
		job.PostedAt = now
	}
	job.FirstSeen, job.LastSeen = now, now
	job.Sources[0].LastSeen = now
	return job, true
}

// companyBoard reports a feed that is one company's career page on an ATS.
func companyBoard(feed string) bool {
	kind, _, ok := strings.Cut(feed, ":")
	switch kind {
	case "greenhouse", "lever", "ashby", "workday":
		return ok
	}
	return false
}

// touch records that a board still lists the job.
func touch(j models.Job, st models.Posting, now time.Time) models.Job {
	j.LastSeen = now
	for k := range j.Sources {
		if j.Sources[k].Source == st.Source && j.Sources[k].ExternalID == st.ExternalID {
			j.Sources[k].LastSeen = now
		}
	}
	return j
}

// refresh applies a re-fetched posting to its job. A single-source job takes
// the fresh extraction (the post may be edited, the rules may be better) but
// keeps its identity; a merged job only gains what was missing.
func refresh(old, fresh models.Job) models.Job {
	if len(old.Sources) > 1 {
		return dedupe.Merge(old, fresh)
	}
	fresh.ID, fresh.Slug, fresh.FirstSeen = old.ID, old.Slug, old.FirstSeen
	if !old.PostedAt.IsZero() && old.PostedAt.Before(fresh.PostedAt) {
		fresh.PostedAt = old.PostedAt
	}
	return fresh
}

// Closed reports a vacancy whose every listing has disappeared from a feed
// that has been fetched successfully since.
//
// Stream sources (a channel post, a feed window) cannot tell either way and
// are ignored, so an employer removing the role closes it even if a channel
// once reposted it. A job with only stream sources never closes; it ages out.
func Closed(j models.Job, feeds map[string]models.FeedStatus) bool {
	listed := false
	for _, s := range j.Sources {
		if !s.Listing() {
			continue
		}
		listed = true
		if !feeds[s.Feed].LastOK.After(s.LastSeen.Add(time.Hour)) {
			return false
		}
	}
	return listed
}

type fetchResult struct {
	name  string
	posts []models.Posting
	err   error
}

func (p *Pipeline) fetchAll(ctx context.Context, sources []Source) []fetchResult {
	results := make([]fetchResult, len(sources))
	sem := make(chan struct{}, p.parallel)
	var wg sync.WaitGroup

	for i, src := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = fetchResult{name: src.Name(), err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			start := time.Now()
			posts, err := src.Fetch(ctx)
			for k := range posts {
				posts[k].Feed = src.Name()
			}
			results[i] = fetchResult{name: src.Name(), posts: posts, err: err}
			p.log.Info("feed fetched", "feed", src.Name(), "postings", len(posts), "took", time.Since(start).Round(time.Millisecond))
		}()
	}
	wg.Wait()
	return results
}

// noiseWindow is how many consecutive posts are compared at once. Channels
// change their footer every few months, so a footer is "frequent" within a
// window of recent posts, not across the whole history.
const noiseWindow = 60

// learnNoise finds what a Telegram channel repeats under its posts: footer
// lines, and handles or links that show up in a large share of posts (sister
// channels, the admin, a folder invite). Emails are exempt: an agency that
// posts often still has a real inbox.
func learnNoise(feed string, posts []models.Posting) extract.Noise {
	noise := extract.Noise{Lines: map[string]bool{}, Contacts: map[string]bool{}}
	channel, isTelegram := strings.CutPrefix(feed, "telegram:")
	if !isTelegram {
		return noise
	}
	noise.Contacts[extract.ContactKey(models.ContactTelegram, channel)] = true

	sorted := append([]models.Posting(nil), posts...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].PostedAt.After(sorted[b].PostedAt) })
	for start := 0; start < len(sorted); start += noiseWindow {
		window := sorted[start:min(start+noiseWindow, len(sorted))]
		if len(window) < 3 && start > 0 {
			window = sorted[max(0, len(sorted)-noiseWindow):] // a short tail joins the previous window
		}
		learnWindow(window, noise)
	}
	return noise
}

func learnWindow(posts []models.Posting, noise extract.Noise) {
	if len(posts) < 3 {
		return // too few to tell a footer from a coincidence
	}
	// A footer sits under nearly every post of its era; an active recruiter
	// under a fraction of them. A handle written out in the text needs half
	// the window to count as footer. A link hidden under a word ("Задачи" ->
	// t.me/go_problems_lib) names nobody to the reader, so a fifth is enough.
	visibleBar := max(3, len(posts)/2)
	hiddenBar := max(3, len(posts)/5)

	lineCount := map[string]int{}
	visible := map[string]int{}
	hidden := map[string]int{}
	for _, p := range posts {
		// Channels repeat a header ("Обсуждение: @chat") as well as a footer.
		for _, l := range edgeLines(p.Text, 3, 4) {
			lineCount[l]++
		}
		inText := map[string]bool{}
		for _, c := range extract.Contacts(p.Text, nil, nil, false) {
			inText[extract.ContactKey(c.Kind, c.Value)] = true
		}
		for _, c := range extract.Contacts(p.Text, p.Links, nil, true) {
			if c.Kind == models.ContactEmail {
				continue
			}
			k := extract.ContactKey(c.Kind, c.Value)
			if inText[k] {
				visible[k]++
			} else {
				hidden[k]++
			}
		}
	}
	for l, n := range lineCount {
		if n >= visibleBar {
			noise.Lines[l] = true
		}
	}
	for k, n := range visible {
		if n >= visibleBar {
			noise.Contacts[k] = true
		}
	}
	for k, n := range hidden {
		if n >= hiddenBar {
			noise.Contacts[k] = true
		}
	}
}

// edgeLines returns the first head and last tail non-empty lines, cleaned
// and de-duplicated: where channels put their headers and footers.
func edgeLines(text string, head, tail int) []string {
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		if l := extract.CleanLine(raw); l != "" {
			lines = append(lines, l)
		}
	}
	seen := map[string]bool{}
	var out []string
	add := func(l string) {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	for _, l := range lines[:min(head, len(lines))] {
		add(l)
	}
	for _, l := range lines[max(0, len(lines)-tail):] {
		add(l)
	}
	return out
}

func postedOrSeen(j models.Job) time.Time {
	if !j.PostedAt.IsZero() {
		return j.PostedAt
	}
	return j.FirstSeen
}

func withStatus(feeds map[string]models.FeedStatus, name string, s models.FeedStatus) map[string]models.FeedStatus {
	if feeds == nil {
		feeds = map[string]models.FeedStatus{}
	}
	feeds[name] = s
	return feeds
}
