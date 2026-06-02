// Command crawler builds a local DuckDB of GitHub non-fork repos with stars >= 10.
//
// Subcommands:
//
//	init-db      create/verify the DuckDB schema
//	token-check  verify a GitHub token can be resolved
//	stats        print a summary of the local database
//	enumerate    (P2) enumerate repos via the GitHub Search API
//	enrich       (P3) enrich stored repos via ecosyste.ms
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hoveychen/opensource-world/internal/aggregate"
	"github.com/hoveychen/opensource-world/internal/db"
	"github.com/hoveychen/opensource-world/internal/ecosystems"
	"github.com/hoveychen/opensource-world/internal/ghtoken"
	"github.com/hoveychen/opensource-world/internal/github"
)

const defaultDBPath = "data/repos.duckdb"

// defaultMailto joins the ecosyste.ms polite pool (~15k/hr vs ~5k/hr anonymous).
// Override per-run with -mailto or the ECOSYSTEMS_MAILTO env var; set empty to
// stay anonymous.
const defaultMailto = "yuheng_chen@outlook.com"

func usage() {
	fmt.Fprint(os.Stderr, `crawler — local GitHub repo database builder

Usage: crawler <command> [flags]

Commands:
  init-db       create/verify the DuckDB schema (path: `+defaultDBPath+`)
  token-check   verify a GitHub token can be resolved
  stats         print a summary of the local database
  enumerate     enumerate repos via GitHub Search (P2)
  enrich        enrich stored repos via ecosyste.ms
  aggregate     write JSON summaries for the visualization site

Env:
  DB_PATH       override the DuckDB file path
`)
}

func dbPath() string {
	if p := os.Getenv("DB_PATH"); p != "" {
		return p
	}
	return defaultDBPath
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init-db":
		err = cmdInitDB()
	case "token-check":
		err = cmdTokenCheck()
	case "stats":
		err = cmdStats()
	case "enumerate":
		err = cmdEnumerate(os.Args[2:])
	case "enrich":
		err = cmdEnrich(os.Args[2:])
	case "aggregate":
		err = cmdAggregate(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func ensureDataDir(path string) error {
	if dir := dirOf(path); dir != "" {
		return os.MkdirAll(dir, 0o755)
	}
	return nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return ""
}

func cmdInitDB() error {
	if err := ensureDataDir(dbPath()); err != nil {
		return err
	}
	database, err := db.Open(dbPath())
	if err != nil {
		return err
	}
	defer database.Close()
	fmt.Printf("schema ready at %s\n", dbPath())
	return nil
}

func cmdTokenCheck() error {
	tok, err := ghtoken.Resolve()
	if err != nil {
		return err
	}
	masked := tok
	if len(masked) > 8 {
		masked = masked[:4] + "…" + masked[len(masked)-4:]
	}
	fmt.Printf("GitHub token resolved: %s\n", masked)
	return nil
}

func cmdAggregate(args []string) error {
	fs := flag.NewFlagSet("aggregate", flag.ExitOnError)
	out := fs.String("out", "web/public/data", "output directory for the JSON summaries")
	fs.Parse(args)

	database, err := db.Open(dbPath())
	if err != nil {
		return err
	}
	defer database.Close()

	if err := aggregate.Run(database, *out); err != nil {
		return err
	}
	log.Printf("aggregated JSON written to %s", *out)
	return nil
}

func cmdStats() error {
	database, err := db.Open(dbPath())
	if err != nil {
		return err
	}
	defer database.Close()
	s, err := database.Stats()
	if err != nil {
		return err
	}
	fmt.Printf("repos:           %d\n", s.TotalRepos)
	fmt.Printf("enriched:        %d\n", s.Enriched)
	fmt.Printf("forks (want 0):  %d\n", s.Forks)
	fmt.Printf("windows done:    %d\n", s.WindowsDone)
	fmt.Printf("stars range:     %d .. %d\n", s.MinStars.Int64, s.MaxStars.Int64)
	return nil
}

// withMaxRuntime derives a context that cancels after d (if d > 0). When it
// fires, the running command sees ctx.Err() and exits cleanly with progress
// saved — used to stay under CI per-job time limits.
func withMaxRuntime(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	log.Printf("max-runtime %s; will stop cleanly before then", d)
	return context.WithTimeout(ctx, d)
}

func cmdEnumerate(args []string) error {
	fs := flag.NewFlagSet("enumerate", flag.ExitOnError)
	minStars := fs.Int("min-stars", 10, "lower star bound (inclusive)")
	maxStars := fs.Int("max-stars", 0, "upper star bound (0 = auto-probe current max)")
	from := fs.String("from", "", "earliest created date YYYY-MM-DD (default 2007-01-01)")
	to := fs.String("to", "", "latest created date YYYY-MM-DD (default today)")
	maxRuntime := fs.Duration("max-runtime", 0, "stop cleanly after this duration, saving progress (0 = no limit); e.g. 5h")
	fs.Parse(args)

	opts := github.EnumerateOptions{MinStars: *minStars, MaxStars: *maxStars}
	var err error
	if *from != "" {
		if opts.From, err = time.Parse("2006-01-02", *from); err != nil {
			return fmt.Errorf("bad -from: %w", err)
		}
	}
	if *to != "" {
		if opts.To, err = time.Parse("2006-01-02", *to); err != nil {
			return fmt.Errorf("bad -to: %w", err)
		}
	}

	tok, err := ghtoken.Resolve()
	if err != nil {
		return err
	}
	if err := ensureDataDir(dbPath()); err != nil {
		return err
	}
	database, err := db.Open(dbPath())
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := withMaxRuntime(ctx, *maxRuntime)
	defer cancel()

	client := github.NewClient(tok)
	log.Printf("enumerating stars>=%d (non-fork) into %s", *minStars, dbPath())
	if err := client.Enumerate(ctx, database, opts); err != nil {
		if ctx.Err() != nil {
			log.Printf("interrupted; progress saved (resume by re-running)")
			return nil
		}
		return err
	}
	s, _ := database.Stats()
	log.Printf("done. repos=%d windows=%d", s.TotalRepos, s.WindowsDone)
	return nil
}

func cmdEnrich(args []string) error {
	fs := flag.NewFlagSet("enrich", flag.ExitOnError)
	limit := fs.Int("limit", 0, "max repos to enrich this run (0 = all pending)")
	mailto := fs.String("mailto", defaultMailto, "contact email for the ecosyste.ms polite pool (~15k/hr); empty = anonymous ~5k/hr")
	maxRuntime := fs.Duration("max-runtime", 0, "stop cleanly after this duration, saving progress (0 = no limit); e.g. 5h")
	fs.Parse(args)
	if env := os.Getenv("ECOSYSTEMS_MAILTO"); env != "" {
		*mailto = env
	}

	database, err := db.Open(dbPath())
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := withMaxRuntime(ctx, *maxRuntime)
	defer cancel()

	client := ecosystems.NewClient(*mailto)
	pending, err := database.CountPendingEnrichment()
	if err != nil {
		return err
	}
	log.Printf("enriching via ecosyste.ms (%d pending total)", pending)

	stamped, failed, err := ecosystems.Enrich(ctx, database, client, *limit)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		log.Printf("interrupted after %d (skipped %d after errors); progress saved (resume by re-running)", stamped, failed)
		return nil
	}
	log.Printf("done. enriched %d repos this run (skipped %d after errors)", stamped, failed)
	return nil
}
