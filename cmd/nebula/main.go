package main

import (
	"github.com/sagar0163/nebula/internal/cli"

	// Register the ncruces WASM-based SQLite driver (CGO_ENABLED=0 compatible).
	_ "github.com/ncruces/go-sqlite3/driver"
)

func main() {
	cli.Execute()
}
