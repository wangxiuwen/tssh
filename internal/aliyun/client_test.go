package aliyun

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/wangxiuwen/tssh/internal/model"
)

// --- Mock ECS API ---

type mockECS struct {
	describeInstancesFn         func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error)
	runCommandFn                func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error)
	describeInvocationResultsFn func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error)
	startTerminalSessionFn      func(req *ecs.StartTerminalSessionRequest) (*ecs.StartTerminalSessionResponse, error)
	sendFileFn                  func(req *ecs.SendFileRequest) (*ecs.SendFileResponse, error)
	stopInstanceFn              func(req *ecs.StopInstanceRequest) (*ecs.StopInstanceResponse, error)
	startInstanceFn             func(req *ecs.StartInstanceRequest) (*ecs.StartInstanceResponse, error)
	rebootInstanceFn            func(req *ecs.RebootInstanceRequest) (*ecs.RebootInstanceResponse, error)
}

func (m *mockECS) DescribeInstances(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
	if m.describeInstancesFn != nil {
		return m.describeInstancesFn(req)
	}
	return &ecs.DescribeInstancesResponse{}, nil
}
func (m *mockECS) RunCommand(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
	if m.runCommandFn != nil {
		return m.runCommandFn(req)
	}
	return &ecs.RunCommandResponse{}, nil
}
func (m *mockECS) DescribeInvocationResults(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
	if m.describeInvocationResultsFn != nil {
		return m.describeInvocationResultsFn(req)
	}
	return &ecs.DescribeInvocationResultsResponse{}, nil
}
func (m *mockECS) StartTerminalSession(req *ecs.StartTerminalSessionRequest) (*ecs.StartTerminalSessionResponse, error) {
	if m.startTerminalSessionFn != nil {
		return m.startTerminalSessionFn(req)
	}
	return &ecs.StartTerminalSessionResponse{}, nil
}
func (m *mockECS) SendFile(req *ecs.SendFileRequest) (*ecs.SendFileResponse, error) {
	if m.sendFileFn != nil {
		return m.sendFileFn(req)
	}
	return &ecs.SendFileResponse{}, nil
}
func (m *mockECS) StopInstance(req *ecs.StopInstanceRequest) (*ecs.StopInstanceResponse, error) {
	if m.stopInstanceFn != nil {
		return m.stopInstanceFn(req)
	}
	return &ecs.StopInstanceResponse{}, nil
}
func (m *mockECS) StartInstance(req *ecs.StartInstanceRequest) (*ecs.StartInstanceResponse, error) {
	if m.startInstanceFn != nil {
		return m.startInstanceFn(req)
	}
	return &ecs.StartInstanceResponse{}, nil
}
func (m *mockECS) RebootInstance(req *ecs.RebootInstanceRequest) (*ecs.RebootInstanceResponse, error) {
	if m.rebootInstanceFn != nil {
		return m.rebootInstanceFn(req)
	}
	return &ecs.RebootInstanceResponse{}, nil
}

func newTestClient(mock *mockECS) *Client {
	return &Client{api: mock, region: "cn-test", sleepFn: func(d time.Duration) {}}
}

// --- Tests ---

func TestIsThrottled(t *testing.T) {
	cases := []struct {
		err    error
		expect bool
	}{
		{nil, false},
		{fmt.Errorf("some error"), false},
		{fmt.Errorf("Throttling.User"), true},
		{fmt.Errorf("throttling rate"), true},
		{fmt.Errorf("TooManyRequests"), true},
		{fmt.Errorf("ServiceUnavailable"), true},
	}
	for _, c := range cases {
		if got := isThrottled(c.err); got != c.expect {
			t.Errorf("isThrottled(%v) = %v, want %v", c.err, got, c.expect)
		}
	}
}

func TestRateLimit(t *testing.T) {
	// Use real time.Sleep (sleepFn=nil) to test actual rate limiting + cover sleep() else branch
	c := &Client{api: &mockECS{}, region: "cn-test"}
	start := time.Now()
	c.rateLimit()
	c.rateLimit()
	elapsed := time.Since(start)
	if elapsed < apiMinInterval {
		t.Errorf("rate limit not enforced: elapsed %v < %v", elapsed, apiMinInterval)
	}
}

