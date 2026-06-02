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
	"fmt"
	"os"

	"github.com/hoveychen/opensource-world/internal/db"
	"github.com/hoveychen/opensource-world/internal/ghtoken"
)

const defaultDBPath = "data/repos.duckdb"

func usage() {
	fmt.Fprint(os.Stderr, `crawler — local GitHub repo database builder

Usage: crawler <command> [flags]

Commands:
  init-db       create/verify the DuckDB schema (path: `+defaultDBPath+`)
  token-check   verify a GitHub token can be resolved
  stats         print a summary of the local database
  enumerate     enumerate repos via GitHub Search (P2)
  enrich        enrich stored repos via ecosyste.ms (P3)

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
	fmt.Printf("windows done:    %d\n", s.WindowsDone)
	fmt.Printf("stars range:     %d .. %d\n", s.MinStars.Int64, s.MaxStars.Int64)
	return nil
}

// cmdEnumerate and cmdEnrich are wired up in P2 and P3.
func cmdEnumerate(args []string) error {
	return fmt.Errorf("enumerate: not yet implemented (P2)")
}

func cmdEnrich(args []string) error {
	return fmt.Errorf("enrich: not yet implemented (P3)")
}
