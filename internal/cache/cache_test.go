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

func TestFilterInstances_EmptyPattern(t *testing.T) {
	instances := []model.Instance{{Name: "a"}, {Name: "b"}}
	result := FilterInstances(instances, "")
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

func TestFilterInstances_WhitespacePattern(t *testing.T) {
	instances := []model.Instance{{Name: "a"}}
	result := FilterInstances(instances, "   ")
	if len(result) != 1 {
		t.Errorf("whitespace-only pattern should match all, got %d", len(result))
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

func TestFilterInstances_CaseInsensitive(t *testing.T) {
	instances := []model.Instance{{Name: "Prod-Web-01"}}
	result := FilterInstances(instances, "prod")
	if len(result) != 1 {
		t.Errorf("keyword match should be case insensitive")
	}
}

func TestFilterInstances_ByIP(t *testing.T) {
	instances := []model.Instance{
		{Name: "a", PrivateIP: "10.0.0.1"},
		{Name: "b", PrivateIP: "10.0.0.2"},
	}
	result := FilterInstances(instances, "10.0.0.1")
	if len(result) != 1 || result[0].Name != "a" {
		t.Errorf("expected match by IP, got %v", result)
	}
}

func TestFilterInstances_ByPublicIP(t *testing.T) {
	instances := []model.Instance{
		{Name: "a", PublicIP: "1.2.3.4"},
		{Name: "b", EIP: "5.6.7.8"},
	}
	result := FilterInstances(instances, "1.2.3.4")
	if len(result) != 1 {
		t.Errorf("expected match by PublicIP")
	}
	result = FilterInstances(instances, "5.6.7.8")
	if len(result) != 1 {
		t.Errorf("expected match by EIP")
	}
}

func TestFilterInstances_ByID(t *testing.T) {
	instances := []model.Instance{{Name: "a", ID: "i-abcdef123"}}
	result := FilterInstances(instances, "i-abcdef123")
	if len(result) != 1 {
		t.Errorf("expected match by ID")
	}
}

func TestFilterInstances_ByTagInKeyword(t *testing.T) {
	instances := []model.Instance{
		{Name: "a", Tags: map[string]string{"env": "prod"}},
		{Name: "b", Tags: map[string]string{"env": "staging"}},
	}
	result := FilterInstances(instances, "env=prod")
	if len(result) != 1 {
		t.Errorf("expected tag match in keyword mode, got %d", len(result))
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

func TestFilterInstances_TagKeyOnly(t *testing.T) {
	instances := []model.Instance{
		{Name: "a", Tags: map[string]string{"env": "prod"}},
		{Name: "b", Tags: map[string]string{}},
	}
	result := FilterInstances(instances, "tag:env")
	if len(result) != 1 {
		t.Errorf("tag key-only filter: expected 1, got %d", len(result))
	}
}

func TestFilterInstances_TagCaseInsensitive(t *testing.T) {
	instances := []model.Instance{
		{Name: "a", Tags: map[string]string{"Env": "Prod"}},
	}
	result := FilterInstances(instances, "tag:env=prod")
	if len(result) != 1 {
		t.Errorf("tag match should be case insensitive")
	}
}

func TestFilterInstances_TagNoMatch(t *testing.T) {
	instances := []model.Instance{
		{Name: "a", Tags: map[string]string{"env": "prod"}},
	}
	result := FilterInstances(instances, "tag:role=web")
	if len(result) != 0 {
		t.Errorf("expected 0 for unmatched tag, got %d", len(result))
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

func TestFilterInstances_RegexCaseInsensitive(t *testing.T) {
	instances := []model.Instance{{Name: "Prod-Web"}}
	result := FilterInstances(instances, "/prod-web/")
	if len(result) != 1 {
		t.Errorf("regex should be case insensitive")
	}
}

func TestFilterInstances_InvalidRegex(t *testing.T) {
	instances := []model.Instance{{Name: "test"}}
	// Invalid regex should fall through to keyword match
	result := FilterInstances(instances, "/[invalid/")
	// "test" doesn't contain "[invalid" so 0 matches
	if len(result) != 0 {
		t.Errorf("invalid regex should fall to keyword, got %d", len(result))
	}
}

func TestFilterInstances_ShortRegex(t *testing.T) {
	instances := []model.Instance{{Name: "ab"}}
	// "/" alone is not a regex pattern
	result := FilterInstances(instances, "/")
	if len(result) != 0 {
		t.Errorf("single slash should be keyword, got %d", len(result))
	}
}

func TestFilterInstances_NoInstances(t *testing.T) {
	result := FilterInstances([]model.Instance{}, "prod")
	if result != nil && len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

func TestCacheSaveLoad(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}

	instances := []model.Instance{
		{ID: "i-001", Name: "test-01", Status: "Running", PrivateIP: "10.0.0.1"},
		{ID: "i-002", Name: "test-02", Tags: map[string]string{"env": "prod"}},
	}

	if err := c.Save(instances); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := c.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(loaded) != 2 || loaded[0].ID != "i-001" {
		t.Errorf("unexpected: %v", loaded)
	}
	if loaded[1].Tags["env"] != "prod" {
		t.Errorf("tags not preserved: %v", loaded[1].Tags)
	}
}

func TestCacheLoad_Missing(t *testing.T) {
	c := &Cache{dir: t.TempDir(), file: "/nonexistent/file.json"}
	_, err := c.Load()
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestCacheLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.json")
	os.WriteFile(f, []byte("{invalid"), 0644)
	c := &Cache{dir: dir, file: f}
	_, err := c.Load()
	if err == nil {
		t.Error("expected error for invalid JSON")
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

func TestCacheFindByPattern(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}
	c.Save([]model.Instance{
		{Name: "prod-web-01"},
		{Name: "prod-db-01"},
		{Name: "staging-01"},
	})

	results, err := c.FindByPattern("prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2, got %d", len(results))
	}
}

func TestCacheFindByTag(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}
	c.Save([]model.Instance{
		{Name: "a", Tags: map[string]string{"env": "prod"}},
		{Name: "b", Tags: map[string]string{"env": "staging"}},
		{Name: "c", Tags: map[string]string{}},
	})

	results, err := c.FindByTag("env", "prod")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 1 || results[0].Name != "a" {
		t.Errorf("expected a, got %v", results)
	}

	// Key only
	results, _ = c.FindByTag("env", "")
	if len(results) != 2 {
		t.Errorf("key-only: expected 2, got %d", len(results))
	}
}

func TestCacheFindByTag_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}
	c.Save([]model.Instance{
		{Name: "a", Tags: map[string]string{"Env": "Prod"}},
	})

	results, _ := c.FindByTag("env", "prod")
	if len(results) != 1 {
		t.Errorf("tag search should be case insensitive")
	}
}

func TestCacheAge(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}

	age := c.Age()
	if age.Hours() < 24 {
		t.Errorf("expected large age for missing file, got %v", age)
	}

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

func TestCacheEnsure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "deep")
	c := &Cache{dir: dir, file: filepath.Join(dir, "test.json")}
	if err := c.Ensure(); err != nil {
		t.Fatalf("ensure failed: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Error("directory should exist after ensure")
	}
}

func TestCacheHistoryDir(t *testing.T) {
	c := &Cache{dir: "/tmp/test-cache"}
	if c.HistoryDir() != "/tmp/test-cache" {
		t.Errorf("unexpected: %s", c.HistoryDir())
	}
}

func TestNewWithProfile(t *testing.T) {
	c1 := NewWithProfile("default")
	c2 := NewWithProfile("staging")
	c3 := NewWithProfile("")
	c4 := New()

	home, _ := os.UserHomeDir()
	expected1 := filepath.Join(home, ".cache", "tssh", "instances.json")
	expected2 := filepath.Join(home, ".cache", "tssh", "instances_staging.json")

	if c1.file != expected1 {
		t.Errorf("default: expected %s, got %s", expected1, c1.file)
	}
	if c2.file != expected2 {
		t.Errorf("staging: expected %s, got %s", expected2, c2.file)
	}
	if c3.file != expected1 {
		t.Errorf("empty: expected %s, got %s", expected1, c3.file)
	}
	if c4.file != expected1 {
		t.Errorf("New(): expected %s, got %s", expected1, c4.file)
	}
}

// --- Additional tests for 100% coverage ---

func TestCacheSave_EnsureError(t *testing.T) {
	// Use a path that can't be created (file exists where dir expected)
	tmpFile := filepath.Join(t.TempDir(), "blocker")
	os.WriteFile(tmpFile, []byte("x"), 0644)
	c := &Cache{dir: filepath.Join(tmpFile, "sub"), file: filepath.Join(tmpFile, "sub", "test.json")}

	err := c.Save([]model.Instance{{Name: "test"}})
	if err == nil {
		t.Error("expected error when dir can't be created")
	}
}

func TestCacheFindByName_LoadError(t *testing.T) {
	c := &Cache{dir: t.TempDir(), file: "/nonexistent/file.json"}
	_, err := c.FindByName("test")
	if err == nil {
		t.Error("expected error for missing cache file")
	}
}

func TestCacheFindByPattern_LoadError(t *testing.T) {
	c := &Cache{dir: t.TempDir(), file: "/nonexistent/file.json"}
	_, err := c.FindByPattern("test")
	if err == nil {
		t.Error("expected error for missing cache file")
	}
}

func TestCacheFindByTag_LoadError(t *testing.T) {
	c := &Cache{dir: t.TempDir(), file: "/nonexistent/file.json"}
	_, err := c.FindByTag("env", "prod")
	if err == nil {
		t.Error("expected error for missing cache file")
	}
}