func TestFetchAllInstances_SinglePage(t *testing.T) {
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			resp := &ecs.DescribeInstancesResponse{}
			resp.TotalCount = 2
			resp.Instances.Instance = []ecs.Instance{
				{
					InstanceId:   "i-002",
					InstanceName: "web-02",
					Status:       "Running",
					RegionId:     "cn-test",
					ZoneId:       "cn-test-a",
					VpcAttributes: ecs.VpcAttributes{
						PrivateIpAddress: ecs.PrivateIpAddressInDescribeInstanceAttribute{IpAddress: []string{"10.0.0.2"}},
					},
					Tags: ecs.TagsInDescribeInstances{Tag: []ecs.Tag{{TagKey: "env", TagValue: "prod"}}},
				},
				{
					InstanceId:   "i-001",
					InstanceName: "web-01",
					Status:       "Stopped",
					RegionId:     "cn-test",
					VpcAttributes: ecs.VpcAttributes{
						PrivateIpAddress: ecs.PrivateIpAddressInDescribeInstanceAttribute{IpAddress: []string{"10.0.0.1"}},
					},
					PublicIpAddress: ecs.PublicIpAddressInDescribeInstances{IpAddress: []string{"1.2.3.4"}},
					EipAddress:      ecs.EipAddressInDescribeInstances{IpAddress: "5.6.7.8"},
				},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	instances, err := c.FetchAllInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
	// Should be sorted by name
	if instances[0].Name != "web-01" {
		t.Errorf("expected web-01 first, got %s", instances[0].Name)
	}
	if instances[0].PublicIP != "1.2.3.4" {
		t.Errorf("expected PublicIP 1.2.3.4, got %s", instances[0].PublicIP)
	}
	if instances[0].EIP != "5.6.7.8" {
		t.Errorf("expected EIP 5.6.7.8, got %s", instances[0].EIP)
	}
	if instances[1].Tags["env"] != "prod" {
		t.Errorf("expected tag env=prod, got %v", instances[1].Tags)
	}
}

func TestFetchAllInstances_Error(t *testing.T) {
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			return nil, fmt.Errorf("network error")
		},
	}
	c := newTestClient(mock)
	_, err := c.FetchAllInstances()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetInstanceDetail(t *testing.T) {
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			resp := &ecs.DescribeInstancesResponse{}
			resp.Instances.Instance = []ecs.Instance{
				{
					InstanceId:         "i-001",
					InstanceType:       "ecs.g6.large",
					Cpu:                2,
					Memory:             8192,
					OSName:             "CentOS 7",
					CreationTime:       "2024-01-01",
					ExpiredTime:        "2025-01-01",
					InstanceChargeType: "PrePaid",
					VpcAttributes:      ecs.VpcAttributes{VpcId: "vpc-001"},
					SecurityGroupIds:   ecs.SecurityGroupIdsInDescribeInstances{SecurityGroupId: []string{"sg-001"}},
				},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	detail, err := c.GetInstanceDetail("i-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.InstanceType != "ecs.g6.large" {
		t.Errorf("expected ecs.g6.large, got %s", detail.InstanceType)
	}
	if detail.CPU != 2 || detail.Memory != 8192 {
		t.Errorf("wrong cpu/mem: %d/%d", detail.CPU, detail.Memory)
	}
}

func TestGetInstanceDetail_NotFound(t *testing.T) {
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			resp := &ecs.DescribeInstancesResponse{}
			resp.Instances.Instance = nil
			return resp, nil
		},
	}
	c := newTestClient(mock)
	_, err := c.GetInstanceDetail("i-nonexistent")
	if err == nil || err.Error() != "instance not found: i-nonexistent" {
		t.Errorf("expected not found error, got %v", err)
	}
}

