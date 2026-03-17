package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wangxiuwen/tssh/internal/model"
)

// TsshConfig is the tssh-specific config file structure (~/.tssh/config.json)
type TsshConfig struct {
	Default  string         `json:"default"`
	Profiles []model.Config `json:"profiles"`
}

// Load reads credentials for a specific profile.
// Priority: env vars → ~/.tssh/config.json → ~/.aliyun/config.json
func Load(profile string) (*model.Config, error) {
	akID := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	akSecret := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	region := os.Getenv("ALIBABA_CLOUD_REGION_ID")
	if region == "" {
		region = "cn-beijing"
	}

	if akID != "" && akSecret != "" && profile == "" {
		return &model.Config{
			AccessKeyID:     akID,
			AccessKeySecret: akSecret,
			Region:          region,
			ProfileName:     "env",
		}, nil
	}

	home, _ := os.UserHomeDir()
	tsshConfigPath := filepath.Join(home, ".tssh", "config.json")
	if data, err := os.ReadFile(tsshConfigPath); err == nil {
		var cfg TsshConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			targetProfile := profile
			if targetProfile == "" {
				targetProfile = cfg.Default
			}
			if targetProfile == "" {
				targetProfile = "default"
			}
			for _, p := range cfg.Profiles {
				if p.ProfileName == targetProfile {
					if p.Region == "" {
						p.Region = "cn-beijing"
					}
					return &p, nil
				}
			}
			if profile != "" {
				return nil, fmt.Errorf("profile '%s' not found in %s", profile, tsshConfigPath)
			}
		}
	}

	configPath := filepath.Join(home, ".aliyun", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("no credentials found: set env vars or create ~/.tssh/config.json")
	}

	var cfg struct {
		Profiles []struct {
			Name            string `json:"name"`
			AccessKeyID     string `json:"access_key_id"`
			AccessKeySecret string `json:"access_key_secret"`
			RegionID        string `json:"region_id"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	targetProfile := profile
	if targetProfile == "" {
		targetProfile = "default"
	}
	for _, p := range cfg.Profiles {
		if p.Name == targetProfile {
			r := p.RegionID
			if r == "" {
				r = "cn-beijing"
			}
			return &model.Config{
				AccessKeyID:     p.AccessKeyID,
				AccessKeySecret: p.AccessKeySecret,
				Region:          r,
				ProfileName:     p.Name,
			}, nil
		}
	}
	return nil, fmt.Errorf("profile '%s' not found in config", targetProfile)
}

// ListProfiles returns all available profile names
func ListProfiles() []string {
	var profiles []string

	if os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID") != "" {
		profiles = append(profiles, "env")
	}

	home, _ := os.UserHomeDir()
	tsshConfigPath := filepath.Join(home, ".tssh", "config.json")
	if data, err := os.ReadFile(tsshConfigPath); err == nil {
		var cfg TsshConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			for _, p := range cfg.Profiles {
				profiles = append(profiles, p.ProfileName)
			}
		}
	}

	configPath := filepath.Join(home, ".aliyun", "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg struct {
			Profiles []struct{ Name string `json:"name"` } `json:"profiles"`
		}
		if err := json.Unmarshal(data, &cfg); err == nil {
			for _, p := range cfg.Profiles {
				profiles = append(profiles, "aliyun:"+p.Name)
			}
		}
	}
	return profiles
}
