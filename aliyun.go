package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
)

// AliyunClient wraps the ECS client with rate limiting
type AliyunClient struct {
	client  *ecs.Client
	region  string
	mu      sync.Mutex
	lastReq time.Time
}

// apiMinInterval is the minimum time between API calls (10 req/s)
const apiMinInterval = 100 * time.Millisecond

// rateLimit waits to ensure we don't exceed the API rate limit
func (a *AliyunClient) rateLimit() {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(a.lastReq)
	if elapsed < apiMinInterval {
		time.Sleep(apiMinInterval - elapsed)
	}
	a.lastReq = time.Now()
}

// isThrottled checks if an error is an API throttling error
func isThrottled(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Throttling") || strings.Contains(s, "throttling") ||
		strings.Contains(s, "TooManyRequests") || strings.Contains(s, "ServiceUnavailable")
}

// NewAliyunClient creates a new client from config
func NewAliyunClient(config *Config) (*AliyunClient, error) {
	client, err := ecs.NewClientWithAccessKey(config.Region, config.AccessKeyID, config.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create ECS client: %w", err)
	}
	return &AliyunClient{client: client, region: config.Region}, nil
}

// FetchAllInstances retrieves all ECS instances with pagination, including tags
func (a *AliyunClient) FetchAllInstances() ([]Instance, error) {
	var all []Instance
	pageSize := 100
	page := 1

	for {
		req := ecs.CreateDescribeInstancesRequest()
		req.RegionId = a.region
		req.PageSize = requests.NewInteger(pageSize)
		req.PageNumber = requests.NewInteger(page)

		resp, err := a.client.DescribeInstances(req)
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

			// Extract tags
			tags := make(map[string]string)
			for _, tag := range inst.Tags.Tag {
				if tag.TagKey != "" {
					tags[tag.TagKey] = tag.TagValue
				}
			}

			all = append(all, Instance{
				ID:        inst.InstanceId,
				Name:      inst.InstanceName,
				Status:    inst.Status,
				PrivateIP: privateIP,
				PublicIP:  publicIP,
				EIP:       eip,
				Region:    inst.RegionId,
				Zone:      inst.ZoneId,
				Tags:      tags,
			})
		}



		if len(all) >= resp.TotalCount {
			break
		}
		page++
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Name < all[j].Name
	})
	return all, nil
}

// InstanceDetail holds extended instance information
type InstanceDetail struct {
	InstanceType     string
	CPU              int
	Memory           int // in MB
	OSName           string
	CreationTime     string
	ExpiredTime      string
	ChargeType       string
	VpcID            string
	SecurityGroupIDs []string
}

