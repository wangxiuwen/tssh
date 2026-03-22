package aliyun

import (
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/wangxiuwen/tssh/internal/model"
)

// ecsAPI defines the subset of ECS SDK methods we use — enables mocking in tests
type ecsAPI interface {
	DescribeInstances(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error)
	RunCommand(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error)
	DescribeInvocationResults(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error)
	StartTerminalSession(req *ecs.StartTerminalSessionRequest) (*ecs.StartTerminalSessionResponse, error)
	SendFile(req *ecs.SendFileRequest) (*ecs.SendFileResponse, error)
	StopInstance(req *ecs.StopInstanceRequest) (*ecs.StopInstanceResponse, error)
	StartInstance(req *ecs.StartInstanceRequest) (*ecs.StartInstanceResponse, error)
	RebootInstance(req *ecs.RebootInstanceRequest) (*ecs.RebootInstanceResponse, error)
}

// Client wraps the ECS client with rate limiting
type Client struct {
	api     ecsAPI
	region  string
	mu      sync.Mutex
	lastReq time.Time
	sleepFn func(time.Duration) // injectable for testing
}

const apiMinInterval = 100 * time.Millisecond

func (a *Client) sleep(d time.Duration) {
	if a.sleepFn != nil {
		a.sleepFn(d)
	} else {
		time.Sleep(d)
	}
}

func (a *Client) rateLimit() {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if elapsed := now.Sub(a.lastReq); elapsed < apiMinInterval {
		a.sleep(apiMinInterval - elapsed)
	}
	a.lastReq = time.Now()
}

func isThrottled(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Throttling") || strings.Contains(s, "throttling") ||
		strings.Contains(s, "TooManyRequests") || strings.Contains(s, "ServiceUnavailable")
}

// ecsClientFactory is the function used to create SDK clients — overridable in tests
var ecsClientFactory = func(region, accessKeyID, accessKeySecret string) (ecsAPI, error) {
	return ecs.NewClientWithAccessKey(region, accessKeyID, accessKeySecret)
}

// NewClient creates a new Aliyun ECS client from config
func NewClient(cfg *model.Config) (*Client, error) {
	api, err := ecsClientFactory(cfg.Region, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create ECS client: %w", err)
	}
	return &Client{api: api, region: cfg.Region}, nil
}