func TestStopStartRebootInstance(t *testing.T) {
	called := map[string]bool{}
	mock := &mockECS{
		stopInstanceFn: func(req *ecs.StopInstanceRequest) (*ecs.StopInstanceResponse, error) {
			called["stop"] = true
			if req.InstanceId != "i-001" {
				t.Errorf("wrong instanceId: %s", req.InstanceId)
			}
			return &ecs.StopInstanceResponse{}, nil
		},
		startInstanceFn: func(req *ecs.StartInstanceRequest) (*ecs.StartInstanceResponse, error) {
			called["start"] = true
			return &ecs.StartInstanceResponse{}, nil
		},
		rebootInstanceFn: func(req *ecs.RebootInstanceRequest) (*ecs.RebootInstanceResponse, error) {
			called["reboot"] = true
			return &ecs.RebootInstanceResponse{}, nil
		},
	}

	c := newTestClient(mock)
	if err := c.StopInstance("i-001"); err != nil {
		t.Errorf("stop: %v", err)
	}
	if err := c.StartInstance("i-001"); err != nil {
		t.Errorf("start: %v", err)
	}
	if err := c.RebootInstance("i-001"); err != nil {
		t.Errorf("reboot: %v", err)
	}
	for _, op := range []string{"stop", "start", "reboot"} {
		if !called[op] {
			t.Errorf("%s not called", op)
		}
	}
}

func TestWrapCommand_Short(t *testing.T) {
	result := wrapCommand("uptime")
	// All commands now use base64 encoding with COLUMNS prefix
	expectedB64 := base64.StdEncoding.EncodeToString([]byte("uptime"))
	expected := fmt.Sprintf("export COLUMNS=32767; eval \"$(echo '%s' | base64 -d)\"", expectedB64)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestWrapCommand_ShortWithQuotes(t *testing.T) {
	cmd := "echo 'hello world'"
	result := wrapCommand(cmd)
	expectedB64 := base64.StdEncoding.EncodeToString([]byte(cmd))
	expected := fmt.Sprintf("export COLUMNS=32767; eval \"$(echo '%s' | base64 -d)\"", expectedB64)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestWrapCommand_Long(t *testing.T) {
	// Create a command longer than longCommandThreshold
	longCmd := strings.Repeat("echo hello; ", 200)
	result := wrapCommand(longCmd)
	// Should use base64 eval wrapper with COLUMNS prefix
	prefix := "export COLUMNS=32767; eval \"$(echo '"
	if !strings.HasPrefix(result, prefix) {
		t.Errorf("expected base64 eval wrapper with COLUMNS, got: %s", result[:80])
	}
	if !strings.HasSuffix(result, "' | base64 -d)\"") {
		t.Errorf("expected base64 eval suffix, got: %s", result[len(result)-30:])
	}
	// Verify the embedded base64 decodes back to original command
	b64Part := result[len(prefix):len(result)-len("' | base64 -d)\"")]
	decoded, err := base64.StdEncoding.DecodeString(b64Part)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	if string(decoded) != longCmd {
		t.Errorf("decoded command mismatch")
	}
}

func TestWrapCommand_ExactThreshold(t *testing.T) {
	// All commands now use base64, regardless of length
	cmd := strings.Repeat("a", longCommandThreshold)
	result := wrapCommand(cmd)
	if !strings.HasPrefix(result, "export COLUMNS=32767; eval ") {
		t.Errorf("expected base64 wrapping at exact threshold")
	}
	cmd2 := strings.Repeat("a", longCommandThreshold+1)
	result2 := wrapCommand(cmd2)
	if !strings.HasPrefix(result2, "export COLUMNS=32767; eval ") {
		t.Errorf("expected base64 wrapping above threshold")
	}
}

func TestWrapCommand_ComplexCommand(t *testing.T) {
	// Complex command with semicolons, pipes, quotes — previously broken with bash -c
	cmd := "echo '=== Test ==='; sysctl net.ipv4.tcp_tw_reuse 2>/dev/null; netstat -tn | grep TIME_WAIT | wc -l"
	result := wrapCommand(cmd)
	// Extract and decode base64 content
	prefix := "export COLUMNS=32767; eval \"$(echo '"
	suffix := "' | base64 -d)\""
	b64Part := result[len(prefix):len(result)-len(suffix)]
	decoded, err := base64.StdEncoding.DecodeString(b64Part)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	if string(decoded) != cmd {
		t.Errorf("expected %q, got %q", cmd, string(decoded))
	}
}

func TestRunCommand_Success(t *testing.T) {
	invokeID := "inv-001"
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			// Verify command is base64 encoded
			decoded, err := base64.StdEncoding.DecodeString(req.CommandContent)
			if err != nil {
				t.Errorf("command not base64: %v", err)
			}
			// All commands now use base64 eval wrapper with COLUMNS prefix
			if !strings.HasPrefix(string(decoded), "export COLUMNS=32767; eval ") {
				t.Errorf("expected base64 eval wrapper, got '%s'", decoded)
			}
			resp := &ecs.RunCommandResponse{}
			resp.InvokeId = invokeID
			return resp, nil
		},
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			if req.InvokeId != invokeID {
				t.Errorf("wrong invokeId: %s", req.InvokeId)
			}
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Success", Output: base64.StdEncoding.EncodeToString([]byte("up 5 days")), ExitCode: 0},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	result, err := c.RunCommand("i-001", "uptime", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
}

func TestRunCommand_LongCommand(t *testing.T) {
	longCmd := strings.Repeat("echo hello; ", 200)
	invokeID := "inv-long"
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			decoded, err := base64.StdEncoding.DecodeString(req.CommandContent)
			if err != nil {
				t.Errorf("command not base64: %v", err)
			}
			// All commands now use eval wrapper with COLUMNS prefix
			if !strings.HasPrefix(string(decoded), "export COLUMNS=32767; eval ") {
				t.Errorf("expected eval wrapper for long command, got: %s", string(decoded)[:60])
			}
			resp := &ecs.RunCommandResponse{}
			resp.InvokeId = invokeID
			return resp, nil
		},
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Success", ExitCode: 0},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	result, err := c.RunCommand("i-001", longCmd, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
}

