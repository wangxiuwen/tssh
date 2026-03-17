package config

import (
	"os"
	"testing"
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

func TestLoadNoCredentials(t *testing.T) {
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	os.Unsetenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")

	// This will fail if no config files exist, which is expected in CI
	_, err := Load("nonexistent-profile-12345")
	if err == nil {
		t.Error("expected error for nonexistent profile")
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
