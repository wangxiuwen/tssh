package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wangxiuwen/tssh/internal/model"
)

func TestFilterInstances_Empty(t *testing.T) {
	result := FilterInstances(nil, "")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestFilterInstances_Keyword(t *testing.T) {
	instances := []model.Instance{
		{Name: "prod-web-01", PrivateIP: "10.0.0.1"},
		{Name: "prod-db-01", PrivateIP: "10.0.0.2"},
		{Name: "staging-web-01", PrivateIP: "10.0.1.1"},
	}

	result := FilterInstances(instances, "prod")
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}

	result = FilterInstances(instances, "web")
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}

	result = FilterInstances(instances, "prod web")
	if len(result) != 1 {
		t.Errorf("expected 1 (AND match), got %d", len(result))
	}
}

func TestFilterInstances_Tag(t *testing.T) {
	instances := []model.Instance{
		{Name: "a", Tags: map[string]string{"env": "prod", "role": "web"}},
		{Name: "b", Tags: map[string]string{"env": "prod", "role": "db"}},
		{Name: "c", Tags: map[string]string{"env": "staging"}},
	}

	result := FilterInstances(instances, "tag:env=prod")
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}

	result = FilterInstances(instances, "tag:role=web")
	if len(result) != 1 {
		t.Errorf("expected 1, got %d", len(result))
	}
}

func TestFilterInstances_Regex(t *testing.T) {
	instances := []model.Instance{
		{Name: "prod-web-01"},
		{Name: "prod-web-02"},
		{Name: "staging-web-01"},
	}

	result := FilterInstances(instances, "/prod-web-0[12]/")
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

func TestCacheSaveLoad(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{
		dir:  dir,
		file: filepath.Join(dir, "test.json"),
	}

	instances := []model.Instance{
		{ID: "i-001", Name: "test-01", Status: "Running", PrivateIP: "10.0.0.1"},
	}

	if err := c.Save(instances); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := c.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(loaded) != 1 || loaded[0].ID != "i-001" {
		t.Errorf("unexpected: %v", loaded)
	}
}

func TestCacheFindByName(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}

	instances := []model.Instance{
		{ID: "i-001", Name: "web-01"},
		{ID: "i-002", Name: "db-01"},
	}
	c.Save(instances)

	inst, err := c.FindByName("web-01")
	if err != nil || inst.ID != "i-001" {
		t.Errorf("expected i-001, got %v err=%v", inst, err)
	}

	_, err = c.FindByName("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent")
	}
}

func TestCacheAge(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}

	// Non-existent file should return large age
	age := c.Age()
	if age.Hours() < 24 {
		t.Errorf("expected large age for missing file, got %v", age)
	}

	// After save, age should be small
	c.Save([]model.Instance{})
	age = c.Age()
	if age.Seconds() > 5 {
		t.Errorf("expected small age after save, got %v", age)
	}
}

func TestCacheExists(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}

	if c.Exists() {
		t.Error("should not exist before save")
	}

	c.Save([]model.Instance{})
	if !c.Exists() {
		t.Error("should exist after save")
	}
}

func TestNewWithProfile(t *testing.T) {
	c1 := NewWithProfile("default")
	c2 := NewWithProfile("staging")
	c3 := NewWithProfile("")

	home, _ := os.UserHomeDir()
	expected1 := filepath.Join(home, ".cache", "tssh", "instances.json")
	expected2 := filepath.Join(home, ".cache", "tssh", "instances_staging.json")

	if c1.file != expected1 {
		t.Errorf("default profile: expected %s, got %s", expected1, c1.file)
	}
	if c2.file != expected2 {
		t.Errorf("staging profile: expected %s, got %s", expected2, c2.file)
	}
	if c3.file != expected1 {
		t.Errorf("empty profile: expected %s, got %s", expected1, c3.file)
	}
}
