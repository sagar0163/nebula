package profile

type UserProfile struct {
	LanguagePreferences string
	Tools               []string
}

type ProjectProfile struct {
	RepoURL     string
	Description string
	Stack       []string
}

func GetUserProfile() UserProfile {
	return UserProfile{
		LanguagePreferences: "Go developer, prefers minimal abstractions",
		Tools:               []string{"git", "make"},
	}
}

func GetProjectProfile() ProjectProfile {
	return ProjectProfile{
		RepoURL:     "github.com/sagar0163/nebula",
		Description: "Nebula CLI, AI agent",
		Stack:       []string{"Go 1.22"},
	}
}
