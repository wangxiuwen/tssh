package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangxiuwen/tssh/internal/model"
)

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("ALIBABA_CLOUD_ACCESS_KEY_ID", "test-id")
	os.Setenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "test-secret")
	os.Setenv("ALIBABA_CLOUD_REGION_ID", "cn-shanghai")
	defer func() {
		os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
		os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
		os.Unsetenv("ALIBABA_CLOUD_REGION_ID")
	}()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.AccessKeyID != "test-id" {
		t.Errorf("expected test-id, got %s", cfg.AccessKeyID)
	}
	if cfg.AccessKeySecret != "test-secret" {
		t.Errorf("expected test-secret, got %s", cfg.AccessKeySecret)
	}
	if cfg.Region != "cn-shanghai" {
		t.Errorf("expected cn-shanghai, got %s", cfg.Region)
	}
	if cfg.ProfileName != "env" {
		t.Errorf("expected env, got %s", cfg.ProfileName)
	}
}

func TestLoadFromEnvDefaultRegion(t *testing.T) {
	os.Setenv("ALIBABA_CLOUD_ACCESS_KEY_ID", "id")
	os.Setenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "secret")
	os.Unsetenv("ALIBABA_CLOUD_REGION_ID")
	defer func() {
		os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
		os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	}()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Region != "cn-beijing" {
		t.Errorf("expected cn-beijing default, got %s", cfg.Region)
	}
}

func TestLoadFromEnv_IgnoredWhenProfileSet(t *testing.T) {
	os.Setenv("ALIBABA_CLOUD_ACCESS_KEY_ID", "env-id")
	os.Setenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "env-secret")
	defer func() {
		os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
		os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	}()

	// When profile is set, env vars should NOT be used (falls through to config files)
	_, err := Load("some-profile")
	// Should fail because no config files with this profile exist
	if err == nil {
		t.Error("expected error when profile is set but no config files exist")
	}
}

func TestLoadNoCredentials(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	_, err := Load("nonexistent-profile-12345")
	if err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestLoadFromTsshConfig(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	// Create temp ~/.tssh/config.json
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	tsshDir := filepath.Join(home, ".tssh")
	os.MkdirAll(tsshDir, 0755)

	cfg := TsshConfig{
		Default: "prod",
		Profiles: []model.Config{
			{ProfileName: "prod", AccessKeyID: "prod-id", AccessKeySecret: "prod-secret", Region: "cn-hangzhou"},
			{ProfileName: "staging", AccessKeyID: "stg-id", AccessKeySecret: "stg-secret", Region: ""},
		},
	}

	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tsshDir, "config.json"), data, 0644)

	// Load default profile
	result, err := Load("")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.AccessKeyID != "prod-id" {
		t.Errorf("expected prod-id, got %s", result.AccessKeyID)
	}
	if result.Region != "cn-hangzhou" {
		t.Errorf("expected cn-hangzhou, got %s", result.Region)
	}

	// Load specific profile
	result, err = Load("staging")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.AccessKeyID != "stg-id" {
		t.Errorf("expected stg-id, got %s", result.AccessKeyID)
	}
	if result.Region != "cn-beijing" {
		t.Errorf("empty region should default to cn-beijing, got %s", result.Region)
	}
}

func TestLoadFromTsshConfig_ProfileNotFound(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	tsshDir := filepath.Join(home, ".tssh")
	os.MkdirAll(tsshDir, 0755)

	cfg := TsshConfig{
		Profiles: []model.Config{
			{ProfileName: "prod", AccessKeyID: "id", AccessKeySecret: "secret"},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tsshDir, "config.json"), data, 0644)

	_, err := Load("nonexistent")
	if err == nil {
		t.Error("expected error for missing profile")
	}
}