func TestRunCommand_Failed(t *testing.T) {
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			resp := &ecs.RunCommandResponse{}
			resp.InvokeId = "inv-fail"
			return resp, nil
		},
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Failed", ExitCode: 1, ErrorCode: "ExitCodeNonZero"},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	result, err := c.RunCommand("i-001", "exit 1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("expected exit 1, got %d", result.ExitCode)
	}
}

func TestRunCommand_FailedWithErrorCode(t *testing.T) {
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			resp := &ecs.RunCommandResponse{}
			resp.InvokeId = "inv-err"
			return resp, nil
		},
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Failed", ExitCode: 0, ErrorCode: "AgentNotRunning", ErrorInfo: "agent offline"},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	_, err := c.RunCommand("i-001", "cmd", 10)
	if err == nil {
		t.Fatal("expected error for agent not running")
	}
}

func TestRunCommand_Stopped(t *testing.T) {
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			resp := &ecs.RunCommandResponse{}
			resp.InvokeId = "inv-stop"
			return resp, nil
		},
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Stopped"},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	_, err := c.RunCommand("i-001", "cmd", 10)
	if err == nil || err.Error() != "command stopped" {
		t.Errorf("expected 'command stopped', got %v", err)
	}
}

func TestRunCommand_APIError(t *testing.T) {
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			return nil, fmt.Errorf("AccessDenied")
		},
	}

	c := newTestClient(mock)
	_, err := c.RunCommand("i-001", "cmd", 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunCommand_DefaultTimeout(t *testing.T) {
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			resp := &ecs.RunCommandResponse{}
			resp.InvokeId = "inv"
			return resp, nil
		},
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Success", ExitCode: 0},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	// timeout <= 0 should default to 60
	result, err := c.RunCommand("i-001", "cmd", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0")
	}
}

func TestStartSession(t *testing.T) {
	mock := &mockECS{
		startTerminalSessionFn: func(req *ecs.StartTerminalSessionRequest) (*ecs.StartTerminalSessionResponse, error) {
			resp := &ecs.StartTerminalSessionResponse{}
			resp.WebSocketUrl = "wss://example.com/ws"
			resp.SessionId = "sess-001"
			resp.SecurityToken = "tok"
			return resp, nil
		},
	}

	c := newTestClient(mock)
	url, sid, tok, err := c.StartSession("i-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "wss://example.com/ws" || sid != "sess-001" || tok != "tok" {
		t.Errorf("unexpected: url=%s sid=%s tok=%s", url, sid, tok)
	}
}

func TestStartPortForwardSession(t *testing.T) {
	mock := &mockECS{
		startTerminalSessionFn: func(req *ecs.StartTerminalSessionRequest) (*ecs.StartTerminalSessionResponse, error) {
			resp := &ecs.StartTerminalSessionResponse{}
			resp.WebSocketUrl = "wss://pf.example.com/ws"
			resp.SessionId = "pf-001"
			return resp, nil
		},
	}

	c := newTestClient(mock)
	url, _, _, err := c.StartPortForwardSession("i-001", 22)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "wss://pf.example.com/ws" {
		t.Errorf("unexpected url: %s", url)
	}
}

