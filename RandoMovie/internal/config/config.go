package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Config holds persistent user preferences.
type Config struct {
	GamePath  string   `json:"game_path"`
	VideoPool []string `json:"video_pool"`
}

// configDir returns the OS-appropriate directory for storing the config file.
func configDir() string {
	switch runtime.GOOS {
	case "linux":
		if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
			return filepath.Join(dir, "RandoMovie")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "RandoMovie")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "RandoMovie")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "RandoMovie")
	}
	return "."
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

// Load reads the config from disk. Returns an empty default config if the
// file does not exist yet or cannot be parsed.
func Load() *Config {
	c := &Config{}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return c // no config file yet — use defaults
	}
	if err := json.Unmarshal(data, c); err != nil {
		return &Config{} // corrupt config — reset to defaults rather than crashing
	}
	return c
}

// Save writes the config to disk, creating the config directory if needed.
func (c *Config) Save() error {
	os.MkdirAll(configDir(), 0755)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0644)
}

// AddVideo appends path to the pool, ignoring duplicates.
func (c *Config) AddVideo(path string) {
	for _, v := range c.VideoPool {
		if v == path {
			return
		}
	}
	c.VideoPool = append(c.VideoPool, path)
}

// RemoveVideo removes the first occurrence of path from the pool.
func (c *Config) RemoveVideo(path string) {
	for i, v := range c.VideoPool {
		if v == path {
			c.VideoPool = append(c.VideoPool[:i], c.VideoPool[i+1:]...)
			return
		}
	}
}