func TestLoadFromAliyunConfig(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	cfg := struct {
		Profiles []struct {
			Name            string `json:"name"`
			AccessKeyID     string `json:"access_key_id"`
			AccessKeySecret string `json:"access_key_secret"`
			RegionID        string `json:"region_id"`
		} `json:"profiles"`
	}{
		Profiles: []struct {
			Name            string `json:"name"`
			AccessKeyID     string `json:"access_key_id"`
			AccessKeySecret string `json:"access_key_secret"`
			RegionID        string `json:"region_id"`
		}{
			{Name: "default", AccessKeyID: "aliyun-id", AccessKeySecret: "aliyun-secret", RegionID: "cn-shenzhen"},
			{Name: "dev", AccessKeyID: "dev-id", AccessKeySecret: "dev-secret", RegionID: ""},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), data, 0644)

	result, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AccessKeyID != "aliyun-id" {
		t.Errorf("expected aliyun-id, got %s", result.AccessKeyID)
	}
	if result.Region != "cn-shenzhen" {
		t.Errorf("expected cn-shenzhen, got %s", result.Region)
	}

	// Load specific profile
	result, err = Load("dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Region != "cn-beijing" {
		t.Errorf("empty region should default, got %s", result.Region)
	}
}

func TestLoadFromAliyunConfig_InvalidJSON(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), []byte("{bad json"), 0644)

	_, err := Load("")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadFromAliyunConfig_ProfileNotFound(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	cfg := struct {
		Profiles []struct {
			Name string `json:"name"`
		} `json:"profiles"`
	}{
		Profiles: []struct {
			Name string `json:"name"`
		}{{Name: "default"}},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), data, 0644)

	_, err := Load("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

func TestListProfilesIncludesEnv(t *testing.T) {
	os.Setenv("ALIBABA_CLOUD_ACCESS_KEY_ID", "test")
	defer os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")

	profiles := ListProfiles()
	found := false
	for _, p := range profiles {
		if p == "env" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'env' in profiles when env var is set")
	}
}

func TestListProfilesNoEnv(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	profiles := ListProfiles()
	for _, p := range profiles {
		if p == "env" {
			t.Error("should not include 'env' when env var is not set")
		}
	}
}

func TestListProfilesFromConfigs(t *testing.T) {
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")

	// Create tssh config
	tsshDir := filepath.Join(home, ".tssh")
	os.MkdirAll(tsshDir, 0755)
	tsshCfg := TsshConfig{
		Profiles: []model.Config{
			{ProfileName: "prod"},
			{ProfileName: "staging"},
		},
	}
	data, _ := json.Marshal(tsshCfg)
	os.WriteFile(filepath.Join(tsshDir, "config.json"), data, 0644)

	// Create aliyun config
	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)
	aliyunCfg := struct {
		Profiles []struct {
			Name string `json:"name"`
		} `json:"profiles"`
	}{
		Profiles: []struct {
			Name string `json:"name"`
		}{{Name: "default"}},
	}
	data, _ = json.Marshal(aliyunCfg)
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), data, 0644)

	profiles := ListProfiles()
	if len(profiles) < 3 {
		t.Errorf("expected at least 3 profiles (prod, staging, aliyun:default), got %v", profiles)
	}
}

// --- Grafana config tests ---

func TestLoadGrafanaFromEnv(t *testing.T) {
	os.Setenv("TSSH_GRAFANA_URL", "https://grafana.example.com")
	os.Setenv("TSSH_GRAFANA_TOKEN", "glsa_test_token")
	defer func() {
		os.Unsetenv("TSSH_GRAFANA_URL")
		os.Unsetenv("TSSH_GRAFANA_TOKEN")
	}()

	cfg, err := LoadGrafana()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "https://grafana.example.com" {
		t.Errorf("expected https://grafana.example.com, got %s", cfg.Endpoint)
	}
	if cfg.Token != "glsa_test_token" {
		t.Errorf("expected glsa_test_token, got %s", cfg.Token)
	}
}

func TestLoadGrafanaFromConfigFile(t *testing.T) {
	os.Unsetenv("TSSH_GRAFANA_URL")
	os.Unsetenv("TSSH_GRAFANA_TOKEN")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	tsshDir := filepath.Join(home, ".tssh")
	os.MkdirAll(tsshDir, 0755)

	cfg := TsshConfig{
		Profiles: []model.Config{
			{ProfileName: "default", AccessKeyID: "id", AccessKeySecret: "secret"},
		},
		Grafana: &model.GrafanaConfig{
			Endpoint: "https://grafana.test.com",
			Token:    "glsa_file_token",
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tsshDir, "config.json"), data, 0644)

	result, err := LoadGrafana()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Endpoint != "https://grafana.test.com" {
		t.Errorf("expected https://grafana.test.com, got %s", result.Endpoint)
	}
	if result.Token != "glsa_file_token" {
		t.Errorf("expected glsa_file_token, got %s", result.Token)
	}
}

