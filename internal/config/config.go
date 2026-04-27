package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wangxiuwen/tssh/internal/model"
)

// TsshConfig is the tssh-specific config file structure (~/.tssh/config.json)
type TsshConfig struct {
	Default  string               `json:"default"`
	Profiles []model.Config       `json:"profiles"`
	Grafana  *model.GrafanaConfig `json:"grafana,omitempty"`
}

// Load reads credentials for a specific profile.
// Priority: env vars → ~/.tssh/config.json → ~/.aliyun/config.json
//
// 当 profile == "" 时, 默认 profile 的选择顺序为:
//  1. ALIBABA_CLOUD_PROFILE 环境变量
//  2. ~/.tssh/config.json 顶层 "default"
//  3. ~/.aliyun/config.json 顶层 "current" (跟随 aliyun-cli `aliyun configure switch`)
//  4. 字面量 "default"
func Load(profile string) (*model.Config, error) {
	akID := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	akSecret := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	stsToken := os.Getenv("ALIBABA_CLOUD_SECURITY_TOKEN")
	region := os.Getenv("ALIBABA_CLOUD_REGION_ID")
	if region == "" {
		region = "cn-beijing"
	}

	// Env-var profile is the default when user didn't specify one. Also honor
	// `--profile env` explicitly so `ListProfiles` advertising "env" stays
	// consistent with what `--profile` accepts.
	if akID != "" && akSecret != "" && (profile == "" || profile == "env") {
		return &model.Config{
			AccessKeyID:     akID,
			AccessKeySecret: akSecret,
			SecurityToken:   stsToken,
			Region:          region,
			ProfileName:     "env",
		}, nil
	}

	// 用户没显式指定时, 让 ALIBABA_CLOUD_PROFILE 充当 sticky default.
	implicitProfile := profile
	if implicitProfile == "" {
		implicitProfile = os.Getenv("ALIBABA_CLOUD_PROFILE")
	}

	home, _ := os.UserHomeDir()
	tsshConfigPath := filepath.Join(home, ".tssh", "config.json")
	if data, err := os.ReadFile(tsshConfigPath); err == nil {
		var cfg TsshConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			targetProfile := implicitProfile
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
					if err := validateCreds(&p, tsshConfigPath); err != nil {
						return nil, err
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
		Current  string `json:"current"`
		Profiles []struct {
			Name            string `json:"name"`
			Mode            string `json:"mode"`
			AccessKeyID     string `json:"access_key_id"`
			AccessKeySecret string `json:"access_key_secret"`
			StsToken        string `json:"sts_token"`
			StsExpiration   int64  `json:"sts_expiration"`
			RegionID        string `json:"region_id"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	targetProfile := implicitProfile
	if targetProfile == "" {
		// 跟随 aliyun-cli `aliyun configure switch --profile X` 的选择.
		targetProfile = cfg.Current
	}
	if targetProfile == "" {
		targetProfile = "default"
	}
	for _, p := range cfg.Profiles {
		if p.Name == targetProfile {
			r := p.RegionID
			if r == "" {
				r = "cn-beijing"
			}
			result := &model.Config{
				AccessKeyID:     p.AccessKeyID,
				AccessKeySecret: p.AccessKeySecret,
				SecurityToken:   p.StsToken,
				Region:          r,
				ProfileName:     p.Name,
			}
			if err := validateCreds(result, configPath); err != nil {
				// SSO-style profiles (CloudSSO / RamRoleArn / ...) need a refresh.
				if p.Mode != "" && p.Mode != "AK" {
					return nil, fmt.Errorf("profile '%s' (mode=%s) 凭据为空, 请先运行 `aliyun sso login --profile %s` 或 `aliyun configure --profile %s` 刷新凭据 (来自 %s)",
						p.Name, p.Mode, p.Name, p.Name, configPath)
				}
				// 给个候选 profile 提示, 避免用户对着空 default 不知道该用哪个.
				if hint := suggestUsableProfiles(cfg.Profiles, p.Name); hint != "" {
					return nil, fmt.Errorf("%w; %s", err, hint)
				}
				return nil, err
			}
			// CloudSSO 的 STS 通常 1 小时过期, 过期后必须重新登录.
			if p.StsToken != "" && p.StsExpiration > 0 && time.Now().Unix() > p.StsExpiration {
				return nil, fmt.Errorf("profile '%s' STS 凭据已于 %s 过期, 请运行 `aliyun sso login --profile %s` 刷新",
					p.Name, time.Unix(p.StsExpiration, 0).Format("2006-01-02 15:04:05"), p.Name)
			}
			return result, nil
		}
	}
	return nil, fmt.Errorf("profile '%s' not found in config", targetProfile)
}

// suggestUsableProfiles 当用户撞到空凭据时, 把 config 里其它"看起来能用"的
// profile 列出来作为提示, 省掉一次 `tssh profiles` + `--profile X` 的来回.
// 入参用匿名结构体切片, 直接复用 Load 内部的解析结果.
func suggestUsableProfiles(profiles []struct {
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	StsToken        string `json:"sts_token"`
	StsExpiration   int64  `json:"sts_expiration"`
	RegionID        string `json:"region_id"`
}, skip string) string {
	var ok []string
	for _, p := range profiles {
		if p.Name == skip {
			continue
		}
		if p.AccessKeyID != "" && p.AccessKeySecret != "" {
			ok = append(ok, p.Name)
		}
	}
	if len(ok) == 0 {
		return ""
	}
	return fmt.Sprintf("可用 profile: %v, 试试 `--profile %s` 或 `aliyun configure switch --profile %s`",
		ok, ok[0], ok[0])
}

// validateCreds 在把 profile 交给 SDK 之前做空值兜底, 避免冒出
// "AccessKeyId not supplied" 这种用户看不懂的底层错误.
func validateCreds(c *model.Config, source string) error {
	if c.AccessKeyID == "" || c.AccessKeySecret == "" {
		return fmt.Errorf("profile '%s' 缺少 access_key_id/access_key_secret (来自 %s); 请配置环境变量 ALIBABA_CLOUD_ACCESS_KEY_ID/SECRET 或编辑该文件", c.ProfileName, source)
	}
	return nil
}

// LoadGrafana reads Grafana configuration.
// Priority: env vars → ~/.tssh/config.json
func LoadGrafana() (*model.GrafanaConfig, error) {
	endpoint := os.Getenv("TSSH_GRAFANA_URL")
	token := os.Getenv("TSSH_GRAFANA_TOKEN")

	if endpoint != "" && token != "" {
		return &model.GrafanaConfig{Endpoint: endpoint, Token: token}, nil
	}

	home, _ := os.UserHomeDir()
	tsshConfigPath := filepath.Join(home, ".tssh", "config.json")
	if data, err := os.ReadFile(tsshConfigPath); err == nil {
		var cfg TsshConfig
		if err := json.Unmarshal(data, &cfg); err == nil && cfg.Grafana != nil {
			g := cfg.Grafana
			if endpoint != "" {
				g.Endpoint = endpoint
			}
			if token != "" {
				g.Token = token
			}
			if g.Endpoint == "" || g.Token == "" {
				return nil, fmt.Errorf("grafana 配置不完整: 需要 endpoint 和 token")
			}
			return g, nil
		}
	}

	return nil, fmt.Errorf("未找到 Grafana 配置: 请设置环境变量 TSSH_GRAFANA_URL/TSSH_GRAFANA_TOKEN 或在 ~/.tssh/config.json 中添加 grafana 段")
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
			Profiles []struct {
				Name string `json:"name"`
			} `json:"profiles"`
		}
		if err := json.Unmarshal(data, &cfg); err == nil {
			for _, p := range cfg.Profiles {
				profiles = append(profiles, "aliyun:"+p.Name)
			}
		}
	}
	return profiles
}