// GetInstanceDetail fetches detailed information for a single instance
func (a *AliyunClient) GetInstanceDetail(instanceID string) (*InstanceDetail, error) {
	a.rateLimit()
	req := ecs.CreateDescribeInstancesRequest()
	req.RegionId = a.region
	req.InstanceIds = fmt.Sprintf("[\"%s\"]", instanceID)

	resp, err := a.client.DescribeInstances(req)
	if err != nil {
		return nil, fmt.Errorf("DescribeInstances failed: %w", err)
	}

	if len(resp.Instances.Instance) == 0 {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	inst := resp.Instances.Instance[0]
	return &InstanceDetail{
		InstanceType:     inst.InstanceType,
		CPU:              inst.Cpu,
		Memory:           inst.Memory,
		OSName:           inst.OSName,
		CreationTime:     inst.CreationTime,
		ExpiredTime:      inst.ExpiredTime,
		ChargeType:       inst.InstanceChargeType,
		VpcID:            inst.VpcAttributes.VpcId,
		SecurityGroupIDs: inst.SecurityGroupIds.SecurityGroupId,
	}, nil
}

// StartSession creates a terminal session and returns the WebSocket URL
func (a *AliyunClient) StartSession(instanceID string) (webSocketURL, sessionID, token string, err error) {
	req := ecs.CreateStartTerminalSessionRequest()
	req.RegionId = a.region
	req.InstanceId = &[]string{instanceID}

	resp, err := a.client.StartTerminalSession(req)
	if err != nil {
		return "", "", "", fmt.Errorf("StartTerminalSession failed: %w", err)
	}
	return resp.WebSocketUrl, resp.SessionId, resp.SecurityToken, nil
}

// StartPortForwardSession creates a port forwarding session
func (a *AliyunClient) StartPortForwardSession(instanceID string, port int) (webSocketURL, sessionID, token string, err error) {
	req := ecs.CreateStartTerminalSessionRequest()
	req.RegionId = a.region
	req.InstanceId = &[]string{instanceID}
	req.PortNumber = requests.NewInteger(port)

	resp, err := a.client.StartTerminalSession(req)
	if err != nil {
		return "", "", "", fmt.Errorf("StartTerminalSession (port %d) failed: %w", port, err)
	}
	return resp.WebSocketUrl, resp.SessionId, resp.SecurityToken, nil
}

// CommandResult holds the result of a remote command execution
type CommandResult struct {
	Output   string
	ExitCode int
}

// RunCommand executes a command on an instance and returns the result.
// Includes rate limiting and automatic retry on API throttling.
func (a *AliyunClient) RunCommand(instanceID, command string, timeoutSec int) (*CommandResult, error) {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}

	req := ecs.CreateRunCommandRequest()
	req.RegionId = a.region
	req.Type = "RunShellScript"
	req.CommandContent = base64.StdEncoding.EncodeToString([]byte(command))
	req.ContentEncoding = "Base64"
	req.InstanceId = &[]string{instanceID}
	req.Timeout = requests.NewInteger(timeoutSec)

	// Retry with backoff on throttling
	var resp *ecs.RunCommandResponse
	var err error
	for retry := 0; retry < 5; retry++ {
		a.rateLimit()
		resp, err = a.client.RunCommand(req)
		if err == nil {
			break
		}
		if !isThrottled(err) {
			return nil, fmt.Errorf("RunCommand failed: %w", err)
		}
		// Throttled — backoff and retry
		backoff := time.Duration(1<<uint(retry)) * time.Second // 1s, 2s, 4s, 8s, 16s
		time.Sleep(backoff)
	}
	if err != nil {
		return nil, fmt.Errorf("RunCommand failed (throttled): %w", err)
	}

	return a.waitForCommandResult(resp.InvokeId, instanceID, timeoutSec)
}

// waitForCommandResult polls for command completion with exponential backoff.
// Also rate-limits polling requests and retries on throttling.
func (a *AliyunClient) waitForCommandResult(invokeID, instanceID string, timeoutSec int) (*CommandResult, error) {
	// Exponential backoff: 500ms, 1s, 2s, 3s, cap at 5s
	const (
		initialDelay = 500 * time.Millisecond
		maxDelay     = 5 * time.Second
		backoffBase  = 2.0
	)

	deadline := time.Now().Add(time.Duration(timeoutSec+10) * time.Second)

	for attempt := 0; time.Now().Before(deadline); attempt++ {
		// Exponential backoff sleep
		delay := time.Duration(float64(initialDelay) * math.Pow(backoffBase, float64(attempt)))
		if delay > maxDelay {
			delay = maxDelay
		}
		time.Sleep(delay)

		a.rateLimit()
		req := ecs.CreateDescribeInvocationResultsRequest()
		req.RegionId = a.region
		req.InvokeId = invokeID
		req.InstanceId = instanceID

		resp, err := a.client.DescribeInvocationResults(req)
		if err != nil {
			if isThrottled(err) {
				// Throttled on poll — just wait longer and retry
				time.Sleep(2 * time.Second)
				continue
			}
			return nil, err
		}

		for _, result := range resp.Invocation.InvocationResults.InvocationResult {
			switch result.InvocationStatus {
			case "Success", "Finished":
				return &CommandResult{Output: result.Output, ExitCode: int(result.ExitCode)}, nil
			case "Failed":
				// Non-zero exit code is NOT an error — commands like grep return 1 normally.
				// Always include output. Only return error for infrastructure failures.
				r := &CommandResult{Output: result.Output, ExitCode: int(result.ExitCode)}
				if result.ErrorCode != "" && result.ExitCode == 0 {
					return r, fmt.Errorf("command failed: %s: %s", result.ErrorCode, result.ErrorInfo)
				}
				return r, nil
			case "Stopped":
				return nil, fmt.Errorf("command stopped")
			case "Running", "Pending", "Scheduled":
				// Still in progress
			}
		}
	}
	return nil, fmt.Errorf("command timed out after %ds", timeoutSec)
}

