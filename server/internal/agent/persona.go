package agent

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Persona struct {
	Name           string            `yaml:"name"`
	Identity       string            `yaml:"identity"`
	CoreRules      []string          `yaml:"core_rules"`
	Style          map[string]string `yaml:"style"`
	OutputContract map[string]any    `yaml:"output_contract"`
	Safety         map[string]any    `yaml:"safety"`
}

func LoadPersonaFile(path string) (Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Persona{}, fmt.Errorf("read persona %q: %w", path, err)
	}
	var persona Persona
	if err := yaml.Unmarshal(data, &persona); err != nil {
		return Persona{}, fmt.Errorf("parse persona %q: %w", path, err)
	}
	if persona.Name == "" || persona.Identity == "" {
		return Persona{}, fmt.Errorf("persona %q must include name and identity", path)
	}
	return persona, nil
}
