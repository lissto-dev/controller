package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the operator configuration
type Config struct {
	API        APIConfig             `yaml:"api"`
	Logging    LoggingConfig         `yaml:"logging"`
	Namespaces NamespacesConfig      `yaml:"namespaces"`
	Repos      map[string]RepoConfig `yaml:"repos"`
	Stacks     StacksConfig          `yaml:"stacks"`
}

// APIConfig holds API configuration
type APIConfig struct {
	Server ServerConfig `yaml:"server"`
}

// ServerConfig holds server connection settings
type ServerConfig struct {
	URL       string `yaml:"url"`
	PublicURL string `yaml:"publicUrl,omitempty"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level string `yaml:"level"`
}

// NamespacesConfig holds namespace scoping configuration
type NamespacesConfig struct {
	Global           string `yaml:"global"`
	DeveloperPrefix  string `yaml:"developerPrefix"`
	AutoCreatePrefix bool   `yaml:"autoCreatePrefix"`
}

// RepoConfig holds repository configuration
type RepoConfig struct {
	URL      string   `yaml:"url"`
	Name     string   `yaml:"name,omitempty"`
	Branches []string `yaml:"branches"`
}

// StacksConfig holds stack configuration for ingress objects
type StacksConfig struct {
	Public PublicConfig `yaml:"public"`
	Global GlobalConfig `yaml:"global,omitempty"`
}

// PublicConfig holds public configuration for ingress objects
type PublicConfig struct {
	IngressClass string `yaml:"ingressClass"`
	HostSuffix   string `yaml:"hostSuffix"`
}

// GlobalConfig holds global configuration
type GlobalConfig struct {
	Images ImagesConfig `yaml:"images,omitempty"`
}

// ImagesConfig holds image resolution configuration
type ImagesConfig struct {
	Registry         string `yaml:"registry,omitempty"`
	RepositoryPrefix string `yaml:"repositoryPrefix,omitempty"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	// Validate API Server
	if c.API.Server.URL == "" {
		return fmt.Errorf("api.server.url is required")
	}

	// Validate Logging
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.Logging.Level] {
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error")
	}

	// Validate Namespaces
	if c.Namespaces.Global == "" {
		return fmt.Errorf("namespaces.global is required")
	}
	if c.Namespaces.DeveloperPrefix == "" {
		return fmt.Errorf("namespaces.developerPrefix is required")
	}

	// Validate Repos
	if len(c.Repos) == 0 {
		return fmt.Errorf("at least one repository must be configured")
	}
	for repoName, repo := range c.Repos {
		if repo.URL == "" {
			return fmt.Errorf("repo %s: url is required", repoName)
		}
		if len(repo.Branches) == 0 {
			return fmt.Errorf("repo %s: at least one branch must be configured", repoName)
		}
	}

	// Validate Stacks
	if c.Stacks.Public.IngressClass == "" {
		return fmt.Errorf("stacks.public.ingressClass is required")
	}
	if c.Stacks.Public.HostSuffix == "" {
		return fmt.Errorf("stacks.public.hostSuffix is required")
	}

	return nil
}

// NormalizeRepositoryURL strips transport prefixes and normalizes repository URLs
// Examples:
//   - "https://github.com/lissto/lissto.git" -> "github.com/lissto/lissto"
//   - "git@github.com:lissto/lissto.git" -> "github.com/lissto/lissto"
//   - "github.com/lissto/lissto" -> "github.com/lissto/lissto"
func NormalizeRepositoryURL(repoURL string) string {
	normalized := strings.TrimSpace(repoURL)

	// Remove common transport prefixes
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.TrimPrefix(normalized, "git://")
	normalized = strings.TrimPrefix(normalized, "ssh://")

	// Handle git@github.com:org/repo format
	if strings.HasPrefix(normalized, "git@") {
		normalized = strings.TrimPrefix(normalized, "git@")
		// Replace the colon with a slash: github.com:org/repo -> github.com/org/repo
		normalized = strings.Replace(normalized, ":", "/", 1)
	}

	// Remove .git suffix
	normalized = strings.TrimSuffix(normalized, ".git")

	// Remove trailing slash
	normalized = strings.TrimSuffix(normalized, "/")

	return normalized
}

// ValidateRepository checks if a repository URL is configured
// Returns the repo config key and true if found, empty string and false otherwise
func (c *Config) ValidateRepository(repoURL string) (string, bool) {
	if repoURL == "" {
		return "", false
	}

	normalizedInput := NormalizeRepositoryURL(repoURL)

	for repoKey, repo := range c.Repos {
		normalizedConfigURL := NormalizeRepositoryURL(repo.URL)
		if normalizedInput == normalizedConfigURL {
			return repoKey, true
		}
	}

	return "", false
}

// IsGlobalBranch checks if a branch is configured as global for a specific repository
// Both repository and branch are required
func (c *Config) IsGlobalBranch(repository, branch string) bool {
	if repository == "" || branch == "" {
		return false
	}

	repoKey, found := c.ValidateRepository(repository)
	if !found {
		return false
	}

	repo := c.Repos[repoKey]
	for _, repoBranch := range repo.Branches {
		if repoBranch == branch {
			return true
		}
	}
	return false
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		API: APIConfig{
			Server: ServerConfig{
				URL: "http://lissto-api:8080",
			},
		},
		Logging: LoggingConfig{
			Level: "info",
		},
		Namespaces: NamespacesConfig{
			Global:           "lissto-global",
			DeveloperPrefix:  "dev-",
			AutoCreatePrefix: true,
		},
		Repos: map[string]RepoConfig{
			"main": {
				URL:      "https://github.com/example/repo",
				Name:     "Main Repository",
				Branches: []string{"main", "master"},
			},
		},
		Stacks: StacksConfig{
			Public: PublicConfig{
				IngressClass: "nginx",
				HostSuffix:   ".example.com",
			},
		},
	}
}
