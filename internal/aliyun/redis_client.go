package aliyun

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	r_kvstore "github.com/aliyun/alibaba-cloud-sdk-go/services/r-kvstore"
	"github.com/wangxiuwen/tssh/internal/model"
)

// redisAPI defines the subset of R-KVStore SDK methods we use — enables mocking in tests
type redisAPI interface {
	DescribeInstances(req *r_kvstore.DescribeInstancesRequest) (*r_kvstore.DescribeInstancesResponse, error)
}

// RedisClient wraps the R-KVStore client with rate limiting
type RedisClient struct {
	api     redisAPI
	region  string
	mu      sync.Mutex
	lastReq time.Time
	sleepFn func(time.Duration)
}

func (c *RedisClient) sleep(d time.Duration) {
	if c.sleepFn != nil {
		c.sleepFn(d)
	} else {
		time.Sleep(d)
	}
}

func (c *RedisClient) rateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if elapsed := now.Sub(c.lastReq); elapsed < apiMinInterval {
		c.sleep(apiMinInterval - elapsed)
	}
	c.lastReq = time.Now()
}

// redisClientFactory is overridable in tests
var redisClientFactory = func(region, accessKeyID, accessKeySecret, securityToken string) (redisAPI, error) {
	if securityToken != "" {
		return r_kvstore.NewClientWithStsToken(region, accessKeyID, accessKeySecret, securityToken)
	}
	return r_kvstore.NewClientWithAccessKey(region, accessKeyID, accessKeySecret)
}

// NewRedisClient creates a new Aliyun Redis (R-KVStore) client from config
func NewRedisClient(cfg *model.Config) (*RedisClient, error) {
	api, err := redisClientFactory(cfg.Region, cfg.AccessKeyID, cfg.AccessKeySecret, cfg.SecurityToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client: %w", err)
	}
	return &RedisClient{api: api, region: cfg.Region}, nil
}

// FetchAllRedisInstances retrieves all Redis instances with pagination
func (c *RedisClient) FetchAllRedisInstances() ([]model.RedisInstance, error) {
	var all []model.RedisInstance
	page := 1
	for {
		c.rateLimit()
		req := r_kvstore.CreateDescribeInstancesRequest()
		req.RegionId = c.region
		req.PageSize = requests.NewInteger(50)
		req.PageNumber = requests.NewInteger(page)

		resp, err := c.api.DescribeInstances(req)
		if err != nil {
			return nil, fmt.Errorf("Redis DescribeInstances failed: %w", err)
		}
		for _, inst := range resp.Instances.KVStoreInstance {
			all = append(all, model.RedisInstance{
				ID:               inst.InstanceId,
				Name:             inst.InstanceName,
				Status:           inst.InstanceStatus,
				InstanceClass:    inst.InstanceClass,
				InstanceType:     inst.InstanceType,
				EngineVersion:    inst.EngineVersion,
				ArchitectureType: inst.ArchitectureType,
				Capacity:         inst.Capacity,
				ConnectionDomain: inst.ConnectionDomain,
				Port:             inst.Port,
				PrivateIP:        inst.PrivateIp,
				VpcID:            inst.VpcId,
				NetworkType:      inst.NetworkType,
				RegionID:         inst.RegionId,
				ZoneID:           inst.ZoneId,
				ChargeType:       inst.ChargeType,
				CreateTime:       inst.CreateTime,
				EndTime:          inst.EndTime,
				Connections:      inst.Connections,
				Bandwidth:        inst.Bandwidth,
				QPS:              inst.QPS,
			})
		}
		if len(resp.Instances.KVStoreInstance) == 0 || len(all) >= resp.TotalCount {
			break
		}
		page++
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, nil
}