func TestStartSession_Error(t *testing.T) {
	mock := &mockECS{
		startTerminalSessionFn: func(req *ecs.StartTerminalSessionRequest) (*ecs.StartTerminalSessionResponse, error) {
			return nil, fmt.Errorf("session limit exceeded")
		},
	}

	c := newTestClient(mock)
	_, _, _, err := c.StartSession("i-001")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSendFile(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("hello world"), 0644)

	var sentContent string
	mock := &mockECS{
		sendFileFn: func(req *ecs.SendFileRequest) (*ecs.SendFileResponse, error) {
			sentContent = req.Content
			if req.Name != "test.txt" {
				t.Errorf("expected name test.txt, got %s", req.Name)
			}
			if req.TargetDir != "/tmp" {
				t.Errorf("expected dir /tmp, got %s", req.TargetDir)
			}
			return &ecs.SendFileResponse{}, nil
		},
	}

	c := newTestClient(mock)
	err := c.SendFile("i-001", tmpFile, "/tmp", "test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	decoded, _ := base64.StdEncoding.DecodeString(sentContent)
	if string(decoded) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", decoded)
	}
}

func TestSendFile_ReadError(t *testing.T) {
	c := newTestClient(&mockECS{})
	err := c.SendFile("i-001", "/nonexistent/file.txt", "/tmp", "file.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSendFile_APIError(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("data"), 0644)

	mock := &mockECS{
		sendFileFn: func(req *ecs.SendFileRequest) (*ecs.SendFileResponse, error) {
			return nil, fmt.Errorf("file too large")
		},
	}

	c := newTestClient(mock)
	err := c.SendFile("i-001", tmpFile, "/tmp", "test.txt")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchInstanceByID(t *testing.T) {
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			resp := &ecs.DescribeInstancesResponse{}
			resp.Instances.Instance = []ecs.Instance{
				{
					InstanceId:   "i-001",
					InstanceName: "test",
					Status:       "Running",
					RegionId:     "cn-test",
					VpcAttributes: ecs.VpcAttributes{
						PrivateIpAddress: ecs.PrivateIpAddressInDescribeInstanceAttribute{IpAddress: []string{"10.0.0.1"}},
					},
				},
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	instances, err := c.FetchInstanceByID("i-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != "i-001" {
		t.Errorf("unexpected: %v", instances)
	}
}

func TestNewClient_InvalidCredentials(t *testing.T) {
	cfg := &model.Config{
		AccessKeyID:     "",
		AccessKeySecret: "",
		Region:          "cn-test",
	}
	// NewClient with empty credentials — SDK may or may not error
	// We test that the function doesn't panic
	_, _ = NewClient(cfg)
}

// --- Additional tests for 100% coverage ---

func TestFetchAllInstances_MultiPage(t *testing.T) {
	callCount := 0
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			callCount++
			resp := &ecs.DescribeInstancesResponse{}
			resp.TotalCount = 2
			if callCount == 1 {
				resp.Instances.Instance = []ecs.Instance{
					{InstanceId: "i-001", InstanceName: "a"},
				}
			} else {
				resp.Instances.Instance = []ecs.Instance{
					{InstanceId: "i-002", InstanceName: "b"},
				}
			}
			return resp, nil
		},
	}

	c := newTestClient(mock)
	instances, err := c.FetchAllInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances across 2 pages, got %d", len(instances))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (pagination), got %d", callCount)
	}
}

func TestGetInstanceDetail_APIError(t *testing.T) {
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			return nil, fmt.Errorf("network timeout")
		},
	}
	c := newTestClient(mock)
	_, err := c.GetInstanceDetail("i-001")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchInstanceByID_APIError(t *testing.T) {
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			return nil, fmt.Errorf("forbidden")
		},
	}
	c := newTestClient(mock)
	_, err := c.FetchInstanceByID("i-001")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchInstanceByID_NoPrivateIP(t *testing.T) {
	mock := &mockECS{
		describeInstancesFn: func(req *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
			resp := &ecs.DescribeInstancesResponse{}
			resp.Instances.Instance = []ecs.Instance{
				{InstanceId: "i-001", InstanceName: "test", Status: "Running"},
			}
			return resp, nil
		},
	}
	c := newTestClient(mock)
	instances, err := c.FetchInstanceByID("i-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if instances[0].PrivateIP != "" {
		t.Errorf("expected empty PrivateIP, got %s", instances[0].PrivateIP)
	}
}