// FetchAllInstances retrieves all ECS instances with pagination
func (a *Client) FetchAllInstances() ([]model.Instance, error) {
	var all []model.Instance
	page := 1
	for {
		req := ecs.CreateDescribeInstancesRequest()
		req.RegionId = a.region
		req.PageSize = requests.NewInteger(100)
		req.PageNumber = requests.NewInteger(page)

		resp, err := a.api.DescribeInstances(req)
		if err != nil {
			return nil, fmt.Errorf("DescribeInstances failed: %w", err)
		}
		for _, inst := range resp.Instances.Instance {
			var privateIP, publicIP, eip string
			if len(inst.VpcAttributes.PrivateIpAddress.IpAddress) > 0 {
				privateIP = inst.VpcAttributes.PrivateIpAddress.IpAddress[0]
			}
			if len(inst.PublicIpAddress.IpAddress) > 0 {
				publicIP = inst.PublicIpAddress.IpAddress[0]
			}
			if inst.EipAddress.IpAddress != "" {
				eip = inst.EipAddress.IpAddress
			}
			tags := make(map[string]string)
			for _, tag := range inst.Tags.Tag {
				if tag.TagKey != "" {
					tags[tag.TagKey] = tag.TagValue
				}
			}
			all = append(all, model.Instance{
				ID: inst.InstanceId, Name: inst.InstanceName, Status: inst.Status,
				PrivateIP: privateIP, PublicIP: publicIP, EIP: eip,
				Region: inst.RegionId, Zone: inst.ZoneId, VpcID: inst.VpcAttributes.VpcId, Tags: tags,
			})
		}
		if len(all) >= resp.TotalCount {
			break
		}
		page++
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, nil
}

// GetInstanceDetail fetches detailed info for a single instance
func (a *Client) GetInstanceDetail(instanceID string) (*model.InstanceDetail, error) {
	a.rateLimit()
	req := ecs.CreateDescribeInstancesRequest()
	req.RegionId = a.region
	req.InstanceIds = fmt.Sprintf("[\"%s\"]", instanceID)
	resp, err := a.api.DescribeInstances(req)
	if err != nil {
		return nil, err
	}
	if len(resp.Instances.Instance) == 0 {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}
	inst := resp.Instances.Instance[0]
	return &model.InstanceDetail{
		InstanceType: inst.InstanceType, CPU: inst.Cpu, Memory: inst.Memory,
		OSName: inst.OSName, CreationTime: inst.CreationTime, ExpiredTime: inst.ExpiredTime,
		ChargeType: inst.InstanceChargeType, VpcID: inst.VpcAttributes.VpcId,
		SecurityGroupIDs: inst.SecurityGroupIds.SecurityGroupId,
	}, nil
}

// StopInstance stops an ECS instance
func (a *Client) StopInstance(instanceID string) error {
	a.rateLimit()
	req := ecs.CreateStopInstanceRequest()
	req.RegionId = a.region
	req.InstanceId = instanceID
	req.ForceStop = requests.NewBoolean(false)
	_, err := a.api.StopInstance(req)
	return err
}

// StartInstance starts an ECS instance
func (a *Client) StartInstance(instanceID string) error {
	a.rateLimit()
	req := ecs.CreateStartInstanceRequest()
	req.RegionId = a.region
	req.InstanceId = instanceID
	_, err := a.api.StartInstance(req)
	return err
}

// RebootInstance reboots an ECS instance
func (a *Client) RebootInstance(instanceID string) error {
	a.rateLimit()
	req := ecs.CreateRebootInstanceRequest()
	req.RegionId = a.region
	req.InstanceId = instanceID
	req.ForceStop = requests.NewBoolean(false)
	_, err := a.api.RebootInstance(req)
	return err
}

// FetchInstanceByID fetches a single instance by ID
func (a *Client) FetchInstanceByID(instanceID string) ([]model.Instance, error) {
	a.rateLimit()
	req := ecs.CreateDescribeInstancesRequest()
	req.RegionId = a.region
	req.InstanceIds = fmt.Sprintf("[\"%s\"]", instanceID)
	resp, err := a.api.DescribeInstances(req)
	if err != nil {
		return nil, err
	}
	var result []model.Instance
	for _, inst := range resp.Instances.Instance {
		var privateIP string
		if len(inst.VpcAttributes.PrivateIpAddress.IpAddress) > 0 {
			privateIP = inst.VpcAttributes.PrivateIpAddress.IpAddress[0]
		}
		result = append(result, model.Instance{
			ID: inst.InstanceId, Name: inst.InstanceName, Status: inst.Status,
			PrivateIP: privateIP, Region: inst.RegionId,
		})
	}
	return result, nil
}

// StartSession creates a terminal session
func (a *Client) StartSession(instanceID string) (wsURL, sessionID, token string, err error) {
	req := ecs.CreateStartTerminalSessionRequest()
	req.RegionId = a.region
	req.InstanceId = &[]string{instanceID}
	resp, err := a.api.StartTerminalSession(req)
	if err != nil {
		return "", "", "", fmt.Errorf("StartTerminalSession failed: %w", err)
	}
	return resp.WebSocketUrl, resp.SessionId, resp.SecurityToken, nil
}

// StartPortForwardSession creates a port forwarding session
func (a *Client) StartPortForwardSession(instanceID string, port int) (wsURL, sessionID, token string, err error) {
	req := ecs.CreateStartTerminalSessionRequest()
	req.RegionId = a.region
	req.InstanceId = &[]string{instanceID}
	req.PortNumber = requests.NewInteger(port)
	resp, err := a.api.StartTerminalSession(req)
	if err != nil {
		return "", "", "", fmt.Errorf("StartTerminalSession (port %d) failed: %w", port, err)
	}
	return resp.WebSocketUrl, resp.SessionId, resp.SecurityToken, nil
}

// longCommandThreshold is the byte limit above which commands are sent via
// a base64-encoded wrapper script to avoid Cloud Assistant API URL limits.
const longCommandThreshold = 2048

// wrapCommand wraps a command for execution on the remote host.
// All commands are base64-encoded to avoid shell quoting/escaping issues
// (fixes complex commands with semicolons, pipes, quotes etc.).
// COLUMNS=32767 prevents PTY width truncation of output.
func wrapCommand(command string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(command))
	return fmt.Sprintf("export COLUMNS=32767; eval \"$(echo '%s' | base64 -d)\"", encoded)
}

