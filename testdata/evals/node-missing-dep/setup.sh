#!/usr/bin/env bash
# Creates a package.json with no "build" script, so `npm run build` fails.
set -euo pipefail

command -v npm >/dev/null 2>&1 || { echo "npm not available" >&2; exit 127; }

cat > package.json <<'EOF'
{
  "name": "missing-script-fixture",
  "version": "1.0.0",
  "private": true,
  "description": "Fixture: package.json is missing the build script.",
  "scripts": {
    "test": "node test.js"
  }
}
EOF

cat > index.js <<'EOF'
const greet = (name) => `hello ${name}`;

if (require.main === module) {
  console.log(greet("nebula"));
}

module.exports = { greet };
EOF

cat > test.js <<'EOF'
const assert = require("assert");
const { greet } = require("./index.js");

assert.strictEqual(greet("nebula"), "hello nebula");
console.log("ok");
EOF
