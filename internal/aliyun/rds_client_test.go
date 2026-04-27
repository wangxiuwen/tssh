package aliyun

import (
	"fmt"
	"testing"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/wangxiuwen/tssh/internal/model"
)

// --- Mock RDS API ---

type mockRDS struct {
	describeDBInstancesFn func(req *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error)
}

func (m *mockRDS) DescribeDBInstances(req *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error) {
	if m.describeDBInstancesFn != nil {
		return m.describeDBInstancesFn(req)
	}
	return &rds.DescribeDBInstancesResponse{}, nil
}

func newTestRDSClient(mock *mockRDS) *RDSClient {
	return &RDSClient{api: mock, region: "cn-test", sleepFn: func(d time.Duration) {}}
}

// --- Tests ---

func TestFetchAllRDSInstances_SinglePage(t *testing.T) {
	mock := &mockRDS{
		describeDBInstancesFn: func(req *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error) {
			resp := &rds.DescribeDBInstancesResponse{}
			resp.TotalRecordCount = 2
			resp.Items.DBInstance = []rds.DBInstance{
				{
					DBInstanceId:          "rm-002",
					DBInstanceDescription: "prod-mysql",
					DBInstanceStatus:      "Running",
					Engine:                "MySQL",
					EngineVersion:         "8.0",
					DBInstanceClass:       "rds.mysql.s2.large",
					ConnectionString:      "rm-002.mysql.rds.aliyuncs.com",
					VpcId:                 "vpc-001",
					InstanceNetworkType:   "VPC",
					RegionId:              "cn-test",
					ZoneId:                "cn-test-a",
					PayType:               "Prepaid",
					CreateTime:            "2024-01-01T00:00:00Z",
					ExpireTime:            "2025-01-01T00:00:00Z",
					LockMode:              "Unlock",
					Category:              "HighAvailability",
				},
				{
					DBInstanceId:          "rm-001",
					DBInstanceDescription: "dev-mysql",
					DBInstanceStatus:      "Running",
					Engine:                "MySQL",
					EngineVersion:         "5.7",
					DBInstanceClass:       "rds.mysql.s1.small",
					ConnectionString:      "rm-001.mysql.rds.aliyuncs.com",
					RegionId:              "cn-test",
				},
			}
			return resp, nil
		},
	}

	c := newTestRDSClient(mock)
	instances, err := c.FetchAllRDSInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
	// Should be sorted by name
	if instances[0].Name != "dev-mysql" {
		t.Errorf("expected dev-mysql first, got %s", instances[0].Name)
	}
	if instances[1].Engine != "MySQL" {
		t.Errorf("expected MySQL engine, got %s", instances[1].Engine)
	}
	if instances[1].EngineVersion != "8.0" {
		t.Errorf("expected version 8.0, got %s", instances[1].EngineVersion)
	}
}

func TestFetchAllRDSInstances_NoDescription(t *testing.T) {
	mock := &mockRDS{
		describeDBInstancesFn: func(req *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error) {
			resp := &rds.DescribeDBInstancesResponse{}
			resp.TotalRecordCount = 1
			resp.Items.DBInstance = []rds.DBInstance{
				{
					DBInstanceId:     "rm-unnamed",
					DBInstanceStatus: "Running",
					Engine:           "MySQL",
				},
			}
			return resp, nil
		},
	}

	c := newTestRDSClient(mock)
	instances, err := c.FetchAllRDSInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Name should fall back to ID when Description is empty
	if instances[0].Name != "rm-unnamed" {
		t.Errorf("expected name=rm-unnamed, got %s", instances[0].Name)
	}
}

func TestFetchAllRDSInstances_MultiPage(t *testing.T) {
	callCount := 0
	mock := &mockRDS{
		describeDBInstancesFn: func(req *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error) {
			callCount++
			resp := &rds.DescribeDBInstancesResponse{}
			resp.TotalRecordCount = 2
			if callCount == 1 {
				resp.Items.DBInstance = []rds.DBInstance{
					{DBInstanceId: "rm-001", DBInstanceDescription: "a", DBInstanceStatus: "Running"},
				}
			} else {
				resp.Items.DBInstance = []rds.DBInstance{
					{DBInstanceId: "rm-002", DBInstanceDescription: "b", DBInstanceStatus: "Running"},
				}
			}
			return resp, nil
		},
	}

	c := newTestRDSClient(mock)
	instances, err := c.FetchAllRDSInstances()
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

func TestFetchAllRDSInstances_Error(t *testing.T) {
	mock := &mockRDS{
		describeDBInstancesFn: func(req *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	c := newTestRDSClient(mock)
	_, err := c.FetchAllRDSInstances()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRDSClient_InvalidCredentials(t *testing.T) {
	origFactory := rdsClientFactory
	defer func() { rdsClientFactory = origFactory }()
	rdsClientFactory = func(region, accessKeyID, accessKeySecret, securityToken string) (rdsAPI, error) {
		return nil, fmt.Errorf("invalid credentials")
	}

	_, err := NewRDSClient(&model.Config{Region: "cn-test"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRDSClient_Success(t *testing.T) {
	origFactory := rdsClientFactory
	defer func() { rdsClientFactory = origFactory }()
	rdsClientFactory = func(region, accessKeyID, accessKeySecret, securityToken string) (rdsAPI, error) {
		return &mockRDS{}, nil
	}

	client, err := NewRDSClient(&model.Config{Region: "cn-test", AccessKeyID: "ak", AccessKeySecret: "sk"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestRDSClient_RateLimit(t *testing.T) {
	c := &RDSClient{api: &mockRDS{}, region: "cn-test"}
	start := time.Now()
	c.rateLimit()
	c.rateLimit()
	elapsed := time.Since(start)
	if elapsed < apiMinInterval {
		t.Errorf("rate limit not enforced: elapsed %v < %v", elapsed, apiMinInterval)
	}
}