func TestLoadGrafanaEnvOverridesFile(t *testing.T) {
	os.Setenv("TSSH_GRAFANA_URL", "https://env-grafana.com")
	os.Unsetenv("TSSH_GRAFANA_TOKEN")
	defer os.Unsetenv("TSSH_GRAFANA_URL")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	tsshDir := filepath.Join(home, ".tssh")
	os.MkdirAll(tsshDir, 0755)

	cfg := TsshConfig{
		Grafana: &model.GrafanaConfig{
			Endpoint: "https://file-grafana.com",
			Token:    "glsa_file_token",
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tsshDir, "config.json"), data, 0644)

	result, err := LoadGrafana()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Endpoint != "https://env-grafana.com" {
		t.Errorf("env should override file endpoint, got %s", result.Endpoint)
	}
	if result.Token != "glsa_file_token" {
		t.Errorf("token should come from file, got %s", result.Token)
	}
}

func TestLoadGrafanaNoConfig(t *testing.T) {
	os.Unsetenv("TSSH_GRAFANA_URL")
	os.Unsetenv("TSSH_GRAFANA_TOKEN")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	_, err := LoadGrafana()
	if err == nil {
		t.Error("expected error when no grafana config exists")
	}
}

func TestLoadGrafanaIncompleteConfig(t *testing.T) {
	os.Unsetenv("TSSH_GRAFANA_URL")
	os.Unsetenv("TSSH_GRAFANA_TOKEN")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	tsshDir := filepath.Join(home, ".tssh")
	os.MkdirAll(tsshDir, 0755)

	// Only endpoint, no token
	cfg := TsshConfig{
		Grafana: &model.GrafanaConfig{
			Endpoint: "https://grafana.test.com",
			Token:    "",
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tsshDir, "config.json"), data, 0644)

	_, err := LoadGrafana()
	if err == nil {
		t.Error("expected error for incomplete grafana config")
	}
}

// --- Additional tests for 100% coverage ---

func TestLoadFromTsshConfig_NoDefaultFallsToDefault(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	tsshDir := filepath.Join(home, ".tssh")
	os.MkdirAll(tsshDir, 0755)

	// No "default" field in config, and no explicit profile → falls to "default" profile name
	cfg := TsshConfig{
		Default: "",
		Profiles: []model.Config{
			{ProfileName: "default", AccessKeyID: "def-id", AccessKeySecret: "def-secret", Region: "cn-beijing"},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tsshDir, "config.json"), data, 0644)

	result, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AccessKeyID != "def-id" {
		t.Errorf("expected def-id, got %s", result.AccessKeyID)
	}
}

// 用户跑过 `aliyun configure switch --profile turingsso` 后, tssh 不传 --profile
// 也应该跟着用 turingsso, 不该再去找字面 "default".
func TestLoadFromAliyunConfig_FollowsCurrent(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	os.Unsetenv("ALIBABA_CLOUD_PROFILE")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	raw := `{"current":"sso","profiles":[
		{"name":"default","mode":"AK","access_key_id":"","access_key_secret":"","region_id":"cn-beijing"},
		{"name":"sso","mode":"AK","access_key_id":"sso-id","access_key_secret":"sso-sec","region_id":"cn-shanghai"}
	]}`
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), []byte(raw), 0644)

	got, err := Load("")
	if err != nil {
		t.Fatalf("expected to follow `current` field, got error: %v", err)
	}
	if got.ProfileName != "sso" {
		t.Errorf("expected profile sso (from `current`), got %q", got.ProfileName)
	}
}

// 显式 --profile 一定盖过 current 字段.
func TestLoadFromAliyunConfig_ExplicitProfileOverridesCurrent(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	os.Unsetenv("ALIBABA_CLOUD_PROFILE")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	raw := `{"current":"a","profiles":[
		{"name":"a","mode":"AK","access_key_id":"a-id","access_key_secret":"a-sec"},
		{"name":"b","mode":"AK","access_key_id":"b-id","access_key_secret":"b-sec"}
	]}`
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), []byte(raw), 0644)

	got, err := Load("b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProfileName != "b" {
		t.Errorf("expected explicit profile b, got %q", got.ProfileName)
	}
}

