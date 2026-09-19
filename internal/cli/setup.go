package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zalando/go-keyring"
)

const (
	keyringService = "nebula"
)

// setupResult holds everything collected by the setup wizard before it is
// persisted to disk / the OS keyring.
type setupResult struct {
	provider   string
	safety     string
	memory     bool
	apiKey     string
	baseURL    string
	diagModel  string
	healModel  string
	learnModel string
	embedModel string
}

// configFilePath resolves the nebula config path (respecting --config).
func configFilePath() string {
	if cfgFile != "" {
		return cfgFile
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "nebula", "config.toml")
}

// configFileExists reports whether a config file already exists.
func configFileExists() bool {
	p := configFilePath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// runSetupWizard walks the user through first-run configuration: provider,
// safety level and memory settings first, then provider-specific details.
func runSetupWizard() error {
	var (
		provider     string
		safety       string
		enableMemory = true
	)

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("LLM provider").
				Description("Which provider should Nebula use for diagnosis and healing?").
				Options(
					huh.NewOption("Groq", "groq"),
					huh.NewOption("Gemini", "gemini"),
					huh.NewOption("Ollama (local)", "ollama"),
					huh.NewOption("Skip for now", "skip"),
				).
				Value(&provider),
			huh.NewSelect[string]().
				Title("Safety level").
				Description("How aggressively Nebula should review commands before running them.").
				Options(
					huh.NewOption("Permissive", "permissive"),
					huh.NewOption("Balanced", "balanced"),
					huh.NewOption("Strict", "strict"),
				).
				Value(&safety),
			huh.NewConfirm().
				Title("Enable memory").
				Description("Remember command history and past fixes to learn from failures.").
				Value(&enableMemory),
		),
	).Run(); err != nil {
		return fmt.Errorf("setup: base form: %w", err)
	}

	r := setupResult{
		provider: provider,
		safety:   safety,
		memory:   enableMemory,
	}

	switch provider {
	case "groq":
		if err := groqForm(&r); err != nil {
			return err
		}
	case "gemini":
		if err := geminiForm(&r); err != nil {
			return err
		}
	case "ollama":
		if err := ollamaForm(&r); err != nil {
			return err
		}
	case "skip":
		// no provider-specific details needed
	default:
		return fmt.Errorf("setup: unknown provider %q", provider)
	}

	if err := r.persist(); err != nil {
		return err
	}

	r.printSummary()
	return nil
}

// groqForm collects a Groq API key plus workload-specialized models.
func groqForm(r *setupResult) error {
	var (
		apiKey string
		diag   = "llama-3.1-8b-instant"
		heal   = "llama-3.3-70b-versatile"
	)

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Groq API key").
				Description("Get one at https://console.groq.com/keys — stored in the OS keyring.").
				EchoMode(huh.EchoModePassword).
				Value(&apiKey),
			huh.NewSelect[string]().
				Title("Diagnose model").
				Description("Fast and cheap — used for failure analysis.").
				Options(
					huh.NewOption("llama-3.1-8b-instant", "llama-3.1-8b-instant"),
					huh.NewOption("llama-3.3-70b-versatile", "llama-3.3-70b-versatile"),
					huh.NewOption("mixtral-8x7b-32768", "mixtral-8x7b-32768"),
				).
				Value(&diag),
			huh.NewSelect[string]().
				Title("Heal model").
				Description("Stronger model — used for fix suggestions.").
				Options(
					huh.NewOption("llama-3.3-70b-versatile", "llama-3.3-70b-versatile"),
					huh.NewOption("llama-3.1-8b-instant", "llama-3.1-8b-instant"),
					huh.NewOption("llama-3.3-70b-specdec", "llama-3.3-70b-specdec"),
				).
				Value(&heal),
		),
	).Run(); err != nil {
		return fmt.Errorf("setup: groq form: %w", err)
	}

	r.apiKey = apiKey
	r.diagModel = diag
	r.healModel = heal
	r.learnModel = heal
	r.embedModel = ""
	return nil
}

