package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

type policy struct {
	target      int64
	versions    map[int64]bool
	guard       bool
	online      bool
	maintenance bool
}

func (p policy) check(applied []int64) error {
	var current int64
	for _, version := range applied {
		if version != 0 && !p.versions[version] {
			return fmt.Errorf("database migration %d is not in this image; downgrades and unknown history are unsupported", version)
		}
		if version > current {
			current = version
		}
	}
	if p.guard && current > 0 && current < p.target && !p.online && !p.maintenance {
		return errors.New("pending migrations on an existing database require a supported source and online-upgrade approval, or an acknowledged maintenance window")
	}
	return nil
}

type guardedLocker struct {
	lock.SessionLocker
	policy policy
}

func (l guardedLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	if lockErr := l.SessionLocker.SessionLock(ctx, conn); lockErr != nil {
		return lockErr
	}
	checkErr := l.checkHistory(ctx, conn)
	if checkErr != nil {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return errors.Join(checkErr, l.SessionUnlock(unlockCtx, conn))
	}
	return nil
}

func (l guardedLocker) checkHistory(ctx context.Context, conn *sql.Conn) error {
	var exists bool
	if queryErr := conn.QueryRowContext(ctx, "SELECT to_regclass('public.goose_db_version') IS NOT NULL").Scan(&exists); queryErr != nil {
		return queryErr
	}
	if !exists {
		return nil
	}
	rows, err := conn.QueryContext(ctx, "SELECT version_id FROM (SELECT DISTINCT ON (version_id) version_id, is_applied FROM public.goose_db_version ORDER BY version_id, id DESC) versions WHERE is_applied ORDER BY version_id")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var applied []int64
	for rows.Next() {
		var version int64
		if scanErr := rows.Scan(&version); scanErr != nil {
			return scanErr
		}
		applied = append(applied, version)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return rowsErr
	}
	return l.policy.check(applied)
}

func newProvider(db *sql.DB, files fs.FS, expected string, p policy) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 120), lock.WithUnlockTimeout(1, 5))
	if err != nil {
		return nil, err
	}
	guard := &guardedLocker{SessionLocker: locker, policy: p}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(guard), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return nil, err
	}
	sources := provider.ListSources()
	guard.policy.target = sources[len(sources)-1].Version
	guard.policy.versions = make(map[int64]bool, len(sources))
	for _, source := range sources {
		guard.policy.versions[source.Version] = true
	}
	if expected != "" && expected != strconv.FormatInt(guard.policy.target, 10) {
		return nil, fmt.Errorf("expected schema target %q does not match image target %d", expected, guard.policy.target)
	}
	return provider, nil
}

func migrate(ctx context.Context, provider *goose.Provider, command string, output io.Writer) error {
	if _, statusErr := provider.Status(ctx); statusErr != nil {
		return statusErr
	}
	switch command {
	case "up":
		if _, upErr := provider.Up(ctx); upErr != nil {
			return upErr
		}
	case "down":
		if _, downErr := provider.Down(ctx); downErr != nil {
			return downErr
		}
	case "status":
	default:
		return fmt.Errorf("unsupported command %q", command)
	}
	statuses, err := provider.Status(ctx)
	if err != nil {
		return err
	}
	for _, entry := range statuses {
		if command == "up" && entry.State != goose.StateApplied {
			return fmt.Errorf("migration %d is not applied after migration", entry.Source.Version)
		}
		if _, writeErr := fmt.Fprintf(output, "%d %s\n", entry.Source.Version, entry.State); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(output)
	directory := flags.String("dir", "/migrations", "migration directory")
	expected := flags.String("target", os.Getenv("MIGRATION_SCHEMA_TARGET"), "expected image schema target")
	timeout := flags.Duration("timeout", 9*time.Minute, "database operation deadline")
	if parseErr := flags.Parse(args); parseErr != nil {
		return parseErr
	}
	command := "up"
	if flags.NArg() == 1 {
		command = flags.Arg(0)
	}
	if flags.NArg() > 1 || (command != "up" && command != "down" && command != "status" && command != "target") {
		return errors.New("expected up, down, status, or target")
	}
	if *timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if command != "target" {
		if filename := os.Getenv("DATABASE_URL_FILE"); filename != "" {
			contents, readErr := os.ReadFile(filename)
			if readErr != nil {
				return errors.New("DATABASE_URL_FILE is not readable")
			}
			databaseURL = strings.TrimSpace(string(contents))
		}
		if databaseURL == "" {
			return errors.New("DATABASE_URL or DATABASE_URL_FILE is required")
		}
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	config.RuntimeParams["search_path"] = "public"
	db := stdlib.OpenDB(*config)
	defer func() { _ = db.Close() }()
	p := policy{guard: os.Getenv("MIGRATION_GUARD_UPGRADE") == "true", online: os.Getenv("MIGRATION_ONLINE_APPROVED") == "true", maintenance: os.Getenv("MIGRATION_MAINTENANCE_ACKNOWLEDGED") == "true"}
	if p.guard && (command != "up" || *expected == "") {
		return errors.New("helm migrations require up and an explicit schema target")
	}
	provider, err := newProvider(db, os.DirFS(*directory), *expected, p)
	if err != nil {
		return err
	}
	if command == "target" {
		sources := provider.ListSources()
		_, err = fmt.Fprintln(output, sources[len(sources)-1].Version)
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	if migrateErr := migrate(runCtx, provider, command, output); migrateErr != nil {
		message := strings.ReplaceAll(migrateErr.Error(), databaseURL, "[redacted database URL]")
		return errors.New(message)
	}
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
