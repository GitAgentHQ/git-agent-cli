package config

import (
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// WriteUserField writes a key-value pair to the user config file,
// preserving all existing keys.
func WriteUserField(configPath, key, value string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	rawMap := readYAMLMap(configPath)
	def := KeyRegistry[key]
	switch def.Type {
	case "bool":
		b, _ := strconv.ParseBool(value)
		rawMap[key] = b
	default:
		rawMap[key] = value
	}
	data, err := yaml.Marshal(rawMap)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// ReadUserField reads a single key from the user config file.
// Returns ("", false, nil) when the key is not present.
func ReadUserField(configPath, key string) (string, bool, error) {
	return ReadProjectField(configPath, key)
}
