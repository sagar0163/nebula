#!/usr/bin/env bash
# Creates a Python module with a syntax error (missing colon after def).
set -euo pipefail

command -v python3 >/dev/null 2>&1 || { echo "python3 not available" >&2; exit 127; }

cat > bad.py <<'EOF'
def greet(name)
    return "hello " + name


if __name__ == "__main__":
    print(greet("nebula"))
EOF
