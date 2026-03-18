package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
