package profile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type UserProfile struct {
	Name                string
	Email               string
	LanguagePreferences string
}

type ProjectProfile struct {
	RepoURL     string
	ReadmeIntro string
}

func GetUserProfile() UserProfile {
	var profile UserProfile
	
	// Read from git config
	cmdName := exec.Command("git", "config", "--global", "user.name")
	if out, err := cmdName.Output(); err == nil {
		profile.Name = strings.TrimSpace(string(out))
	}
	
	cmdEmail := exec.Command("git", "config", "--global", "user.email")
	if out, err := cmdEmail.Output(); err == nil {
		profile.Email = strings.TrimSpace(string(out))
	}
	
	// Stub preferences (could be fetched from SQLite in the future)
	profile.LanguagePreferences = "Prefers Go, minimalist abstractions."
	
	return profile
}

func GetProjectProfile(dir string) ProjectProfile {
	var profile ProjectProfile
	
	cmd := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url")
	if out, err := cmd.Output(); err == nil {
		profile.RepoURL = strings.TrimSpace(string(out))
	}
	
	readmePath := filepath.Join(dir, "README.md")
	if content, err := os.ReadFile(readmePath); err == nil {
		lines := strings.Split(string(content), "\n")
		var intro string
		for _, l := range lines {
			if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "[") {
				intro = l
				break
			}
		}
		if len(intro) > 200 {
			intro = intro[:200] + "..."
		}
		profile.ReadmeIntro = intro
	}
	
	return profile
}
