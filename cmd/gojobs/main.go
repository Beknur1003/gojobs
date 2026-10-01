// Command gojobs collects Go vacancies from free sources and renders the board
// as a static site.
//
//	gojobs run       fetch every source, probe a slice of company boards,
//	                 update data/, rebuild the site
//	gojobs discover  probe every company board that is due, in saved batches
//	                 (the first full sweep; safe to interrupt and rerun)
//	gojobs channels  rate candidate Telegram channels listed in a file
//	gojobs build     rebuild the site from data/ without fetching
//	gojobs serve     preview the built site locally
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/Beknur1003/gojobs/internal/config"
	"github.com/Beknur1003/gojobs/internal/handler/site"
	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/httpx"
	"github.com/Beknur1003/gojobs/internal/repository/sources/ats"
	"github.com/Beknur1003/gojobs/internal/repository/sources/boards"
	"github.com/Beknur1003/gojobs/internal/repository/sources/hn"
	"github.com/Beknur1003/gojobs/internal/repository/sources/telegram"
	"github.com/Beknur1003/gojobs/internal/repository/store"
	"github.com/Beknur1003/gojobs/internal/service/discovery"
	"github.com/Beknur1003/gojobs/internal/service/extract"
	"github.com/Beknur1003/gojobs/internal/service/pipeline"
)

const (
	// hnThreads is how many monthly "Who is hiring" threads to read: the
	// current one fills up over the month, so the previous one still has
	// live posts.
	hnThreads = 2
	// sweepBatch is how many boards a full sweep fetches between saves.
	sweepBatch = 400
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cmd := "run"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	configPath := fs.String("config", "sources.yaml", "path to the sources config")
	only := fs.String("only", "", "run only feeds whose name contains this text (debugging)")
	addr := fs.String("addr", "localhost:8080", "address for serve")
	list := fs.String("list", "", "file with Telegram channel names, one per line (channels)")
	cpuProfile := fs.String("cpuprofile", "", "write a CPU profile to this file")
	_ = fs.Parse(args)

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err == nil && pprof.StartCPUProfile(f) == nil {
			defer pprof.StopCPUProfile()
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "run":
		err = collect(ctx, log, *configPath, *only, false)
	case "discover":
		err = collect(ctx, log, *configPath, *only, true)
	case "channels":
		err = rateChannels(ctx, log, *configPath, *list)
	case "build":
		err = build(log, *configPath)
	case "serve":
		err = serve(ctx, log, *configPath, *addr)
	default:
		err = fmt.Errorf("unknown command %q: use run, discover, build or serve", cmd)
	}
	if err != nil {
		log.Error("failed", "cmd", cmd, "err", err)
		pprof.StopCPUProfile()
		os.Exit(1)
	}
}

// collect is both the daily run (sweep=false: every fixed source plus active
// boards plus a slice of due boards, in one batch) and the full sweep
// (sweep=true: only company boards, all that are due, saved batch by batch).
func collect(ctx context.Context, log *slog.Logger, configPath, only string, sweep bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	st, err := store.Load(cfg.Paths.Data)
	if err != nil {
		return err
	}
	registry, err := store.LoadBoards(cfg.Discovery.Registry)
	if err != nil {
		return err
	}
	seeds, err := store.LoadSeeds(cfg.Discovery.SeedsDir, discovery.Kinds)
	if err != nil {
		return err
	}
	cache, err := ats.LoadCache(cfg.Discovery.Cache, extract.RulesVersion)
	if err != nil {
		return err
	}

	client := newClient(cfg)
	filter := ats.Filter{Title: extract.CandidateTitle, Keep: extract.Candidate}

	fixed := fixedSources(cfg, client, filter, cache, st.Feeds, log)
	manual := map[string]bool{}
	for _, s := range fixed {
		manual[s.Name()] = true
	}

	var sources []pipeline.Source
	if !sweep {
		sources = fixed
	}
	if cfg.Discovery.Enabled {
		dcfg := discovery.Config{PerRun: cfg.Discovery.PerRun, Recheck: time.Duration(cfg.Discovery.RecheckDays) * 24 * time.Hour}
		active, probe := discovery.Plan(dcfg, seeds, manual, registry, time.Now(), sweep)
		log.Info("boards planned", "active", len(active), "probe", len(probe), "known", len(registry))
		for _, name := range append(active, probe...) {
			if s, ok := boardSource(name, client, filter, cache, cfg.Discovery.WorkdayQuery); ok {
				sources = append(sources, s)
			}
		}
	}
	if only != "" {
		sources = filterSources(sources, only)
	}

	batch := len(sources)
	if sweep {
		batch = sweepBatch
	}
	p := pipeline.New(log, cfg.Site.KeepDays)
	if sweep {
		// A sweep is mostly company boards spread over a few API hosts and
		// thousands of Workday hosts; extra workers keep every host busy.
		p.SetParallel(24)
	}
	jobs, feeds := st.Jobs, st.Feeds
	started := time.Now()

	for from := 0; from < len(sources); from += batch {
		part := sources[from:min(from+batch, len(sources))]
		log.Info("collecting", "feeds", len(part), "done", from, "total", len(sources), "known_jobs", len(jobs))

		nextJobs, nextFeeds, stats := p.Run(ctx, part, jobs, feeds)
		if ctx.Err() != nil {
			return fmt.Errorf("interrupted, the last batch was not saved: %w", ctx.Err())
		}
		jobs, feeds = nextJobs, nextFeeds
		recordBoards(registry, stats.Feeds)
		log.Info("collected",
			"fetched", stats.Fetched, "go_vacancies", stats.Accepted, "new", stats.New,
			"merged", stats.Merged, "updated", stats.Updated, "seen", stats.Seen, "pruned", stats.Pruned,
			"failed_feeds", len(stats.Failed), "took", time.Since(started).Round(time.Second))

		st = store.State{UpdatedAt: time.Now().UTC(), Feeds: pruneFeeds(feeds, jobs), Jobs: jobs}
		if err := saveAll(cfg, st, registry, cache); err != nil {
			return err
		}
	}
	return render(log, cfg, st, registry)
}

