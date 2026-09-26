#!/usr/bin/env bash
# Creates a Go module that calls strings.ToUpper without importing "strings".
set -euo pipefail

command -v go >/dev/null 2>&1 || { echo "go toolchain not available" >&2; exit 127; }

cat > go.mod <<'EOF'
module example.com/missingimport

go 1.21
EOF

cat > main.go <<'EOF'
package main

import "fmt"

func main() {
	name := "nebula"
	fmt.Println(strings.ToUpper(name))
}
EOF
