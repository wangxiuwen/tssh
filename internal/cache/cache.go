package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wangxiuwen/tssh/internal/model"
)

// Cache manages local instance cache, scoped by profile
type Cache struct {
	dir     string
	file    string
	profile string
}

// New creates a cache with the default profile
func New() *Cache {
	return NewWithProfile("default")
}

// NewWithProfile creates a cache scoped to a specific profile
func NewWithProfile(profile string) *Cache {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".cache", "tssh")
	file := "instances.json"
	if profile != "" && profile != "default" {
		file = fmt.Sprintf("instances_%s.json", profile)
	}
	return &Cache{dir: dir, file: filepath.Join(dir, file), profile: profile}
}

func (c *Cache) Ensure() error      { return os.MkdirAll(c.dir, 0755) }
func (c *Cache) HistoryDir() string { return c.dir }
func (c *Cache) Exists() bool       { _, err := os.Stat(c.file); return err == nil }

func (c *Cache) Age() time.Duration {
	info, err := os.Stat(c.file)
	if err != nil {
		return time.Hour * 24 * 365
	}
	return time.Since(info.ModTime())
}

func (c *Cache) Save(instances []model.Instance) error {
	if err := c.Ensure(); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(instances, "", "  ")
	// Atomic write: os.WriteFile truncates in place; Ctrl-C or a crash
	// mid-write leaves a corrupt JSON that fails Load() on every future run
	// until the user notices and re-syncs. Write to a temp file then rename
	// (rename is atomic on POSIX).
	// 0600: instance list contains internal IPs + tags + EIPs — inventory
	// data other users on a shared host shouldn't see.
	tmp := c.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, c.file)
}

func (c *Cache) Load() ([]model.Instance, error) {
	data, err := os.ReadFile(c.file)
	if err != nil {
		return nil, err
	}
	var instances []model.Instance
	return instances, json.Unmarshal(data, &instances)
}

func (c *Cache) FindByName(name string) (*model.Instance, error) {
	instances, err := c.Load()
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.Name == name {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance '%s' not found", name)
}

func (c *Cache) FindByPattern(pattern string) ([]model.Instance, error) {
	instances, err := c.Load()
	if err != nil {
		return nil, err
	}
	return FilterInstances(instances, pattern), nil
}

func (c *Cache) FindByTag(key, value string) ([]model.Instance, error) {
	instances, err := c.Load()
	if err != nil {
		return nil, err
	}
	var result []model.Instance
	key, value = strings.ToLower(key), strings.ToLower(value)
	for _, inst := range instances {
		for k, v := range inst.Tags {
			if strings.ToLower(k) == key && (value == "" || strings.ToLower(v) == value) {
				result = append(result, inst)
				break
			}
		}
	}
	return result, nil
}

// FilterInstances filters instances using smart pattern matching
func FilterInstances(instances []model.Instance, pattern string) []model.Instance {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return instances
	}

	// Tag filter
	if strings.HasPrefix(pattern, "tag:") {
		tagExpr := pattern[4:]
		parts := strings.SplitN(tagExpr, "=", 2)
		key := strings.ToLower(parts[0])
		value := ""
		if len(parts) == 2 {
			value = strings.ToLower(parts[1])
		}
		var result []model.Instance
		for _, inst := range instances {
			for k, v := range inst.Tags {
				if strings.ToLower(k) == key && (value == "" || strings.ToLower(v) == value) {
					result = append(result, inst)
					break
				}
			}
		}
		return result
	}

	// Regex
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) > 2 {
		re, err := regexp.Compile("(?i)" + pattern[1:len(pattern)-1])
		if err == nil {
			var result []model.Instance
			for _, inst := range instances {
				s := inst.Name + " " + inst.PrivateIP + " " + inst.PublicIP + " " + inst.EIP + " " + inst.ID
				if re.MatchString(s) {
					result = append(result, inst)
				}
			}
			return result
		}
	}

	// Multi-keyword AND
	keywords := strings.Fields(strings.ToLower(pattern))
	var result []model.Instance
	for _, inst := range instances {
		s := strings.ToLower(inst.Name + " " + inst.PrivateIP + " " + inst.PublicIP + " " + inst.EIP + " " + inst.ID)
		for k, v := range inst.Tags {
			s += " " + strings.ToLower(k) + "=" + strings.ToLower(v)
		}
		allMatch := true
		for _, kw := range keywords {
			if !strings.Contains(s, kw) {
				allMatch = false
				break
			}
		}
		if allMatch {
			result = append(result, inst)
		}
	}
	return result
}