// geminiForm collects a Gemini API key plus workload-specialized models.
func geminiForm(r *setupResult) error {
	var (
		apiKey string
		diag   = "gemini-2.0-flash"
		heal   = "gemini-2.0-flash-thinking-exp"
	)

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Gemini API key").
				Description("Get one at https://aistudio.google.com/apikey — stored in the OS keyring.").
				EchoMode(huh.EchoModePassword).
				Value(&apiKey),
			huh.NewSelect[string]().
				Title("Diagnose model").
				Description("Fast and cheap — used for failure analysis.").
				Options(
					huh.NewOption("gemini-2.0-flash", "gemini-2.0-flash"),
					huh.NewOption("gemini-2.5-flash", "gemini-2.5-flash"),
					huh.NewOption("gemini-2.5-pro", "gemini-2.5-pro"),
				).
				Value(&diag),
			huh.NewSelect[string]().
				Title("Heal model").
				Description("Stronger model — used for fix suggestions.").
				Options(
					huh.NewOption("gemini-2.0-flash-thinking-exp", "gemini-2.0-flash-thinking-exp"),
					huh.NewOption("gemini-2.5-flash", "gemini-2.5-flash"),
					huh.NewOption("gemini-2.5-pro", "gemini-2.5-pro"),
				).
				Value(&heal),
		),
	).Run(); err != nil {
		return fmt.Errorf("setup: gemini form: %w", err)
	}

	r.apiKey = apiKey
	r.diagModel = diag
	r.healModel = heal
	r.learnModel = heal
	r.embedModel = "text-embedding-004"
	return nil
}

// ollamaForm collects a local Ollama base URL plus workload-specialized models.
func ollamaForm(r *setupResult) error {
	var (
		baseURL = "http://localhost:11434"
		diag    = "llama3.2:3b"
		heal    = "llama3.2"
	)

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Ollama base URL").
				Description("Address of your local (or remote) Ollama server.").
				Value(&baseURL),
			huh.NewInput().
				Title("Diagnose model").
				Description("Fast and cheap — used for failure analysis.").
				Value(&diag),
			huh.NewInput().
				Title("Heal model").
				Description("Stronger model — used for fix suggestions.").
				Value(&heal),
		),
	).Run(); err != nil {
		return fmt.Errorf("setup: ollama form: %w", err)
	}

	r.baseURL = baseURL
	r.diagModel = diag
	r.healModel = heal
	r.learnModel = "llama3.1:70b"
	r.embedModel = "nomic-embed-text"
	return nil
}

