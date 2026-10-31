// Package localize extracts fault-localization hints (traceback frames and
// code symbols) from SWE-bench issue text.
package localize

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TraceLocation is one frame of a Python traceback.
type TraceLocation struct {
	File     string
	Line     int
	Function string
}

func (t TraceLocation) String() string {
	return fmt.Sprintf("%s:%d in %s", t.File, t.Line, t.Function)
}

var (
	tracebackRe = regexp.MustCompile(`File "([^"]+)", line (\d+)(?:, in ([\w<>.]+))?`)
	backtickRe  = regexp.MustCompile("`([A-Za-z_][\\w.]*)(?:\\(\\))?`")
	camelRe     = regexp.MustCompile(`\b([A-Z][a-z0-9]+(?:[A-Z][a-z0-9]*)+)\b`)
	snakeRe     = regexp.MustCompile(`\b([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\b`)
	dottedRe    = regexp.MustCompile(`\b([a-z_][\w]*(?:\.[A-Za-z_]\w*){1,})\b`)
)

// ParseTraceback extracts (file, line, function) tuples from Python tracebacks
// in text, e.g. `File "path/to/file.py", line 42, in function_name`.
// Duplicate frames are removed; order of first appearance is kept.
func ParseTraceback(text string) []TraceLocation {
	var locs []TraceLocation
	seen := map[string]bool{}
	for _, m := range tracebackRe.FindAllStringSubmatch(text, -1) {
		line, _ := strconv.Atoi(m[2])
		loc := TraceLocation{File: m[1], Line: line, Function: m[3]}
		if k := loc.String(); !seen[k] {
			seen[k] = true
			locs = append(locs, loc)
		}
	}
	return locs
}

// ExtractSymbols extracts likely class/function names from issue text using
// simple heuristics: backtick-quoted identifiers, CamelCase, snake_case and
// dotted module paths. Results are deduplicated, backtick hits first.
func ExtractSymbols(text string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.Trim(s, ".")
		if len(s) < 3 || seen[s] || stopwords[strings.ToLower(s)] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, re := range []*regexp.Regexp{backtickRe, camelRe, dottedRe, snakeRe} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			add(m[1])
		}
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

var stopwords = map[string]bool{"e.g": true, "i.e": true, "etc": true}

// Hints formats traceback locations and symbols as a planning-context block.
// Returns "" if nothing was found. Frames from site-packages/stdlib are listed
// last since the fix is rarely there.
func Hints(text string) string {
	locs := ParseTraceback(text)
	syms := ExtractSymbols(text)
	if len(locs) == 0 && len(syms) == 0 {
		return ""
	}
	var b strings.Builder
	if len(locs) > 0 {
		b.WriteString("Possible locations from traceback (innermost last):\n")
		for _, l := range locs {
			b.WriteString("  - " + l.String() + "\n")
		}
	}
	if len(syms) > 0 {
		b.WriteString("Symbols mentioned in the issue: " + strings.Join(syms, ", ") + "\n")
	}
	return b.String()
}
