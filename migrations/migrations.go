package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed *.sql
var FS embed.FS

func Up(dsn string) error {
	m, err := migrator(dsn)
	if err != nil {
		return err
	}
	err = m.Up()
	if err == migrate.ErrNoChange {
		return nil
	}
	if err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func Down(dsn string) error {
	m, err := migrator(dsn)
	if err != nil {
		return err
	}
	err = m.Down()
	if err == migrate.ErrNoChange {
		return nil
	}
	if err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

func migrator(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(FS, ".")
	if err != nil {
		return nil, fmt.Errorf("create migration source: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	driver, err := mysql.WithInstance(db, &mysql.Config{})
	if err != nil {
		return nil, fmt.Errorf("create driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "mysql", driver)
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return m, nil
}
