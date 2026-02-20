package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/arawak/ganache/migrations"
)

var version = "dev"

func main() {
	fmt.Printf("ganache-migrate version %s\n", version)

	dsn := os.Getenv("GANACHE_DB_DSN")
	if dsn == "" {
		fmt.Println("GANACHE_DB_DSN is required")
		os.Exit(1)
	}
	direction := flag.String("direction", "up", "migration direction: up or down")
	flag.Parse()

	var err error
	switch *direction {
	case "up":
		err = migrations.Up(dsn)
	case "down":
		err = migrations.Down(dsn)
	default:
		err = fmt.Errorf("unknown direction: %s", *direction)
	}
	if err != nil {
		fmt.Println("migration error:", err)
		os.Exit(1)
	}
}
