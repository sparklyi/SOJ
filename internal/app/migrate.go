package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"SOJ/internal/config"

	"github.com/jackc/pgx/v5"
)

func RunMigrate(ctx context.Context, args []string, stdout, stderr io.Writer) (err error) {
	fs := flag.NewFlagSet("soj-migrate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	dir := fs.String("dir", "", "migration directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stdout, "usage: soj-migrate [--dir internal/migrations] up")
		return flag.ErrHelp
	}
	if fs.Arg(0) != "up" {
		return fmt.Errorf("unsupported migration command %q", fs.Arg(0))
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *dir != "" {
		cfg.Migrations.Dir = *dir
	}
	if cfg.Database.DSN == "" {
		return errors.New("SOJ_DATABASE_DSN is required")
	}

	conn, err := pgx.Connect(ctx, cfg.Database.DSN)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(ctx); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close migration database connection: %w", closeErr))
		}
	}()

	return migrateUp(ctx, conn, cfg.Migrations.Dir, stdout)
}

func migrateUp(ctx context.Context, conn *pgx.Conn, dir string, stdout io.Writer) error {
	if _, err := conn.Exec(ctx, `create table if not exists schema_migrations (
	version text primary key,
	applied_at timestamptz not null default now()
)`); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(file, ".up.sql")
		applied, err := migrationApplied(ctx, conn, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("%s: %w", file, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations(version) values ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "applied %s\n", file)
	}

	return nil
}

func migrationApplied(ctx context.Context, conn *pgx.Conn, version string) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `select exists(select 1 from schema_migrations where version = $1)`, version).Scan(&exists)
	return exists, err
}
