package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WriteUserField writes a key-value pair to the user config file,
// preserving all existing keys.
func WriteUserField(configPath, key, value string) error {
	return writeField(configPath, key, value, 0600, true)
}

// writeField writes a key-value pair to a config file, preserving all existing
// keys. When enforceMode is true it chmods the file to mode after writing,
// because WriteFile keeps an existing file's mode and older versions could
// leave a config readable by other local users.
func writeField(path, key, value string, mode os.FileMode, enforceMode bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	rawMap := ReadYAMLMap(path)
	rawMap[key] = coerceForWrite(key, value)
	data, err := yaml.Marshal(rawMap)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	if enforceMode {
		return os.Chmod(path, mode)
	}
	return nil
}

// ReadUserField reads a single key from the user config file.
// Returns ("", false, nil) when the key is not present.
func ReadUserField(configPath, key string) (string, bool, error) {
	return ReadProjectField(configPath, key)
}
