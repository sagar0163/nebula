package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill holds the parsed contents of a skill markdown file.
type Skill struct {
	Name         string
	Description  string
	Instructions string
}

func skillsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "nebula", "skills")
}

// Load reads a skill by name from ~/.config/nebula/skills/<name>.md.
func Load(name string) (*Skill, error) {
	path := filepath.Join(skillsDir(), name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skill %q not found: %w", name, err)
	}
	return parse(name, string(data)), nil
}

// List returns all skills available in ~/.config/nebula/skills/.
func List() ([]*Skill, error) {
	entries, err := os.ReadDir(skillsDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	var skills []*Skill
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		s, err := Load(name)
		if err != nil {
			continue
		}
		skills = append(skills, s)
	}
	return skills, nil
}

// parse splits frontmatter from body and extracts description.
func parse(name, content string) *Skill {
	s := &Skill{Name: name}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) == 3 {
		// parts[1] = frontmatter, parts[2] = body
		for _, line := range strings.Split(parts[1], "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "description:") {
				s.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			}
		}
		s.Instructions = strings.TrimSpace(parts[2])
	} else {
		s.Instructions = strings.TrimSpace(content)
	}
	return s
}
