// Command gojobs collects Go vacancies from free sources and renders the board
// as a static site.
//
//	gojobs run     fetch every source, update data/jobs.json, rebuild the site
//	gojobs build   rebuild the site from data/jobs.json without fetching
//	gojobs serve   preview the built site on http://localhost:8080
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
	"github.com/Beknur1003/gojobs/internal/service/pipeline"
)

// hnThreads is how many monthly "Who is hiring" threads to read: the current
// one fills up over the month, so the previous one still has live posts.
const hnThreads = 2

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
		err = run(ctx, log, *configPath, *only)
	case "build":
		err = build(log, *configPath)
	case "serve":
		err = serve(ctx, log, *configPath, *addr)
	default:
		err = fmt.Errorf("unknown command %q: use run, build or serve", cmd)
	}
	if err != nil {
		log.Error("failed", "cmd", cmd, "err", err)
		pprof.StopCPUProfile()
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, configPath, only string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	st, err := store.Load(cfg.Paths.Data)
	if err != nil {
		return err
	}

	client := httpx.New(cfg.HTTP.UserAgent,
		time.Duration(cfg.HTTP.TimeoutSeconds)*time.Second,
		time.Duration(cfg.HTTP.DelayMillis)*time.Millisecond)

	sources := buildSources(cfg, client, st.Feeds)
	if only != "" {
		sources = filterSources(sources, only)
	}
	log.Info("collecting", "feeds", len(sources), "known_jobs", len(st.Jobs))

	started := time.Now()
	jobs, feeds, stats := pipeline.New(log, cfg.Site.KeepDays).Run(ctx, sources, st.Jobs, st.Feeds)
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted, nothing saved: %w", ctx.Err())
	}
	log.Info("collected",
		"fetched", stats.Fetched, "go_vacancies", stats.Accepted, "new", stats.New,
		"merged", stats.Merged, "updated", stats.Updated, "pruned", stats.Pruned,
		"failed_feeds", len(stats.Failed), "took", time.Since(started).Round(time.Second))

	st = store.State{UpdatedAt: time.Now().UTC(), Feeds: feeds, Jobs: jobs}
	if err := store.Save(cfg.Paths.Data, st); err != nil {
		return err
	}
	return render(log, cfg, st)
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
	for i := range st.Jobs {
		st.Jobs[i].Closed = pipeline.Closed(st.Jobs[i], st.Feeds)
	}
	return render(log, cfg, st)
}

func render(log *slog.Logger, cfg config.Config, st store.State) error {
	b, err := site.New(site.Config{
		Title: cfg.Site.Title, BaseURL: cfg.Site.BaseURL, BasePath: cfg.Site.BasePath,
		Repo: cfg.Site.Repo, FeedDays: cfg.Site.FeedDays, OutDir: cfg.Paths.Out,
	})
	if err != nil {
		return err
	}
	res, err := b.Build(st.Jobs, st.Feeds, st.UpdatedAt)
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

func buildSources(cfg config.Config, client *httpx.Client, feeds map[string]models.FeedStatus) []pipeline.Source {
	var out []pipeline.Source

	for _, ch := range cfg.Telegram.Channels {
		pages := cfg.Telegram.DailyPages
		if feeds["telegram:"+ch.Name].LastOK.IsZero() {
			pages = cfg.Telegram.BackfillPages // first time: read the history
		}
		out = append(out, telegram.New(client, telegram.Channel{Name: ch.Name, GoOnly: ch.GoOnly}, pages))
	}

	if cfg.APIs.RemoteOK {
		out = append(out, boards.NewRemoteOK(client))
	}
	if cfg.APIs.Remotive {
		out = append(out, boards.NewRemotive(client))
	}
	if cfg.APIs.Himalayas {
		out = append(out, boards.NewHimalayas(client))
	}
	if cfg.APIs.Jobicy {
		out = append(out, boards.NewJobicy(client))
	}
	if cfg.APIs.WWR {
		out = append(out, boards.NewWWR(client))
	}
	if cfg.APIs.HN {
		out = append(out, hn.New(client, hnThreads))
	}

	for _, b := range cfg.Greenhouse {
		out = append(out, ats.NewGreenhouse(client, b))
	}
	for _, b := range cfg.Lever {
		out = append(out, ats.NewLever(client, b))
	}
	for _, b := range cfg.Ashby {
		out = append(out, ats.NewAshby(client, b))
	}
	return out
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
