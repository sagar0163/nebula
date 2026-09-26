#!/usr/bin/env bash
# Creates a Go module that assigns a string constant to an int variable.
set -euo pipefail

command -v go >/dev/null 2>&1 || { echo "go toolchain not available" >&2; exit 127; }

cat > go.mod <<'EOF'
module example.com/wrongtype

go 1.21
EOF

cat > main.go <<'EOF'
package main

import "fmt"

func main() {
	var count int = "twelve"
	fmt.Println(count)
}
EOF
