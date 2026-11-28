import re

with open("internal/harness/orchestrator.go", "r") as f:
    content = f.read()

old_summary = """func (o *Orchestrator) buildRepoSummary(workDir string) string {
	// Execute 'find' to get a tree up to depth 3
	cmd := exec.Command("find", ".", "-maxdepth", "3", "-not", "-path", "*/.*", "-type", "d")
	cmd.Dir = workDir
	out, err := cmd.Output()
	
	dirs := string(out)
	if err != nil || dirs == "" {
		dirs = "<unable to list directories>"
	} else if len(dirs) > 1000 {
		dirs = dirs[:1000] + "\\n... (truncated)"
	}"""

new_summary = """func (o *Orchestrator) buildRepoSummary(workDir string) string {
	// Use pure Go filepath.WalkDir instead of heavy exec.Command("find") fork
	var dirsBuilder strings.Builder
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		// Skip hidden dirs
		if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
			return filepath.SkipDir
		}
		
		rel, err := filepath.Rel(workDir, path)
		if err == nil {
			depth := strings.Count(rel, string(os.PathSeparator))
			if depth <= 3 {
				if dirsBuilder.Len() > 0 {
					dirsBuilder.WriteString("\\n")
				}
				if rel == "." {
					dirsBuilder.WriteString(".")
				} else {
					dirsBuilder.WriteString("./" + rel)
				}
				if dirsBuilder.Len() > 1000 {
					return filepath.SkipAll // Early exit when truncated
				}
			} else {
				return filepath.SkipDir
			}
		}
		return nil
	})
	
	dirs := dirsBuilder.String()
	if dirs == "" {
		dirs = "<unable to list directories>"
	} else if len(dirs) >= 1000 {
		dirs += "\\n... (truncated)"
	}"""

# Check if strings/filepath are imported
if "path/filepath" not in content:
    content = content.replace('"time"', '"time"\n\t"path/filepath"\n\t"strings"')
elif "strings" not in content:
    content = content.replace('"time"', '"time"\n\t"strings"')

content = content.replace(old_summary, new_summary)

with open("internal/harness/orchestrator.go", "w") as f:
    f.write(content)