// SendFile uploads a file to an instance via Cloud Assistant
func (a *AliyunClient) SendFile(instanceID, localPath, remotePath, fileName string) error {
	content, err := readFileBase64(localPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	req := ecs.CreateSendFileRequest()
	req.RegionId = a.region
	req.InstanceId = &[]string{instanceID}
	req.Name = fileName
	req.TargetDir = remotePath
	req.Content = content
	req.ContentType = "Base64"
	req.Overwrite = "true"

	_, err = a.client.SendFile(req)
	if err != nil {
		return fmt.Errorf("SendFile failed: %w", err)
	}
	return nil
}

// Config holds Aliyun credentials and settings
type Config struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	Region          string `json:"region"`
	ProfileName     string `json:"name,omitempty"`
}

// TsshConfig is the tssh-specific config file structure (~/.tssh/config.json)
type TsshConfig struct {
	Default  string   `json:"default"`
	Profiles []Config `json:"profiles"`
}

// LoadConfig reads credentials with profile support.
// Priority: env vars → ~/.tssh/config.json → ~/.aliyun/config.json
func LoadConfig() (*Config, error) {
	return LoadConfigWithProfile("")
}

// LoadConfigWithProfile reads credentials for a specific profile.
func LoadConfigWithProfile(profile string) (*Config, error) {
	// 1. Try environment variables first (always highest priority)
	akID := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	akSecret := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	region := os.Getenv("ALIBABA_CLOUD_REGION_ID")
	if region == "" {
		region = "cn-beijing"
	}

	if akID != "" && akSecret != "" && profile == "" {
		return &Config{
			AccessKeyID:     akID,
			AccessKeySecret: akSecret,
			Region:          region,
			ProfileName:     "env",
		}, nil
	}

	// 2. Try ~/.tssh/config.json (tssh native multi-account config)
	home, _ := os.UserHomeDir()
	tsshConfigPath := filepath.Join(home, ".tssh", "config.json")
	if data, err := os.ReadFile(tsshConfigPath); err == nil {
		var cfg TsshConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			targetProfile := profile
			if targetProfile == "" {
				targetProfile = cfg.Default
			}
			if targetProfile == "" {
				targetProfile = "default"
			}
			for _, p := range cfg.Profiles {
				if p.ProfileName == targetProfile {
					if p.Region == "" {
						p.Region = "cn-beijing"
					}
					return &p, nil
				}
			}
			// If profile was explicitly requested but not found, error
			if profile != "" {
				return nil, fmt.Errorf("profile '%s' not found in %s", profile, tsshConfigPath)
			}
		}
	}

	// 3. Fall back to ~/.aliyun/config.json
	configPath := filepath.Join(home, ".aliyun", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("no credentials found: no env vars, no ~/.tssh/config.json, cannot read %s: %w", configPath, err)
	}

	var cfg struct {
		Profiles []struct {
			Name            string `json:"name"`
			AccessKeyID     string `json:"access_key_id"`
			AccessKeySecret string `json:"access_key_secret"`
			RegionID        string `json:"region_id"`
		} `json:"profiles"`
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	targetProfile := profile
	if targetProfile == "" {
		targetProfile = "default"
	}

	for _, p := range cfg.Profiles {
		if p.Name == targetProfile {
			r := p.RegionID
			if r == "" {
				r = "cn-beijing"
			}
			return &Config{
				AccessKeyID:     p.AccessKeyID,
				AccessKeySecret: p.AccessKeySecret,
				Region:          r,
				ProfileName:     p.Name,
			}, nil
		}
	}
	return nil, fmt.Errorf("profile '%s' not found in config", targetProfile)
}

// ListProfiles returns all available profile names
func ListProfiles() []string {
	var profiles []string

	// Check env
	if os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID") != "" {
		profiles = append(profiles, "env")
	}

	// Check ~/.tssh/config.json
	home, _ := os.UserHomeDir()
	tsshConfigPath := filepath.Join(home, ".tssh", "config.json")
	if data, err := os.ReadFile(tsshConfigPath); err == nil {
		var cfg TsshConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			for _, p := range cfg.Profiles {
				profiles = append(profiles, p.ProfileName)
			}
		}
	}

	// Check ~/.aliyun/config.json
	configPath := filepath.Join(home, ".aliyun", "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg struct {
			Profiles []struct {
				Name string `json:"name"`
			} `json:"profiles"`
		}
		if err := json.Unmarshal(data, &cfg); err == nil {
			for _, p := range cfg.Profiles {
				profiles = append(profiles, "aliyun:"+p.Name)
			}
		}
	}

	return profiles
}
