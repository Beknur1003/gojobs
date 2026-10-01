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
}

// SourceRef points back to one place the vacancy was published. Every source
// is linked on the site: that is both honest and required by the APIs' terms.
type SourceRef struct {
	Source     string
	Feed       string
	Name       string
	ExternalID string
	URL        string
	PostedAt   time.Time
	LastSeen   time.Time // last run whose fetch of Feed still returned it
}

// Listing reports whether the source is a live list of open roles (a board,
// a company page) rather than a stream of posts (a channel, a thread).
// Only listings can tell that a vacancy has closed.
func (s SourceRef) Listing() bool {
	return s.Source != "telegram" && s.Source != "hn"
}

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
