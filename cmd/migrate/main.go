package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/example/ganache/migrations"
)

func main() {
	dsn := os.Getenv("GANACHE_DB_DSN")
	if dsn == "" {
		fmt.Println("GANACHE_DB_DSN is required")
		os.Exit(1)
	}
	dir := flag.String("dir", "up", "migration direction: up or down")
	flag.Parse()

	var err error
	switch *dir {
	case "up":
		err = migrations.Up(dsn)
	case "down":
		err = migrations.Down(dsn)
	default:
		err = fmt.Errorf("unknown direction: %s", *dir)
	}
	if err != nil {
		fmt.Println("migration error:", err)
		os.Exit(1)
	}
}