// ALIBABA_CLOUD_PROFILE 充当 sticky default, 等同于在每次命令前加 --profile.
func TestLoadFromAliyunConfig_EnvProfileVar(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	os.Setenv("ALIBABA_CLOUD_PROFILE", "envpicked")
	defer os.Unsetenv("ALIBABA_CLOUD_PROFILE")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	raw := `{"current":"default","profiles":[
		{"name":"default","mode":"AK","access_key_id":"def","access_key_secret":"def"},
		{"name":"envpicked","mode":"AK","access_key_id":"e-id","access_key_secret":"e-sec"}
	]}`
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), []byte(raw), 0644)

	got, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProfileName != "envpicked" {
		t.Errorf("expected ALIBABA_CLOUD_PROFILE to win, got %q", got.ProfileName)
	}
}

// default 空 + 没设 current 时, 错误信息要把可用 profile 列出来,
// 不能只丢一句"default 缺少 AK".
func TestLoadFromAliyunConfig_EmptyDefaultListsCandidates(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	os.Unsetenv("ALIBABA_CLOUD_PROFILE")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	raw := `{"profiles":[
		{"name":"default","mode":"AK","access_key_id":"","access_key_secret":""},
		{"name":"prod","mode":"AK","access_key_id":"p-id","access_key_secret":"p-sec"}
	]}`
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), []byte(raw), 0644)

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for empty default")
	}
	if !strings.Contains(err.Error(), "prod") {
		t.Errorf("error should suggest prod, got: %v", err)
	}
}

// 模拟 aliyun-cli `aliyun sso login` 写出的 CloudSSO profile, 校验
// sts_token 能被透传到 model.Config (否则 SDK 用纯 STS.* AK 调用会被拒).
func TestLoadFromAliyunConfig_CloudSSO(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	future := time.Now().Add(1 * time.Hour).Unix()
	raw := `{"profiles":[{"name":"sso","mode":"CloudSSO","access_key_id":"STS.test","access_key_secret":"sec","sts_token":"tok","sts_expiration":` +
		strings.TrimSpace(formatInt(future)) + `,"region_id":"cn-shanghai"}]}`
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), []byte(raw), 0644)

	got, err := Load("sso")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SecurityToken != "tok" {
		t.Errorf("expected sts_token to propagate, got %q", got.SecurityToken)
	}
	if got.AccessKeyID != "STS.test" {
		t.Errorf("expected STS AK, got %q", got.AccessKeyID)
	}
}

// 用户 SSO 登录但 token 已过期, 必须报"请重新 sso login"而不是把空/过期凭据
// 直接喂给 SDK 让它返回看不懂的 InvalidAccessKeyId.Inactive.
func TestLoadFromAliyunConfig_CloudSSOExpired(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	past := time.Now().Add(-1 * time.Hour).Unix()
	raw := `{"profiles":[{"name":"sso","mode":"CloudSSO","access_key_id":"STS.test","access_key_secret":"sec","sts_token":"tok","sts_expiration":` +
		strings.TrimSpace(formatInt(past)) + `,"region_id":"cn-shanghai"}]}`
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), []byte(raw), 0644)

	_, err := Load("sso")
	if err == nil {
		t.Fatal("expected error for expired STS")
	}
	if !strings.Contains(err.Error(), "过期") {
		t.Errorf("error should mention expiration, got: %v", err)
	}
}

// SSO profile 完全没填 AK (例如 aliyun configure --mode CloudSSO 但还没 sso login),
// 我们应该提示去刷新, 而不是抛 SDK 的 MissingParameter.
func TestLoadFromAliyunConfig_SSOMissingCreds(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	aliyunDir := filepath.Join(home, ".aliyun")
	os.MkdirAll(aliyunDir, 0755)

	raw := `{"profiles":[{"name":"sso","mode":"CloudSSO","access_key_id":"","access_key_secret":"","region_id":"cn-shanghai"}]}`
	os.WriteFile(filepath.Join(aliyunDir, "config.json"), []byte(raw), 0644)

	_, err := Load("sso")
	if err == nil {
		t.Fatal("expected error for empty SSO credentials")
	}
	if !strings.Contains(err.Error(), "sso login") {
		t.Errorf("error should hint at `aliyun sso login`, got: %v", err)
	}
}

func formatInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestLoadNoConfigFiles(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	home := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", origHome)

	// No ~/.tssh/ and no ~/.aliyun/ → should get "no credentials found" error
	_, err := Load("")
	if err == nil {
		t.Error("expected error when no config files exist")
	}
}
