package main

// Type aliases — bridge to internal packages so all cmd files work unchanged
import (
	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/cache"
	"github.com/wangxiuwen/tssh/internal/config"
	"github.com/wangxiuwen/tssh/internal/model"
)

// Type aliases for backward compatibility
type Instance = model.Instance
type Config = model.Config
type CommandResult = model.CommandResult
type InstanceDetail = model.InstanceDetail
type AliyunClient = aliyun.Client
type Cache = cache.Cache

// Function wrappers
func NewAliyunClient(cfg *Config) (*AliyunClient, error) { return aliyun.NewClient(cfg) }
func NewCache() *Cache                                    { return cache.New() }
func NewCacheWithProfile(profile string) *Cache           { return cache.NewWithProfile(profile) }
func LoadConfig() (*Config, error)                        { return config.Load("") }
func LoadConfigWithProfile(profile string) (*Config, error) { return config.Load(profile) }
func ListProfiles() []string                              { return config.ListProfiles() }
func FilterInstances(instances []Instance, pattern string) []Instance {
	return cache.FilterInstances(instances, pattern)
}
