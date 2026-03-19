package aliyun

import (
	"fmt"
	"testing"
	"time"

	r_kvstore "github.com/aliyun/alibaba-cloud-sdk-go/services/r-kvstore"
	"github.com/wangxiuwen/tssh/internal/model"
)

// --- Mock Redis API ---

type mockRedis struct {
	describeInstancesFn func(req *r_kvstore.DescribeInstancesRequest) (*r_kvstore.DescribeInstancesResponse, error)
}

func (m *mockRedis) DescribeInstances(req *r_kvstore.DescribeInstancesRequest) (*r_kvstore.DescribeInstancesResponse, error) {
	if m.describeInstancesFn != nil {
		return m.describeInstancesFn(req)
	}
	return &r_kvstore.DescribeInstancesResponse{}, nil
}

func newTestRedisClient(mock *mockRedis) *RedisClient {
	return &RedisClient{api: mock, region: "cn-test", sleepFn: func(d time.Duration) {}}
}

// --- Tests ---

func TestFetchAllRedisInstances_SinglePage(t *testing.T) {
	mock := &mockRedis{
		describeInstancesFn: func(req *r_kvstore.DescribeInstancesRequest) (*r_kvstore.DescribeInstancesResponse, error) {
			resp := &r_kvstore.DescribeInstancesResponse{}
			resp.TotalCount = 2
			resp.Instances.KVStoreInstance = []r_kvstore.KVStoreInstance{
				{
					InstanceId:       "r-002",
					InstanceName:     "redis-prod",
					InstanceStatus:   "Normal",
					InstanceClass:    "redis.master.small.default",
					InstanceType:     "Redis",
					EngineVersion:    "6.0",
					ArchitectureType: "standard",
					Capacity:         1024,
					ConnectionDomain: "r-002.redis.rds.aliyuncs.com",
					Port:             6379,
					PrivateIp:        "10.0.0.2",
					VpcId:            "vpc-001",
					NetworkType:      "VPC",
					RegionId:         "cn-test",
					ZoneId:           "cn-test-a",
					ChargeType:       "PrePaid",
					CreateTime:       "2024-01-01T00:00:00Z",
					EndTime:          "2025-01-01T00:00:00Z",
					Connections:      10000,
					Bandwidth:        96,
					QPS:              100000,
				},
				{
					InstanceId:       "r-001",
					InstanceName:     "redis-dev",
					InstanceStatus:   "Normal",
					InstanceClass:    "redis.master.micro.default",
					InstanceType:     "Redis",
					EngineVersion:    "5.0",
					Capacity:         256,
					ConnectionDomain: "r-001.redis.rds.aliyuncs.com",
					Port:             6379,
					PrivateIp:        "10.0.0.1",
					RegionId:         "cn-test",
				},
			}
			return resp, nil
		},
	}

	c := newTestRedisClient(mock)
	instances, err := c.FetchAllRedisInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
	// Should be sorted by name
	if instances[0].Name != "redis-dev" {
		t.Errorf("expected redis-dev first, got %s", instances[0].Name)
	}
	if instances[1].ConnectionDomain != "r-002.redis.rds.aliyuncs.com" {
		t.Errorf("expected connection domain, got %s", instances[1].ConnectionDomain)
	}
	if instances[1].Capacity != 1024 {
		t.Errorf("expected capacity 1024, got %d", instances[1].Capacity)
	}
	if instances[1].QPS != 100000 {
		t.Errorf("expected QPS 100000, got %d", instances[1].QPS)
	}
}

func TestFetchAllRedisInstances_MultiPage(t *testing.T) {
	callCount := 0
	mock := &mockRedis{
		describeInstancesFn: func(req *r_kvstore.DescribeInstancesRequest) (*r_kvstore.DescribeInstancesResponse, error) {
			callCount++
			resp := &r_kvstore.DescribeInstancesResponse{}
			resp.TotalCount = 2
			if callCount == 1 {
				resp.Instances.KVStoreInstance = []r_kvstore.KVStoreInstance{
					{InstanceId: "r-001", InstanceName: "a"},
				}
			} else {
				resp.Instances.KVStoreInstance = []r_kvstore.KVStoreInstance{
					{InstanceId: "r-002", InstanceName: "b"},
				}
			}
			return resp, nil
		},
	}

	c := newTestRedisClient(mock)
	instances, err := c.FetchAllRedisInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (pagination), got %d", callCount)
	}
}

func TestFetchAllRedisInstances_Error(t *testing.T) {
	mock := &mockRedis{
		describeInstancesFn: func(req *r_kvstore.DescribeInstancesRequest) (*r_kvstore.DescribeInstancesResponse, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	c := newTestRedisClient(mock)
	_, err := c.FetchAllRedisInstances()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRedisClient_InvalidCredentials(t *testing.T) {
	// NewRedisClient with empty credentials — test it doesn't panic
	origFactory := redisClientFactory
	defer func() { redisClientFactory = origFactory }()
	redisClientFactory = func(region, accessKeyID, accessKeySecret string) (redisAPI, error) {
		return nil, fmt.Errorf("invalid credentials")
	}

	_, err := NewRedisClient(&model.Config{Region: "cn-test"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRedisClient_Success(t *testing.T) {
	origFactory := redisClientFactory
	defer func() { redisClientFactory = origFactory }()
	redisClientFactory = func(region, accessKeyID, accessKeySecret string) (redisAPI, error) {
		return &mockRedis{}, nil
	}

	client, err := NewRedisClient(&model.Config{Region: "cn-test", AccessKeyID: "ak", AccessKeySecret: "sk"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestRedisClient_RateLimit(t *testing.T) {
	c := &RedisClient{api: &mockRedis{}, region: "cn-test"}
	start := time.Now()
	c.rateLimit()
	c.rateLimit()
	elapsed := time.Since(start)
	if elapsed < apiMinInterval {
		t.Errorf("rate limit not enforced: elapsed %v < %v", elapsed, apiMinInterval)
	}
}
