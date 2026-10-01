// Package models holds the domain types shared by every layer. Stdlib only, no
// serialization tags: the JSON shape lives in the repository and site DTOs.
package models

import "time"

// Posting is one raw vacancy exactly as a source returned it, before any rule
// has looked at it. Sources fill only what they know; extraction fills the rest.
type Posting struct {
	Source     string // adapter kind: "telegram", "remoteok", "hn", ...
	Feed       string // one fetchable unit: "telegram:rabota_golang", "greenhouse:stripe"
	SourceName string // what the reader sees: "@rabota_golang", "Remote OK"
	ExternalID string // unique within Source: "rabota_golang/1274"
	URL        string // the original post or listing, always shown on the site

	Title    string
	Company  string
	Location string
	Text     string   // plain text with line breaks kept
	Links    []string // every href in the post, contacts are mined from these too
	Tags     []string
	// Hints is extra text from structured API fields ("Senior", "Salary:
	// $120k", "Europe only"). Rules read it; the site does not show it.
	Hints string

	// Structured fields some APIs provide. Zero means "unknown", and rules
	// extract the value from Text instead.
	Salary     Salary
	Remote     bool
	Grades     []Grade
	Employment []Employment
	ApplyURL   string

	PostedAt time.Time

	// GoOnly marks sources that already filtered to Go (a Go channel, an API
	// queried with tag=golang). Rules still check the text, just less strictly.
	GoOnly bool

	// Stub means the source only confirmed that a listing it served before is
	// still open; its description was not downloaded again. A stub refreshes
	// a known job and is otherwise ignored.
	Stub bool
}

// Job is a normalized vacancy as it is stored and published. Several postings
// of the same vacancy (reposts across channels) collapse into one Job.
type Job struct {
	ID      string // stable, derived from the first posting
	Slug    string
	Title   string
	Company string
	Text    string
	Summary string

	Salary     Salary
	Formats    []WorkFormat
	Grades     []Grade
	Employment []Employment
	English    string // "a1".."c2", empty when not stated
	Stack      []string
	Location   string
	Relocation bool
	Lang       string // "ru" or "en": the language of the post

	Contacts []Contact
	ApplyURL string
	Sources  []SourceRef

	PostedAt  time.Time // earliest posting of this vacancy
	FirstSeen time.Time
	LastSeen  time.Time

	// Closed is derived on every run, never stored: a listing vanished from
	// the board that published it. Telegram posts never close, they age out.
	Closed bool

	// Regions and GoMain are derived on every build, never stored, like
	// Closed: their rules can change without collecting anything again.
	Regions []Region // where the role can be worked from; empty when unknown
	GoMain  bool     // Go is the main language of the role, not one of several
}

// Region is a coarse area a vacancy can be worked from, for the region filter.
type Region string

const (
	RegionWorld  Region = "world" // remote from any country
	RegionKZ     Region = "kz"
	RegionCIS    Region = "cis" // Russia and the CIS, Kazakhstan included
	RegionEurope Region = "europe"
	RegionNA     Region = "na" // the USA and Canada
	RegionLatAm  Region = "latam"
	RegionAsia   Region = "asia" // Asia, the Middle East and Oceania
)

// SourceRef points back to one place the vacancy was published. Every source
// is linked on the site: that is both honest and required by the APIs' terms.
type SourceRef struct {
	Source     string
	Feed       string
	Name       string
	ExternalID string
	URL        string
	// ApplyURL is this copy's own link to the posting. A merged job keeps one
	// ApplyURL for its button, but every copy's link stays here so all the
	// posting ids the job stands for remain known to deduplication.
	ApplyURL string
	PostedAt time.Time
	LastSeen time.Time // last run whose fetch of Feed still returned it
}

// streams are sources read as a window of recent posts, not as the complete
// list of open roles: a role missing from today's window may still be open.
// Channels and threads, but also feeds whose API only serves the newest page
// or few (Arbeitnow, RSS feeds, Remote OK's latest-100).
var streams = map[string]bool{
	"telegram": true, "hn": true, "arbeitnow": true, "workingnomads": true, "djinni": true,
	"golangprojects": true, "weworkremotely": true, "remoteok": true,
	"freehire-stream": true, // channel posts relayed by freehire
}

// Listing reports whether the source is a complete, live list of open roles
// (a company board, a searchable job board) rather than a stream of posts.
// Only a listing can tell that a vacancy has closed; stream posts age out.
func (s SourceRef) Listing() bool { return !streams[s.Source] }

// HasDirectContact reports whether a candidate can write to a human directly,
// which is the main reason this board exists.
func (j Job) HasDirectContact() bool {
	for _, c := range j.Contacts {
		if c.Kind == ContactEmail || c.Kind == ContactTelegram {
			return true
		}
	}
	return false
}

type ContactKind string

const (
	ContactEmail    ContactKind = "email"
	ContactTelegram ContactKind = "telegram"
	ContactURL      ContactKind = "url"
)

type Contact struct {
	Kind  ContactKind
	Value string // address, @handle without "@", or absolute URL
}

// Salary keeps the stated numbers and a monthly USD estimate for filtering.
// The estimate uses fixed rates, so the site always labels it approximate.
type Salary struct {
	Min      int
	Max      int
	Currency string // ISO code: USD, EUR, RUB, KZT, GBP...
	Period   Period

	MonthlyUSDMin int
	MonthlyUSDMax int
}

func (s Salary) Known() bool { return s.Min > 0 || s.Max > 0 }

type Period string

const (
	PeriodMonth Period = "month"
	PeriodYear  Period = "year"
	PeriodHour  Period = "hour"
)

type WorkFormat string

const (
	FormatRemote WorkFormat = "remote"
	FormatHybrid WorkFormat = "hybrid"
	FormatOffice WorkFormat = "office"
)

type Grade string

const (
	GradeIntern Grade = "intern"
	GradeJunior Grade = "junior"
	GradeMiddle Grade = "middle"
	GradeSenior Grade = "senior"
	GradeLead   Grade = "lead"
)

type Employment string

const (
	EmploymentFull     Employment = "fulltime"
	EmploymentPart     Employment = "parttime"
	EmploymentContract Employment = "contract"
)

// FeedStatus is how the last fetch of one feed went. It is published on the
// site, so a dead channel is visible instead of silently shrinking the board.
type FeedStatus struct {
	LastOK  time.Time // last successful fetch; zero if it never worked
	LastRun time.Time
	Count   int // postings the last successful fetch returned
	Error   string
}

// BoardStatus is the last check of one company board in the discovery
// registry: thousands of boards are probed in rotation, and only those that
// had Go roles at their last check are fetched every day.
type BoardStatus struct {
	Checked  time.Time
	OK       bool // false: the board is gone or failed
	Postings int  // listings the board returned
	Go       int  // of those, Go vacancies
	Fails    int  // consecutive failed checks
	Error    string
}
