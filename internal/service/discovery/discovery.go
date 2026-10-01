// Package discovery decides which company boards a run fetches. There are
// tens of thousands of candidate boards and only a small share has Go roles
// at any moment, so every board is probed in rotation (a slice per run) and
// only boards that had Go roles at their last check are fetched every day.
package discovery

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/httpx"
)

type Config struct {
	PerRun  map[string]int // kind ("greenhouse") -> boards to probe per run
	Recheck time.Duration  // a checked board is left alone this long
}

// Plan lists the feeds to fetch: active boards, then boards due a check.
// Manual boards (listed in sources.yaml) are fetched anyway and skipped here.
// all ignores PerRun, for a full sweep.
func Plan(cfg Config, seeds map[string][]string, manual map[string]bool, boards map[string]models.BoardStatus, now time.Time, all bool) (active, probe []string) {
	for name, b := range boards {
		if b.OK && b.Go > 0 && !manual[name] {
			active = append(active, name)
		}
	}
	sort.Strings(active)

	var perKind [][]string
	for _, kind := range sortedKinds(seeds, boards) {
		type due struct {
			name    string
			checked time.Time
			order   int
		}
		var list []due
		seen := map[string]bool{}
		add := func(name string, order int) {
			if seen[name] || manual[name] {
				return
			}
			seen[name] = true
			b, known := boards[name]
			if known && (b.Go > 0 && b.OK || now.Sub(b.Checked) < cfg.Recheck) {
				return // active, or checked recently
			}
			list = append(list, due{name: name, checked: b.Checked, order: order})
		}
		for i, slug := range seeds[kind] {
			add(kind+":"+slug, i)
		}
		for name := range boards {
			if strings.HasPrefix(name, kind+":") {
				add(name, len(seeds[kind]))
			}
		}

		// Never-checked boards first, in seed order; then the longest unchecked.
		sort.SliceStable(list, func(i, k int) bool {
			if list[i].checked.Equal(list[k].checked) {
				return list[i].order < list[k].order
			}
			return list[i].checked.Before(list[k].checked)
		})
		n := len(list)
		if !all {
			n = min(n, cfg.PerRun[kind])
		}
		names := make([]string, n)
		for i, d := range list[:n] {
			names[i] = d.name
		}
		perKind = append(perKind, names)
	}
	return active, interleave(perKind)
}

// interleave merges the per-kind lists round-robin. Each kind lives on its
// own API host with its own pacing, so a mixed queue keeps all of them busy;
// a queue sorted by kind would crawl one host at a time.
func interleave(lists [][]string) []string {
	var out []string
	for i := 0; ; i++ {
		added := false
		for _, l := range lists {
			if i < len(l) {
				out = append(out, l[i])
				added = true
			}
		}
		if !added {
			return out
		}
	}
}

// Result is what one fetch of a board found.
type Result struct {
	Postings int
	Go       int
	Err      error
}

// maxFails is how many failed checks in a row demote an active board.
const maxFails = 3

// Record updates the registry after a run. A board that is gone (404) is
// marked dead. Any other failure still counts as a check, so a broken board
// waits Recheck instead of jumping the queue every run. An active board
// keeps its place through a bad morning or two and is demoted after
// maxFails in a row; a failed fetch that still found Go roles keeps (or
// makes) the board active, so those roles are refreshed daily.
func Record(boards map[string]models.BoardStatus, results map[string]Result, now time.Time) {
	for name, r := range results {
		prev := boards[name]
		switch {
		case r.Err == nil:
			boards[name] = models.BoardStatus{Checked: now, OK: true, Postings: r.Postings, Go: r.Go}
		case r.Go > 0:
			// Partly failed but still serving Go roles: the board works, keep
			// it active and do not count this against it.
			boards[name] = models.BoardStatus{Checked: now, OK: true, Postings: max(prev.Postings, r.Postings),
				Go: max(prev.Go, r.Go), Error: r.Err.Error()}
		case errors.Is(r.Err, httpx.ErrNotFound):
			boards[name] = models.BoardStatus{Checked: now, OK: false, Error: "not found"}
		default:
			st := prev
			st.Checked = now
			st.Error = r.Err.Error()
			st.Fails++
			if st.Fails >= maxFails {
				st.OK, st.Go = false, 0
			}
			boards[name] = st
		}
	}
}

// Kinds is every board kind discovery knows how to probe.
var Kinds = []string{"greenhouse", "lever", "ashby", "workday"}

// IsBoard reports whether a feed name is a company board.
func IsBoard(feed string) bool {
	kind, _, ok := strings.Cut(feed, ":")
	if !ok {
		return false
	}
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func sortedKinds(seeds map[string][]string, boards map[string]models.BoardStatus) []string {
	set := map[string]bool{}
	for k := range seeds {
		set[k] = true
	}
	for name := range boards {
		if kind, _, ok := strings.Cut(name, ":"); ok {
			set[kind] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
