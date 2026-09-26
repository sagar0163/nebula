#!/usr/bin/env bash
# Creates a package.json with a broken build script (wrong command) so
# `node -e "require('./package.json')"` passes but the build script fails.
# The fix is to update the build script to a working command.
set -euo pipefail

command -v node >/dev/null 2>&1 || { echo "node not available" >&2; exit 127; }

cat > package.json <<'EOF'
{
  "name": "missing-script-fixture",
  "version": "1.0.0",
  "private": true,
  "description": "Fixture: package.json build script calls a nonexistent command.",
  "scripts": {
    "test": "node test.js",
    "build": "INVALID_BUILD_COMMAND_DOES_NOT_EXIST"
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
