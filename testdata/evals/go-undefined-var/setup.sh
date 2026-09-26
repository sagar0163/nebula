#!/usr/bin/env bash
# Creates a Go module where main.go calls an undefined function 'printGreeting'
# when the actual function is named 'greet'. The fix is a one-word rename.
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
	fmt.Println(greetUser("nebula"))
}

func greet(name string) string {
	return "hello " + name
}
EOF
