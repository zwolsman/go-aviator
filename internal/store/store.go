package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
	"github.com/zwolsman/go-aviator/migrations"
)

// Open opens a database connection pool and runs migrations.
func Open(dsn string) (*sql.DB, *dbpkg.Queries, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("ping db: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrations: %w", err)
	}

	return db, dbpkg.New(db), nil
}

func runMigrations(db *sql.DB) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
