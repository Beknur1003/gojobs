// Package config loads sources.yaml: the site settings and the list of places
// vacancies are collected from. Adding a channel or a company is a config edit.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Site       Site       `yaml:"site"`
	Paths      Paths      `yaml:"paths"`
	Telegram   Telegram   `yaml:"telegram"`
	APIs       APIs       `yaml:"apis"`
	Greenhouse []string   `yaml:"greenhouse"`
	Lever      []string   `yaml:"lever"`
	Ashby      []string   `yaml:"ashby"`
	Workday    []string   `yaml:"workday"` // "tenant|wd5|site"
	Discovery  Discovery  `yaml:"discovery"`
	HTTP       HTTPConfig `yaml:"http"`
}

// Discovery probes thousands of company boards in rotation and keeps the ones
// that have Go roles. See internal/service/discovery.
type Discovery struct {
	Enabled      bool           `yaml:"enabled"`
	SeedsDir     string         `yaml:"seeds_dir"`
	Registry     string         `yaml:"registry"`
	Cache        string         `yaml:"cache"`
	RecheckDays  int            `yaml:"recheck_days"`
	PerRun       map[string]int `yaml:"per_run"`
	WorkdayQuery string         `yaml:"workday_query"`
}

type Site struct {
	Title    string `yaml:"title"`
	BaseURL  string `yaml:"base_url"`  // absolute, for sitemap and og tags
	BasePath string `yaml:"base_path"` // "/gojobs" on a GitHub project page, "" on a custom domain
	Repo     string `yaml:"repo"`      // shown in the footer: where to suggest a source

	// FeedDays is how far back the feed goes. Older jobs stay in the data file
	// (the next repost is still deduplicated against them) but leave the site.
	FeedDays int `yaml:"feed_days"`
	// KeepDays is how long a job stays in the data file at all.
	KeepDays int `yaml:"keep_days"`
}

type Paths struct {
	Data string `yaml:"data"`
	Out  string `yaml:"out"`
}

type Telegram struct {
	// BackfillPages is used for a channel seen for the first time, DailyPages
	// afterwards. One page is about 20 posts.
	BackfillPages int       `yaml:"backfill_pages"`
	DailyPages    int       `yaml:"daily_pages"`
	Channels      []Channel `yaml:"channels"`
}

type Channel struct {
	Name   string `yaml:"name"`
	GoOnly bool   `yaml:"go_only"` // the whole channel is about Go
	// Search reads only posts matching this word ("golang"): for large
	// channels with every kind of role, where Go posts are a few a month.
	Search string `yaml:"search"`
}

type APIs struct {
	RemoteOK  bool `yaml:"remoteok"`
	Remotive  bool `yaml:"remotive"`
	Himalayas bool `yaml:"himalayas"`
	Jobicy    bool `yaml:"jobicy"`
	HN        bool `yaml:"hn"`
	WWR       bool `yaml:"weworkremotely"`
	Djinni    bool `yaml:"djinni"`
	GoProj    bool `yaml:"golangprojects"`
	Arbeitnow bool `yaml:"arbeitnow"`
	WorkNomad bool `yaml:"workingnomads"`
	Freehire  bool `yaml:"freehire"` // freehire.me open API: ~90 ATS platforms
	Habr      bool `yaml:"habr"`     // Habr Career
	GetMatch  bool `yaml:"getmatch"` // getmatch.ru, read through its sitemap and pages
}

type HTTPConfig struct {
	UserAgent      string `yaml:"user_agent"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	// DelayMillis is the pause between requests to the same host. Every source
	// here is free; being polite is what keeps it that way.
	DelayMillis int `yaml:"delay_millis"`
	// HostDelayMillis overrides it for APIs built for machine traffic.
	HostDelayMillis map[string]int `yaml:"host_delay_millis"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config.Load: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("config.Load: parse %s: %w", path, err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("config.Load: %w", err)
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Site.Title == "" {
		c.Site.Title = "Go Jobs"
	}
	c.Site.BasePath = strings.TrimSuffix(c.Site.BasePath, "/")
	c.Site.BaseURL = strings.TrimSuffix(c.Site.BaseURL, "/")
	if c.Site.FeedDays == 0 {
		c.Site.FeedDays = 60
	}
	if c.Site.KeepDays == 0 {
		c.Site.KeepDays = 180
	}
	if c.Paths.Data == "" {
		c.Paths.Data = "data/jobs.json"
	}
	if c.Paths.Out == "" {
		c.Paths.Out = "public"
	}
	if c.Telegram.BackfillPages == 0 {
		c.Telegram.BackfillPages = 10
	}
	if c.Telegram.DailyPages == 0 {
		c.Telegram.DailyPages = 3
	}
	if c.HTTP.UserAgent == "" {
		c.HTTP.UserAgent = "gojobs/1.0 (+https://github.com/Beknur1003/gojobs)"
	}
	if c.HTTP.TimeoutSeconds == 0 {
		c.HTTP.TimeoutSeconds = 30
	}
	if c.HTTP.DelayMillis == 0 {
		c.HTTP.DelayMillis = 700
	}
	if c.Discovery.SeedsDir == "" {
		c.Discovery.SeedsDir = "data/seeds"
	}
	if c.Discovery.Registry == "" {
		c.Discovery.Registry = "data/boards.json"
	}
	if c.Discovery.Cache == "" {
		c.Discovery.Cache = "cache/ats.json"
	}
	if c.Discovery.RecheckDays == 0 {
		c.Discovery.RecheckDays = 30
	}
	if c.Discovery.WorkdayQuery == "" {
		c.Discovery.WorkdayQuery = "golang"
	}
}

func (c *Config) validate() error {
	if c.Site.BaseURL == "" {
		return errors.New("site.base_url is required (used in sitemap and share links)")
	}
	if c.Site.BasePath != "" && !strings.HasPrefix(c.Site.BasePath, "/") {
		return fmt.Errorf("site.base_path %q must start with /", c.Site.BasePath)
	}
	if c.Site.FeedDays > c.Site.KeepDays {
		return fmt.Errorf("site.feed_days (%d) cannot exceed site.keep_days (%d)", c.Site.FeedDays, c.Site.KeepDays)
	}
	for _, ch := range c.Telegram.Channels {
		if ch.Name == "" || strings.ContainsAny(ch.Name, "/@ ") {
			return fmt.Errorf("telegram channel %q: use the bare name, e.g. rabota_golang", ch.Name)
		}
	}
	return nil
}
