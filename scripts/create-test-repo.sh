#!/usr/bin/env bash
# Create a minimal test repo for SWE-bench testing

set -euo pipefail

REPO_DIR="/tmp/swebench-test-repo"
rm -rf "$REPO_DIR"
mkdir -p "$REPO_DIR"
cd "$REPO_DIR"

git init -q
git config user.email "test@test.com"
git config user.name "Test User"

# Create the buggy code
cat > math.py << 'EOF'
def add(a, b):
    return a - b  # BUG: should be a + b

def subtract(a, b):
    return a + b  # BUG: should be a - b
EOF

# Create test file
cat > test_math.py << 'EOF'
def test_add():
    assert add(2, 3) == 5

def test_subtract():
    assert subtract(5, 3) == 2
EOF

git add .
git commit -q -m "initial commit with bugs"

echo "Created test repo at $REPO_DIR"
echo "Commit: $(git rev-parse HEAD)"