// recordBoards feeds the outcome of company boards back into the registry.
func recordBoards(registry map[string]models.BoardStatus, feeds map[string]pipeline.FeedResult) {
	results := map[string]discovery.Result{}
	for name, fr := range feeds {
		if discovery.IsBoard(name) {
			results[name] = discovery.Result{Postings: fr.Postings, Go: fr.Go, Err: fr.Err}
		}
	}
	discovery.Record(registry, results, time.Now().UTC())
}

// pruneFeeds drops the status of company boards that hold no jobs: there are
// thousands of them and their history lives in the registry instead.
func pruneFeeds(feeds map[string]models.FeedStatus, jobs []models.Job) map[string]models.FeedStatus {
	used := map[string]bool{}
	for _, j := range jobs {
		for _, s := range j.Sources {
			used[s.Feed] = true
		}
	}
	out := make(map[string]models.FeedStatus, len(feeds))
	for name, fs := range feeds {
		if !discovery.IsBoard(name) || used[name] {
			out[name] = fs
		}
	}
	return out
}

func saveAll(cfg config.Config, st store.State, registry map[string]models.BoardStatus, cache *ats.Cache) error {
	if err := store.Save(cfg.Paths.Data, st); err != nil {
		return err
	}
	if err := store.SaveBoards(cfg.Discovery.Registry, registry); err != nil {
		return err
	}
	return cache.Save()
}

func build(log *slog.Logger, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	st, err := store.Load(cfg.Paths.Data)
	if err != nil {
		return err
	}
	registry, err := store.LoadBoards(cfg.Discovery.Registry)
	if err != nil {
		return err
	}
	return render(log, cfg, st, registry)
}

// render rebuilds the site. Closed, Regions and GoMain are derived, never
// stored, so they are recomputed here for every job, whatever this run did
// or did not fetch, and `gojobs build` applies changed rules at once.
func render(log *slog.Logger, cfg config.Config, st store.State, registry map[string]models.BoardStatus) error {
	for i := range st.Jobs {
		j := &st.Jobs[i]
		j.Closed = pipeline.Closed(*j, st.Feeds)
		j.Regions = extract.Regions(*j)
		j.GoMain = extract.GoMain(j.Title, j.Text)
	}
	b, err := site.New(site.Config{
		Title: cfg.Site.Title, BaseURL: cfg.Site.BaseURL, BasePath: cfg.Site.BasePath,
		Repo: cfg.Site.Repo, FeedDays: cfg.Site.FeedDays, OutDir: cfg.Paths.Out,
	})
	if err != nil {
		return err
	}
	var summary site.BoardsSummary
	for _, s := range registry {
		summary.Checked++
		if s.OK && s.Go > 0 {
			summary.Active++
		}
	}
	res, err := b.Build(st.Jobs, st.Feeds, summary, st.UpdatedAt)
	if err != nil {
		return err
	}
	log.Info("site built", "dir", cfg.Paths.Out, "feed", res.Feed, "pages", res.Pages)
	return nil
}

