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
	configFlags := config.RegisterFlags(fs)
	dir := fs.String("dir", "", "migration directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if configFlags.Print {
		return config.Print(stdout, configFlags.File)
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stdout, "usage: soj-migrate [--config config.yaml] [--dir internal/migrations] up")
		return flag.ErrHelp
	}
	if fs.Arg(0) != "up" {
		return fmt.Errorf("unsupported migration command %q", fs.Arg(0))
	}

	cfg, err := config.Load(config.Options{Role: config.RoleMigrate, File: configFlags.File})
	if err != nil {
		return err
	}
	if *dir != "" {
		cfg.Migrations.Dir = *dir
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

const baselineVersion = "000001_baseline"
const migrationLockID int64 = 6276700125626430680

func migrateUp(ctx context.Context, conn *pgx.Conn, dir string, stdout io.Writer) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migration directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("no migrations in %s", dir)
	}

	for _, file := range files {
		version := strings.TrimSuffix(file, ".up.sql")
		content, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return err
		}
		applied, err := applyMigration(ctx, conn, version, content)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if applied {
			_, _ = fmt.Fprintf(stdout, "applied %s\n", file)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, conn *pgx.Conn, version string, content []byte) (bool, error) {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return false, err
	}

	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
		return false, err
	}
	if applied {
		return false, nil
	}
	if version == baselineVersion {
		var initialized bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT FROM schema_migrations)
			OR EXISTS (
				SELECT FROM pg_catalog.pg_class
				WHERE relnamespace = current_schema()::regnamespace
				  AND relkind IN ('r', 'p')
				  AND relname <> 'schema_migrations'
			)`).Scan(&initialized); err != nil {
			return false, err
		}
		if initialized {
			return false, errors.New("database already has tables or migration history; see docs/schema-cutover.md")
		}
	}

	if _, err := tx.Exec(ctx, string(content)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
