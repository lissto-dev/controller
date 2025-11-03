package config

import (
	"fmt"
	"os"

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
	URL string `yaml:"url"`
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
	URL      string     `yaml:"url"`
	Name     string     `yaml:"name,omitempty"`
	Branches []string   `yaml:"branches"`
	Tags     TagsConfig `yaml:"tags"`
}

// TagsConfig holds tag-related configuration
type TagsConfig struct {
	Prefix string `yaml:"prefix"`
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
		if repo.Tags.Prefix == "" {
			return fmt.Errorf("repo %s: tags.prefix is required", repoName)
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

// IsGlobalBranch checks if a branch is configured as global for any repository
func (c *Config) IsGlobalBranch(branch string) bool {
	for _, repo := range c.Repos {
		for _, repoBranch := range repo.Branches {
			if repoBranch == branch {
				return true
			}
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
				Tags: TagsConfig{
					Prefix: "v",
				},
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
