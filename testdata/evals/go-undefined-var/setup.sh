#!/usr/bin/env bash
# Creates a Go module whose main.go references a variable that is never declared.
set -euo pipefail

command -v go >/dev/null 2>&1 || { echo "go toolchain not available" >&2; exit 127; }

cat > go.mod <<'EOF'
module example.com/undefinedvar

go 1.21
EOF

cat > main.go <<'EOF'
package main

import "fmt"

func main() {
	values := []int{4, 9, 2}
	fmt.Println("max:", maximum)
	fmt.Println("known max:", maxOf(values))
}

func maxOf(vals []int) int {
	best := vals[0]
	for _, v := range vals[1:] {
		if v > best {
			best = v
		}
	}
	return best
}
EOF
