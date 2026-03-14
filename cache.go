package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Instance represents an Alibaba Cloud ECS instance
type Instance struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	PrivateIP string `json:"private_ip"`
	PublicIP  string `json:"public_ip"`
	EIP       string `json:"eip"`
	Region    string `json:"region"`
	Zone      string `json:"zone"`
}

// Cache manages local instance cache
type Cache struct {
	dir  string
	file string
}

func NewCache() *Cache {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".cache", "tssh")
	return &Cache{
		dir:  dir,
		file: filepath.Join(dir, "instances.json"),
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

// FindByPattern returns instances matching name, IP, or ID (case-insensitive)
func (c *Cache) FindByPattern(pattern string) ([]Instance, error) {
	instances, err := c.Load()
	if err != nil {
		return nil, err
	}
	pattern = strings.ToLower(pattern)
	var result []Instance
	for _, inst := range instances {
		if strings.Contains(strings.ToLower(inst.Name), pattern) ||
			strings.Contains(inst.PrivateIP, pattern) ||
			strings.Contains(inst.PublicIP, pattern) ||
			strings.Contains(inst.EIP, pattern) ||
			strings.Contains(strings.ToLower(inst.ID), pattern) {
			result = append(result, inst)
		}
	}
	return result, nil
}