// persist writes the collected config to disk and stores any API key in the
// OS keyring.
func (r setupResult) persist() error {
	// 1. Store the API key in the OS keyring (never in the config file).
	var keyringKey string
	switch r.provider {
	case "groq":
		keyringKey = "groq_api_key"
	case "gemini":
		keyringKey = "gemini_api_key"
	}
	if r.apiKey != "" && keyringKey != "" {
		if err := keyring.Set(keyringService, keyringKey, r.apiKey); err != nil {
			return fmt.Errorf("store %s key in OS keyring: %w", r.provider, err)
		}
	}

	// 2. Write the TOML config to ~/.config/nebula/config.toml.
	path := configFilePath()
	if path == "" {
		return fmt.Errorf("resolve config path: home directory unavailable")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(r.configTOML()), 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

// safetyPolicies maps a wizard safety level to the per-risk [safety] policies.
func safetyPolicies(level string) (safe, low, medium, high, dangerous string) {
	switch level {
	case "permissive":
		return "allow", "allow", "allow", "ask", "deny"
	case "strict":
		return "allow", "ask", "ask", "deny", "deny"
	default: // balanced
		return "allow", "allow", "ask", "ask", "deny"
	}
}

// configTOML builds the config file content (no external TOML library).
func (r setupResult) configTOML() string {
	policySafe, policyLow, policyMedium, policyHigh, policyDangerous := safetyPolicies(r.safety)

	var b strings.Builder
	b.WriteString("# Nebula configuration file — generated by `nebula setup`\n")

	primary := r.provider
	if primary == "" {
		primary = "none"
	}
	fmt.Fprintf(&b, "\n[llm]\nprimary = %q\nfallback = \"\"\n", primary)

	switch r.provider {
	case "groq":
		b.WriteString("\n[llm.groq]\n# API key is stored in the OS keyring (service: \"nebula\", key: \"groq_api_key\").\n")
		fmt.Fprintf(&b, "api_key = \"\"\n")
		fmt.Fprintf(&b, "model_diagnose = %q\n", r.diagModel)
		fmt.Fprintf(&b, "model_heal = %q\n", r.healModel)
		fmt.Fprintf(&b, "model_learn = %q\n", r.learnModel)
		b.WriteString("model_embed = \"\"  # Groq has no embed endpoint\n")
	case "gemini":
		b.WriteString("\n[llm.gemini]\n# API key is stored in the OS keyring (service: \"nebula\", key: \"gemini_api_key\").\n")
		fmt.Fprintf(&b, "api_key = \"\"\n")
		fmt.Fprintf(&b, "model_diagnose = %q\n", r.diagModel)
		fmt.Fprintf(&b, "model_heal = %q\n", r.healModel)
		fmt.Fprintf(&b, "model_learn = %q\n", r.learnModel)
		fmt.Fprintf(&b, "model_embed = %q\n", r.embedModel)
	case "ollama":
		b.WriteString("\n[llm.ollama]\n")
		fmt.Fprintf(&b, "base_url = %q\n", r.baseURL)
		fmt.Fprintf(&b, "model_diagnose = %q\n", r.diagModel)
		fmt.Fprintf(&b, "model_heal = %q\n", r.healModel)
		fmt.Fprintf(&b, "model_learn = %q\n", r.learnModel)
		fmt.Fprintf(&b, "model_embed = %q\n", r.embedModel)
	}

	fmt.Fprintf(&b, "\n[memory]\nenabled = %t\n", r.memory)
	b.WriteString("db_path = \"~/.local/share/nebula/nebula.db\"\n")
	b.WriteString("ring_buffer_size = 32768  # 32KB\n")

	fmt.Fprintf(&b, "\n[safety]\nlevel = %q\n", r.safety)
	fmt.Fprintf(&b, "policy_safe = %q\n", policySafe)
	fmt.Fprintf(&b, "policy_low = %q\n", policyLow)
	fmt.Fprintf(&b, "policy_medium = %q\n", policyMedium)
	fmt.Fprintf(&b, "policy_high = %q\n", policyHigh)
	fmt.Fprintf(&b, "policy_dangerous = %q\n", policyDangerous)

	b.WriteString("\n[tui]\ntheme = \"auto\"\n")
	return b.String()
}

// printSummary renders the post-setup success message.
func (r setupResult) printSummary() {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10")).
		Render("✓ Nebula configured successfully!")

	key := func(k string) string {
		return fmt.Sprintf("  %-16s", lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(k+":"))
	}

	fmt.Println()
	fmt.Println(title)
	fmt.Println()
	provider := r.provider
	if provider == "" {
		provider = "none (run `nebula setup` to configure)"
	}
	fmt.Printf("%s %s\n", key("Provider"), provider)
	if r.diagModel != "" {
		fmt.Printf("%s %s\n", key("Diagnose"), r.diagModel)
	}
	if r.healModel != "" {
		fmt.Printf("%s %s\n", key("Heal"), r.healModel)
	}
	fmt.Printf("%s %s\n", key("Safety"), r.safety)
	fmt.Printf("%s %v\n", key("Memory"), r.memory)
	fmt.Printf("%s %s\n", key("Config"), configFilePath())
	switch r.provider {
	case "groq", "gemini":
		if r.apiKey != "" {
			fmt.Printf("%s stored in OS keyring (%s/%s_api_key)\n", key("API key"), keyringService, r.provider)
		} else {
			fmt.Printf("%s not set — set the key later via NEBULA_LLM_%s_API_KEY\n", key("API key"), strings.ToUpper(r.provider))
		}
	}
	fmt.Println()
}
