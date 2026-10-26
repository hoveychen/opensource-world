package db

import (
	"fmt"
	"os"
	"strings"
)

// tables lists every table the crawler owns, in the order Rebuild copies them.
var tables = []string{"repos", "crawl_windows"}

// Verify checks that the database at path is fully readable and writable.
//
// A DuckDB file snapshotted while another process was still writing it (the CI
// job once gzipped the DB while a timed-out crawler was mid-shutdown) opens and
// answers simple counts fine, yet points at blocks past the end of the file, so
// the first commit that touches them fails. Verify therefore reads every column
// of every row and then commits a write and forces a checkpoint, which together
// touch all live data and the metadata/free-list blocks a later commit needs.
func Verify(path string) error {
	d, err := Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	for _, t := range tables {
		var n, h any
		// hash(t) hashes the whole row, forcing every column segment to be read.
		q := fmt.Sprintf(`SELECT count(*), bit_xor(hash(t)) FROM %s t`, t)
		if err := d.QueryRow(q).Scan(&n, &h); err != nil {
			return fmt.Errorf("scan %s: %w", t, err)
		}
	}
	for _, stmt := range []string{
		`CREATE OR REPLACE TABLE _verify_probe AS SELECT 1 AS x`,
		`DROP TABLE _verify_probe`,
		`FORCE CHECKPOINT`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			return fmt.Errorf("write probe (%s): %w", stmt, err)
		}
	}
	return nil
}

// Rebuild copies every table of the database at src into a fresh database at
// dst (which must not exist), recreating the schema and indexes from scratch.
// src is attached read-only, so a damaged file whose data blocks are intact but
// whose index or metadata blocks are not can still be salvaged.
func Rebuild(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("rebuild target %q already exists", dst)
	}
	d, err := Open(dst)
	if err != nil {
		return err
	}
	defer d.Close()
	esc := strings.ReplaceAll(src, "'", "''")
	if _, err := d.Exec(fmt.Sprintf(`ATTACH '%s' AS src (READ_ONLY)`, esc)); err != nil {
		return fmt.Errorf("attach %q: %w", src, err)
	}
	for _, t := range tables {
		// BY NAME: columns added by migrations sit at the end of an old table but
		// may be ordered differently in a fresh one built from schema.sql.
		q := fmt.Sprintf(`INSERT INTO main.%s BY NAME SELECT * FROM src.%s`, t, t)
		if _, err := d.Exec(q); err != nil {
			return fmt.Errorf("copy %s: %w", t, err)
		}
	}
	if _, err := d.Exec(`DETACH src`); err != nil {
		return fmt.Errorf("detach: %w", err)
	}
	if _, err := d.Exec(`FORCE CHECKPOINT`); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	return nil
}
