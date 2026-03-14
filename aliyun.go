package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
)

// AliyunClient wraps the ECS client
type AliyunClient struct {
	client *ecs.Client
	region string
}

// NewAliyunClient creates a new client from ~/.aliyun/config.json
func NewAliyunClient(config *Config) (*AliyunClient, error) {
	client, err := ecs.NewClientWithAccessKey(config.Region, config.AccessKeyID, config.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create ECS client: %w", err)
	}
	return &AliyunClient{client: client, region: config.Region}, nil
}

// FetchAllInstances retrieves all ECS instances with pagination
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

			all = append(all, Instance{
				ID:        inst.InstanceId,
				Name:      inst.InstanceName,
				Status:    inst.Status,
				PrivateIP: privateIP,
				PublicIP:  publicIP,
				EIP:       eip,
				Region:    inst.RegionId,
				Zone:      inst.ZoneId,
			})
		}

		fmt.Printf("  📦 %d / %d instances...\n", len(all), resp.TotalCount)
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

// RunCommand executes a command on an instance and returns the output
func (a *AliyunClient) RunCommand(instanceID, command string) (string, error) {
	req := ecs.CreateRunCommandRequest()
	req.RegionId = a.region
	req.Type = "RunShellScript"
	req.CommandContent = command
	req.InstanceId = &[]string{instanceID}
	req.Timeout = requests.NewInteger(60)

	resp, err := a.client.RunCommand(req)
	if err != nil {
		return "", fmt.Errorf("RunCommand failed: %w", err)
	}

	// Wait for command to complete and get output
	return a.waitForCommandResult(resp.InvokeId, instanceID)
}

// waitForCommandResult polls for command completion
func (a *AliyunClient) waitForCommandResult(invokeID, instanceID string) (string, error) {
	// Initial wait for command to start
	sleepDuration(2)

	for i := 0; i < 60; i++ {
		req := ecs.CreateDescribeInvocationResultsRequest()
		req.RegionId = a.region
		req.InvokeId = invokeID
		req.InstanceId = instanceID

		resp, err := a.client.DescribeInvocationResults(req)
		if err != nil {
			return "", err
		}

		for _, result := range resp.Invocation.InvocationResults.InvocationResult {
			switch result.InvocationStatus {
			case "Success", "Finished":
				return result.Output, nil
			case "Failed":
				return "", fmt.Errorf("command failed (exit %d): %s", result.ExitCode, result.ErrorInfo)
			case "Stopped":
				return "", fmt.Errorf("command stopped")
			case "Running", "Pending", "Scheduled":
				// Still in progress, continue polling
			default:
				// Unknown status, continue polling
			}
			// Still running, continue polling
		}

		sleepDuration(1)
	}
	return "", fmt.Errorf("command timed out after 60s")
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

// Config holds Aliyun credentials
type Config struct {
	AccessKeyID     string
	AccessKeySecret string
	Region          string
}

// LoadConfig reads credentials, preferring env vars over config file
func LoadConfig() (*Config, error) {
	// 1. Try environment variables first
	akID := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	akSecret := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	region := os.Getenv("ALIBABA_CLOUD_REGION_ID")
	if region == "" {
		region = "cn-beijing"
	}

	if akID != "" && akSecret != "" {
		return &Config{
			AccessKeyID:     akID,
			AccessKeySecret: akSecret,
			Region:          region,
		}, nil
	}

	// 2. Fall back to ~/.aliyun/config.json (always use "default" profile)
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".aliyun", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("no AK in env vars and cannot read %s: %w", configPath, err)
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

	// Always use "default" profile, ignore "current"
	for _, p := range cfg.Profiles {
		if p.Name == "default" {
			r := p.RegionID
			if r == "" {
				r = "cn-beijing"
			}
			return &Config{
				AccessKeyID:     p.AccessKeyID,
				AccessKeySecret: p.AccessKeySecret,
				Region:          r,
			}, nil
		}
	}
	return nil, fmt.Errorf("no 'default' profile in config and no env vars set")
}