// serve previews the site under the same base path it will have on GitHub
// Pages, so links behave exactly as in production.
func serve(ctx context.Context, log *slog.Logger, configPath, addr string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	prefix := cfg.Site.BasePath + "/"
	mux.Handle(prefix, http.StripPrefix(cfg.Site.BasePath, http.FileServer(http.Dir(cfg.Paths.Out))))
	if prefix != "/" {
		mux.Handle("/", http.RedirectHandler(prefix, http.StatusFound))
	}

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("serving", "url", "http://"+addr+prefix)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func newClient(cfg config.Config) *httpx.Client {
	hostDelay := map[string]time.Duration{}
	for host, ms := range cfg.HTTP.HostDelayMillis {
		hostDelay[host] = time.Duration(ms) * time.Millisecond
	}
	return httpx.New(cfg.HTTP.UserAgent,
		time.Duration(cfg.HTTP.TimeoutSeconds)*time.Second,
		time.Duration(cfg.HTTP.DelayMillis)*time.Millisecond,
		hostDelay)
}

// fixedSources are the hand-picked feeds of sources.yaml, fetched every run.
func fixedSources(cfg config.Config, client *httpx.Client, filter ats.Filter, cache *ats.Cache, feeds map[string]models.FeedStatus, log *slog.Logger) []pipeline.Source {
	var out []pipeline.Source

	for _, ch := range cfg.Telegram.Channels {
		pages := cfg.Telegram.DailyPages
		if feeds["telegram:"+ch.Name].LastOK.IsZero() {
			pages = cfg.Telegram.BackfillPages // first time: read the history
		}
		out = append(out, telegram.New(client, telegram.Channel{Name: ch.Name, GoOnly: ch.GoOnly, Search: ch.Search}, pages))
	}

	apis := []struct {
		on  bool
		src pipeline.Source
	}{
		{cfg.APIs.RemoteOK, boards.NewRemoteOK(client)},
		{cfg.APIs.Remotive, boards.NewRemotive(client)},
		{cfg.APIs.Himalayas, boards.NewHimalayas(client)},
		{cfg.APIs.Jobicy, boards.NewJobicy(client)},
		{cfg.APIs.WWR, boards.NewWWR(client)},
		{cfg.APIs.Djinni, boards.NewDjinni(client)},
		{cfg.APIs.GoProj, boards.NewGolangProjects(client)},
		{cfg.APIs.Arbeitnow, boards.NewArbeitnow(client)},
		{cfg.APIs.WorkNomad, boards.NewWorkingNomads(client)},
		{cfg.APIs.Freehire, boards.NewFreehire(client)},
		{cfg.APIs.Habr, boards.NewHabr(client)},
		{cfg.APIs.GetMatch, boards.NewGetMatch(client)},
		{cfg.APIs.HN, hn.New(client, hnThreads)},
	}
	for _, a := range apis {
		if a.on {
			out = append(out, a.src)
		}
	}

	manual := map[string][]string{"greenhouse": cfg.Greenhouse, "lever": cfg.Lever, "ashby": cfg.Ashby, "workday": cfg.Workday}
	for kind, slugs := range manual {
		for _, slug := range slugs {
			s, ok := boardSource(kind+":"+slug, client, filter, cache, cfg.Discovery.WorkdayQuery)
			if !ok {
				log.Warn("skipping malformed board", "board", kind+":"+slug)
				continue
			}
			out = append(out, s)
		}
	}
	return out
}

// boardSource builds the adapter for a registry name like "lever:neon".
func boardSource(name string, client *httpx.Client, filter ats.Filter, cache *ats.Cache, workdayQuery string) (pipeline.Source, bool) {
	kind, slug, ok := strings.Cut(name, ":")
	if !ok || slug == "" {
		return nil, false
	}
	switch kind {
	case "greenhouse":
		return ats.NewGreenhouse(client, slug, filter, cache), true
	case "lever":
		return ats.NewLever(client, slug, filter), true
	case "ashby":
		return ats.NewAshby(client, slug, filter), true
	case "workday":
		s, err := ats.NewWorkday(client, slug, workdayQuery, filter, cache)
		if err != nil {
			return nil, false
		}
		return s, true
	default:
		return nil, false
	}
}

func filterSources(in []pipeline.Source, only string) []pipeline.Source {
	var out []pipeline.Source
	for _, s := range in {
		if strings.Contains(s.Name(), only) {
			out = append(out, s)
		}
	}
	return out
}

// rateChannels reads the newest page of each candidate channel and prints how
// much of it is Go vacancies, judged by the same rules as the daily run.
func rateChannels(ctx context.Context, log *slog.Logger, configPath, listPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(listPath)
	if err != nil {
		return fmt.Errorf("channels: %w", err)
	}
	known := map[string]bool{}
	for _, ch := range cfg.Telegram.Channels {
		known[strings.ToLower(ch.Name)] = true
	}

	client := newClient(cfg)
	fmt.Println("channel\tposts\tvacancies\tgo\tlast_post\tstatus")
	for _, line := range strings.Split(string(raw), "\n") {
		name := strings.TrimPrefix(strings.TrimSpace(line), "@")
		if name == "" || strings.HasPrefix(name, "#") || known[strings.ToLower(name)] {
			continue
		}
		posts, err := telegram.New(client, telegram.Channel{Name: name}, 1).Fetch(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			fmt.Printf("%s\t0\t0\t0\t-\terror\n", name)
			continue
		}
		var vacancies, goJobs int
		var last time.Time
		for _, p := range posts {
			if p.PostedAt.After(last) {
				last = p.PostedAt
			}
			if !extract.IsVacancy(p) {
				continue
			}
			vacancies++
			title, _ := extract.TitleAndCompany(p.Text)
			if extract.IsGo(p, title) {
				goJobs++
			}
		}
		status := "ok"
		switch {
		case len(posts) == 0:
			status = "empty"
		case time.Since(last) > 30*24*time.Hour:
			status = "stale"
		}
		lastText := "-"
		if !last.IsZero() {
			lastText = last.Format("2006-01-02")
		}
		fmt.Printf("%s\t%d\t%d\t%d\t%s\t%s\n", name, len(posts), vacancies, goJobs, lastText, status)
	}
	log.Info("channels rated")
	return nil
}
