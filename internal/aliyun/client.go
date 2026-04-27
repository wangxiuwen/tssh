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
	StopInvocation(req *ecs.StopInvocationRequest) (*ecs.StopInvocationResponse, error)
}

// TimeoutError is returned by RunCommand when the local poll deadline
// is reached while the Cloud Assistant invocation is still running.
// The InvokeID can be used with FetchInvocation to retrieve the output later.
type TimeoutError struct {
	InvokeID   string
	InstanceID string
	TimeoutSec int
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("command timed out after %ds (invoke_id=%s, 用 `tssh exec --fetch %s` 取结果, `tssh exec --stop %s` 中止)",
		e.TimeoutSec, e.InvokeID, e.InvokeID, e.InvokeID)
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

// ecsClientFactory is the function used to create SDK clients — overridable in tests.
// 当 securityToken 非空时走 STS (CloudSSO / RamRoleArn 等) 路径, 否则用静态 AK.
var ecsClientFactory = func(region, accessKeyID, accessKeySecret, securityToken string) (ecsAPI, error) {
	if securityToken != "" {
		return ecs.NewClientWithStsToken(region, accessKeyID, accessKeySecret, securityToken)
	}
	return ecs.NewClientWithAccessKey(region, accessKeyID, accessKeySecret)
}

// NewClient creates a new Aliyun ECS client from config
func NewClient(cfg *model.Config) (*Client, error) {
	api, err := ecsClientFactory(cfg.Region, cfg.AccessKeyID, cfg.AccessKeySecret, cfg.SecurityToken)
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
		// Belt-and-suspenders: bail when a page yields no new rows. Otherwise a
		// transient empty response from the API while TotalCount > len(all)
		// would spin forever.
		if len(resp.Instances.Instance) == 0 || len(all) >= resp.TotalCount {
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

// SubmitCommand submits a command for asynchronous execution via Cloud Assistant
// and returns the InvokeID immediately. The command keeps running on the remote
// until it finishes or StopInvocation is called — caller is not blocked.
func (a *Client) SubmitCommand(instanceID, command string, timeoutSec int) (string, error) {
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
			return "", fmt.Errorf("RunCommand failed: %w", err)
		}
		a.sleep(time.Duration(1<<uint(retry)) * time.Second)
	}
	if err != nil {
		return "", fmt.Errorf("RunCommand failed (throttled): %w", err)
	}
	return resp.InvokeId, nil
}

// RunCommand submits a command and blocks until it finishes or timeoutSec is reached.
// On timeout it returns a *TimeoutError carrying the InvokeID so the caller can
// resume with FetchInvocation later.
func (a *Client) RunCommand(instanceID, command string, timeoutSec int) (*model.CommandResult, error) {
	// Normalize before splitting into submit+wait. Otherwise waitForResult would
	// get the raw 0 and abort in ~10s while Cloud Assistant is happy with the
	// default 60s — a source of "--timeout ignored" reports.
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	invokeID, err := a.SubmitCommand(instanceID, command, timeoutSec)
	if err != nil {
		return nil, err
	}
	return a.waitForResult(invokeID, instanceID, timeoutSec)
}

// FetchInvocation returns a single-shot snapshot of an invocation.
// InstanceID is optional; when empty the first entry in the response is used.
// For Running/Pending invocations Output may be partial or empty.
func (a *Client) FetchInvocation(invokeID, instanceID string) (*model.InvocationStatus, error) {
	if invokeID == "" {
		return nil, fmt.Errorf("invokeID 不能为空")
	}
	a.rateLimit()
	req := ecs.CreateDescribeInvocationResultsRequest()
	req.RegionId = a.region
	req.InvokeId = invokeID
	if instanceID != "" {
		req.InstanceId = instanceID
	}
	resp, err := a.api.DescribeInvocationResults(req)
	if err != nil {
		return nil, fmt.Errorf("DescribeInvocationResults: %w", err)
	}
	if len(resp.Invocation.InvocationResults.InvocationResult) == 0 {
		return nil, fmt.Errorf("invocation %s 未找到结果 (实例是否已重启/停止?)", invokeID)
	}
	r := resp.Invocation.InvocationResults.InvocationResult[0]
	return &model.InvocationStatus{
		InvokeID:     r.InvokeId,
		InstanceID:   r.InstanceId,
		Status:       r.InvocationStatus,
		Output:       r.Output,
		ExitCode:     int(r.ExitCode),
		ErrorCode:    r.ErrorCode,
		ErrorInfo:    r.ErrorInfo,
		StartTime:    r.StartTime,
		FinishedTime: r.FinishedTime,
	}, nil
}

// StopInvocation cancels one or more running invocations. If instanceIDs is empty,
// all instances bound to the InvokeID are affected.
func (a *Client) StopInvocation(invokeID string, instanceIDs []string) error {
	if invokeID == "" {
		return fmt.Errorf("invokeID 不能为空")
	}
	a.rateLimit()
	req := ecs.CreateStopInvocationRequest()
	req.RegionId = a.region
	req.InvokeId = invokeID
	if len(instanceIDs) > 0 {
		req.InstanceId = &instanceIDs
	}
	if _, err := a.api.StopInvocation(req); err != nil {
		return fmt.Errorf("StopInvocation: %w", err)
	}
	return nil
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
	return nil, &TimeoutError{InvokeID: invokeID, InstanceID: instanceID, TimeoutSec: timeoutSec}
}

// SendFile uploads a file to an instance via Cloud Assistant
func (a *Client) SendFile(instanceID, localPath, remotePath, fileName string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	return a.SendFileContent(instanceID, data, remotePath, fileName)
}

// SendFileContent uploads raw bytes to an instance via Cloud Assistant.
// Single SendFile call — Cloud Assistant limits Content (base64) to ~32KB,
// so callers must chunk anything larger.
func (a *Client) SendFileContent(instanceID string, data []byte, remotePath, fileName string) error {
	content := base64.StdEncoding.EncodeToString(data)

	req := ecs.CreateSendFileRequest()
	req.RegionId = a.region
	req.InstanceId = &[]string{instanceID}
	req.Name = fileName
	req.TargetDir = remotePath
	req.Content = content
	req.ContentType = "Base64"
	req.Overwrite = "true"
	_, err := a.api.SendFile(req)
	if err != nil {
		return fmt.Errorf("SendFile failed: %w", err)
	}
	return nil
}
