#!/usr/bin/env bash
BIN=~/.local/bin/nebula
EXIT=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1"; EXIT=1; }
$BIN key list >/dev/null 2>&1 && pass "key list" || fail "key list"
$BIN workflow list >/dev/null 2>&1 && pass "workflow list" || fail "workflow list"
$BIN skill list >/dev/null 2>&1 && pass "skill list" || fail "skill list"
$BIN workflow run /nonexistent_xyz.yaml >/dev/null 2>&1 && fail "workflow run missing file" || pass "workflow run missing file"
$BIN skill show doesnotexist_xyz >/dev/null 2>&1 && fail "skill show nonexistent" || pass "skill show nonexistent"
$BIN key remove groq 99 >/dev/null 2>&1 && fail "key remove bad slot" || pass "key remove bad slot"
exit $EXIT