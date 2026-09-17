package gateway

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig  `yaml:"server"`
	ProvidersPath string        `yaml:"providers_path"`
	Logging       LoggingConfig `yaml:"logging"`
}

type ServerConfig struct {
	Host           string        `yaml:"host"`
	Port           int           `yaml:"port"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	IdleTimeout    time.Duration `yaml:"idle_timeout"`
	MaxRequestSize int           `yaml:"max_request_size"`
	ErrorResponse  string        `yaml:"error_response"`
	AllowedIPs     []string      `yaml:"allowed_ips"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

func LoadConfig(configDir string) (Config, error) {
	if strings.TrimSpace(configDir) == "" {
		return Config{}, fmt.Errorf("gateway: config directory is required")
	}
	data, err := os.ReadFile(filepath.Join(configDir, "gateway.yml"))
	if err != nil {
		return Config{}, fmt.Errorf("gateway: read gateway.yml: %w", err)
	}
	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("gateway: parse gateway.yml: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Server.Host == "" {
		return fmt.Errorf("gateway: server.host is required")
	}
	if c.Server.Port < 0 || c.Server.Port > 65535 {
		return fmt.Errorf("gateway: server.port must be between 0 and 65535")
	}
	if c.Server.ReadTimeout <= 0 || c.Server.WriteTimeout <= 0 || c.Server.IdleTimeout <= 0 {
		return fmt.Errorf("gateway: server timeouts must be greater than zero")
	}
	if c.Server.MaxRequestSize <= 0 {
		return fmt.Errorf("gateway: server.max_request_size must be greater than zero")
	}
	if strings.TrimSpace(c.Server.ErrorResponse) == "" {
		return fmt.Errorf("gateway: server.error_response is required")
	}
	for _, entry := range c.Server.AllowedIPs {
		entry = strings.TrimSpace(entry)
		if net.ParseIP(entry) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(entry); err != nil {
			return fmt.Errorf("gateway: invalid server.allowed_ips entry %q", entry)
		}
	}
	if strings.TrimSpace(c.ProvidersPath) == "" {
		return fmt.Errorf("gateway: providers_path is required")
	}
	if filepath.IsAbs(c.ProvidersPath) {
		return fmt.Errorf("gateway: providers_path must be relative to config directory")
	}
	return nil
}

func (c Config) ProvidersDir(configDir string) string {
	return filepath.Join(configDir, c.ProvidersPath)
}