func TestStartPortForwardSession_Error(t *testing.T) {
	mock := &mockECS{
		startTerminalSessionFn: func(req *ecs.StartTerminalSessionRequest) (*ecs.StartTerminalSessionResponse, error) {
			return nil, fmt.Errorf("port unavailable")
		},
	}
	c := newTestClient(mock)
	_, _, _, err := c.StartPortForwardSession("i-001", 22)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunCommand_ThrottleRetryExhausted(t *testing.T) {
	callCount := 0
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			callCount++
			return nil, fmt.Errorf("Throttling.User")
		},
	}
	c := newTestClient(mock)
	_, err := c.RunCommand("i-001", "cmd", 10)
	if err == nil {
		t.Fatal("expected throttle error")
	}
	if callCount < 5 {
		t.Errorf("expected at least 5 retries, got %d", callCount)
	}
}

func TestRunCommand_Finished(t *testing.T) {
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			resp := &ecs.RunCommandResponse{}
			resp.InvokeId = "inv"
			return resp, nil
		},
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Finished", ExitCode: 0, Output: "done"},
			}
			return resp, nil
		},
	}
	c := newTestClient(mock)
	result, err := c.RunCommand("i-001", "cmd", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Error("expected exit 0")
	}
}

func TestWaitForResult_ThrottleThenNonThrottleError(t *testing.T) {
	callCount := 0
	mock := &mockECS{
		runCommandFn: func(req *ecs.RunCommandRequest) (*ecs.RunCommandResponse, error) {
			resp := &ecs.RunCommandResponse{}
			resp.InvokeId = "inv"
			return resp, nil
		},
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("Throttling")
			}
			return nil, fmt.Errorf("InternalError")
		},
	}
	c := newTestClient(mock)
	_, err := c.RunCommand("i-001", "cmd", 30)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWaitForResult_Timeout(t *testing.T) {
	mock := &mockECS{
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			// Return Running status → triggers timeout
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Running"},
			}
			return resp, nil
		},
	}
	c := newTestClient(mock)
	// Call waitForResult directly with -10 timeout so deadline is already past
	_, err := c.waitForResult("inv", "i-001", -10)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForResult_DelayCap(t *testing.T) {
	// Covers the delay > 5s cap path (attempt >= 4 for 500ms * 2^4 = 8s > 5s)
	callCount := 0
	mock := &mockECS{
		describeInvocationResultsFn: func(req *ecs.DescribeInvocationResultsRequest) (*ecs.DescribeInvocationResultsResponse, error) {
			callCount++
			if callCount <= 5 {
				// Return Running for first 5 calls to reach high attempt count
				resp := &ecs.DescribeInvocationResultsResponse{}
				resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
					{InvocationStatus: "Running"},
				}
				return resp, nil
			}
			// Then succeed
			resp := &ecs.DescribeInvocationResultsResponse{}
			resp.Invocation.InvocationResults.InvocationResult = []ecs.InvocationResult{
				{InvocationStatus: "Success", ExitCode: 0},
			}
			return resp, nil
		},
	}
	c := newTestClient(mock)
	result, err := c.waitForResult("inv", "i-001", 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Error("expected exit 0")
	}
	if callCount < 5 {
		t.Errorf("expected at least 5 calls to hit delay cap, got %d", callCount)
	}
}

func TestNewClient_Error(t *testing.T) {
	origFactory := ecsClientFactory
	defer func() { ecsClientFactory = origFactory }()

	ecsClientFactory = func(region, akID, akSecret string) (ecsAPI, error) {
		return nil, fmt.Errorf("auth failed")
	}

	cfg := &model.Config{Region: "cn-test", AccessKeyID: "ak", AccessKeySecret: "sk"}
	_, err := NewClient(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewClient_Success(t *testing.T) {
	origFactory := ecsClientFactory
	defer func() { ecsClientFactory = origFactory }()

	ecsClientFactory = func(region, akID, akSecret string) (ecsAPI, error) {
		return &mockECS{}, nil
	}

	cfg := &model.Config{Region: "cn-test", AccessKeyID: "ak", AccessKeySecret: "sk"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client")
	}
}
