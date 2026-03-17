package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Instance represents an Alibaba Cloud ECS instance
type Instance struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	PrivateIP string            `json:"private_ip"`
	PublicIP  string            `json:"public_ip"`
	EIP       string            `json:"eip"`
	Region    string            `json:"region"`
	Zone      string            `json:"zone"`
	Tags      map[string]string `json:"tags,omitempty"`
	Profile   string            `json:"profile,omitempty"`
}

// Cache manages local instance cache, scoped by profile
type Cache struct {
	dir     string
	file    string
	profile string
}

func NewCache() *Cache {
	return NewCacheWithProfile("default")
}

func NewCacheWithProfile(profile string) *Cache {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".cache", "tssh")
	file := "instances.json"
	if profile != "" && profile != "default" {
		file = fmt.Sprintf("instances_%s.json", profile)
	}
	return &Cache{
		dir:     dir,
		file:    filepath.Join(dir, file),
		profile: profile,
	}
}

func (c *Cache) Ensure() error {
	return os.MkdirAll(c.dir, 0755)
}

func (c *Cache) Save(instances []Instance) error {
	if err := c.Ensure(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(instances, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.file, data, 0644)
}

func (c *Cache) Load() ([]Instance, error) {
	data, err := os.ReadFile(c.file)
	if err != nil {
		return nil, err
	}
	var instances []Instance
	err = json.Unmarshal(data, &instances)
	return instances, err
}

func (c *Cache) Exists() bool {
	_, err := os.Stat(c.file)
	return err == nil
}

func (c *Cache) Age() time.Duration {
	info, err := os.Stat(c.file)
	if err != nil {
		return time.Hour * 24 * 365
	}
	return time.Since(info.ModTime())
}

// FindByName returns instances matching the exact name
func (c *Cache) FindByName(name string) (*Instance, error) {
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

// FindByPattern returns instances matching name, IP, ID, or tags (case-insensitive)
// Supports:
// - Simple substring: "prod"
// - Multi-keyword AND: "prod web" (space-separated, all must match)
// - Regex: "/prod-web-\d+/"
// - Tag: "tag:env=prod"
func (c *Cache) FindByPattern(pattern string) ([]Instance, error) {
	instances, err := c.Load()
	if err != nil {
		return nil, err
	}
	return FilterInstances(instances, pattern), nil
}

// FindByTag returns instances that have specific tag key=value
func (c *Cache) FindByTag(key, value string) ([]Instance, error) {
	instances, err := c.Load()
	if err != nil {
		return nil, err
	}
	var result []Instance
	key = strings.ToLower(key)
	value = strings.ToLower(value)
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
func FilterInstances(instances []Instance, pattern string) []Instance {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return instances
	}

	// Tag filter: "tag:key=value"
	if strings.HasPrefix(pattern, "tag:") {
		tagExpr := pattern[4:]
		parts := strings.SplitN(tagExpr, "=", 2)
		key := strings.ToLower(parts[0])
		value := ""
		if len(parts) == 2 {
			value = strings.ToLower(parts[1])
		}
		var result []Instance
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

	// Regex: "/pattern/"
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) > 2 {
		re, err := regexp.Compile("(?i)" + pattern[1:len(pattern)-1])
		if err == nil {
			var result []Instance
			for _, inst := range instances {
				searchStr := inst.Name + " " + inst.PrivateIP + " " + inst.PublicIP + " " + inst.EIP + " " + inst.ID
				if re.MatchString(searchStr) {
					result = append(result, inst)
				}
			}
			return result
		}
		// Fall through to normal search if regex is invalid
	}

	// Multi-keyword AND: "prod web" → must contain both "prod" and "web"
	keywords := strings.Fields(strings.ToLower(pattern))
	var result []Instance
	for _, inst := range instances {
		searchStr := strings.ToLower(inst.Name + " " + inst.PrivateIP + " " + inst.PublicIP + " " + inst.EIP + " " + inst.ID)
		// Also include tags in search
		for k, v := range inst.Tags {
			searchStr += " " + strings.ToLower(k) + "=" + strings.ToLower(v)
		}

		allMatch := true
		for _, kw := range keywords {
			if !strings.Contains(searchStr, kw) {
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

// HistoryDir returns the path for history storage
func (c *Cache) HistoryDir() string {
	return c.dir
}
