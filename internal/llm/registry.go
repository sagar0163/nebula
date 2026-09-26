package llm

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var defaultLimits = map[string]int{
	// Gemini series
	"gemini-1.5-pro":       2000000,
	"gemini-1.5-flash":     1000000,
	"gemini-1.0-pro":       32768,
	// Claude series
	"claude-3-5-sonnet-20240620": 200000,
	"claude-3-opus-20240229":     200000,
	"claude-3-haiku-20240307":    200000,
	// OpenAI series
	"gpt-4o":               128000,
	"gpt-4o-mini":          128000,
	"gpt-4-turbo":          128000,
	"gpt-3.5-turbo":        16384,
	// Groq / LLaMA / Mistral
	"llama-3.1-70b-versatile":    8192,
	"llama-3.1-8b-instant":       8192,
	"llama3-70b-8192":            8192,
	"llama3-8b-8192":             8192,
	"mixtral-8x7b-32768":         32768,
	"mistral-large-latest":       32768,
	"open-mistral-nemo":          32768,
}

var userLimits = make(map[string]int)
var registryLoaded bool

// LoadModelRegistry reads custom model limits from ~/.config/nebula/models.yaml
func LoadModelRegistry() {
	if registryLoaded {
		return
	}
	registryLoaded = true

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".config", "nebula", "models.yaml")
	
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var parsed map[string]int
	if err := yaml.Unmarshal(data, &parsed); err == nil {
		for k, v := range parsed {
			userLimits[k] = v
		}
	}
}

// GetContextWindow returns the specific input context limit for a given model.
// Returns 0 if entirely unknown.
func GetContextWindow(modelName string) int {
	LoadModelRegistry()
	
	// Exact match in user config
	if limit, ok := userLimits[modelName]; ok {
		return limit
	}
	// Exact match in defaults
	if limit, ok := defaultLimits[modelName]; ok {
		return limit
	}

	// Fallback fuzzy matching (e.g. if the user says "gemini-1.5-pro-latest" instead of "gemini-1.5-pro")
	modelLower := strings.ToLower(modelName)
	if strings.Contains(modelLower, "gemini-1.5") {
		return 1000000
	}
	if strings.Contains(modelLower, "claude") {
		return 200000
	}
	if strings.Contains(modelLower, "gpt-4") {
		return 128000
	}
	if strings.Contains(modelLower, "llama-3.1") {
		return 8192 // Wait, standard llama 3.1 is 128k, but on groq it's often artificially limited to 8k. 
	}
	
	return 0
}
