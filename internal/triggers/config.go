package triggers

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type TriggerConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`     // "cron" or "file-watch"
	Schedule string `yaml:"schedule"` // for cron
	Path     string `yaml:"path"`     // for file-watch
	Goal     string `yaml:"goal"`
}

func LoadConfigs(dir string) ([]TriggerConfig, error) {
	var configs []TriggerConfig
	
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No triggers configured yet
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cfg TriggerConfig
		if err := yaml.Unmarshal(data, &cfg); err == nil {
			configs = append(configs, cfg)
		}
	}
	return configs, nil
}
