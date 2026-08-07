package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WriteUserField writes a key-value pair to the user config file,
// preserving all existing keys.
func WriteUserField(configPath, key, value string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	rawMap := ReadYAMLMap(configPath)
	rawMap[key] = coerceForWrite(key, value)
	data, err := yaml.Marshal(rawMap)
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return err
	}
	// WriteFile preserves an existing file's mode, so repair configurations
	// created by older versions that could be read by other local users.
	return os.Chmod(configPath, 0600)
}

// ReadUserField reads a single key from the user config file.
// Returns ("", false, nil) when the key is not present.
func ReadUserField(configPath, key string) (string, bool, error) {
	return ReadProjectField(configPath, key)
}
