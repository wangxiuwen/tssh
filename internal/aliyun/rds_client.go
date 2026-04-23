package aliyun

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/wangxiuwen/tssh/internal/model"
)

// rdsAPI defines the subset of RDS SDK methods we use — enables mocking in tests
type rdsAPI interface {
	DescribeDBInstances(req *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error)
}

// RDSClient wraps the RDS client with rate limiting
type RDSClient struct {
	api     rdsAPI
	region  string
	mu      sync.Mutex
	lastReq time.Time
	sleepFn func(time.Duration)
}

func (c *RDSClient) sleep(d time.Duration) {
	if c.sleepFn != nil {
		c.sleepFn(d)
	} else {
		time.Sleep(d)
	}
}

func (c *RDSClient) rateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if elapsed := now.Sub(c.lastReq); elapsed < apiMinInterval {
		c.sleep(apiMinInterval - elapsed)
	}
	c.lastReq = time.Now()
}

// rdsClientFactory is overridable in tests
var rdsClientFactory = func(region, accessKeyID, accessKeySecret string) (rdsAPI, error) {
	return rds.NewClientWithAccessKey(region, accessKeyID, accessKeySecret)
}

// NewRDSClient creates a new Aliyun RDS client from config
func NewRDSClient(cfg *model.Config) (*RDSClient, error) {
	api, err := rdsClientFactory(cfg.Region, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create RDS client: %w", err)
	}
	return &RDSClient{api: api, region: cfg.Region}, nil
}

// FetchAllRDSInstances retrieves all RDS instances with pagination
func (c *RDSClient) FetchAllRDSInstances() ([]model.RDSInstance, error) {
	var all []model.RDSInstance
	page := 1
	for {
		c.rateLimit()
		req := rds.CreateDescribeDBInstancesRequest()
		req.RegionId = c.region
		req.PageSize = requests.NewInteger(100)
		req.PageNumber = requests.NewInteger(page)

		resp, err := c.api.DescribeDBInstances(req)
		if err != nil {
			return nil, fmt.Errorf("RDS DescribeDBInstances failed: %w", err)
		}
		for _, inst := range resp.Items.DBInstance {
			name := inst.DBInstanceDescription
			if name == "" {
				name = inst.DBInstanceId
			}
			all = append(all, model.RDSInstance{
				ID:               inst.DBInstanceId,
				Name:             name,
				Status:           inst.DBInstanceStatus,
				Engine:           inst.Engine,
				EngineVersion:    inst.EngineVersion,
				InstanceClass:    inst.DBInstanceClass,
				ConnectionString: inst.ConnectionString,
				VpcID:            inst.VpcId,
				NetworkType:      inst.InstanceNetworkType,
				RegionID:         inst.RegionId,
				ZoneID:           inst.ZoneId,
				PayType:          inst.PayType,
				CreateTime:       inst.CreateTime,
				ExpireTime:       inst.ExpireTime,
				LockMode:         inst.LockMode,
				Category:         inst.Category,
			})
		}
		if len(resp.Items.DBInstance) == 0 || len(all) >= resp.TotalRecordCount {
			break
		}
		page++
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, nil
}