// RunCommand executes a command on an instance with retry on throttling
func (a *Client) RunCommand(instanceID, command string, timeoutSec int) (*model.CommandResult, error) {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	wrapped := wrapCommand(command)

	req := ecs.CreateRunCommandRequest()
	req.RegionId = a.region
	req.Type = "RunShellScript"
	req.CommandContent = base64.StdEncoding.EncodeToString([]byte(wrapped))
	req.ContentEncoding = "Base64"
	req.InstanceId = &[]string{instanceID}
	req.Timeout = requests.NewInteger(timeoutSec)

	var resp *ecs.RunCommandResponse
	var err error
	for retry := 0; retry < 5; retry++ {
		a.rateLimit()
		resp, err = a.api.RunCommand(req)
		if err == nil {
			break
		}
		if !isThrottled(err) {
			return nil, fmt.Errorf("RunCommand failed: %w", err)
		}
		a.sleep(time.Duration(1<<uint(retry)) * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("RunCommand failed (throttled): %w", err)
	}
	return a.waitForResult(resp.InvokeId, instanceID, timeoutSec)
}

func (a *Client) waitForResult(invokeID, instanceID string, timeoutSec int) (*model.CommandResult, error) {
	deadline := time.Now().Add(time.Duration(timeoutSec+10) * time.Second)
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		delay := time.Duration(float64(500*time.Millisecond) * math.Pow(2.0, float64(attempt)))
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		a.sleep(delay)

		a.rateLimit()
		req := ecs.CreateDescribeInvocationResultsRequest()
		req.RegionId = a.region
		req.InvokeId = invokeID
		req.InstanceId = instanceID
		resp, err := a.api.DescribeInvocationResults(req)
		if err != nil {
			if isThrottled(err) {
					a.sleep(2 * time.Second)
				continue
			}
			return nil, err
		}
		for _, result := range resp.Invocation.InvocationResults.InvocationResult {
			switch result.InvocationStatus {
			case "Success", "Finished":
				return &model.CommandResult{Output: result.Output, ExitCode: int(result.ExitCode)}, nil
			case "Failed":
				r := &model.CommandResult{Output: result.Output, ExitCode: int(result.ExitCode)}
				if result.ErrorCode != "" && result.ExitCode == 0 {
					return r, fmt.Errorf("command failed: %s: %s", result.ErrorCode, result.ErrorInfo)
				}
				return r, nil
			case "Stopped":
				return nil, fmt.Errorf("command stopped")
			}
		}
	}
	return nil, fmt.Errorf("command timed out after %ds", timeoutSec)
}

// SendFile uploads a file to an instance via Cloud Assistant
func (a *Client) SendFile(instanceID, localPath, remotePath, fileName string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	content := base64.StdEncoding.EncodeToString(data)

	req := ecs.CreateSendFileRequest()
	req.RegionId = a.region
	req.InstanceId = &[]string{instanceID}
	req.Name = fileName
	req.TargetDir = remotePath
	req.Content = content
	req.ContentType = "Base64"
	req.Overwrite = "true"
	_, err = a.api.SendFile(req)
	if err != nil {
		return fmt.Errorf("SendFile failed: %w", err)
	}
	return nil
}
