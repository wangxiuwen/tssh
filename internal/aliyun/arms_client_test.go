package aliyun

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wangxiuwen/tssh/internal/model"
)

func newTestARMSClient(response string, err error) *ARMSClient {
	return &ARMSClient{
		doRequest: func(apiName string, params map[string]string) ([]byte, error) {
			if err != nil {
				return nil, err
			}
			return []byte(response), nil
		},
		region: "cn-beijing",
	}
}

func TestNewARMSClient_Success(t *testing.T) {
	orig := armsClientFactory
	defer func() { armsClientFactory = orig }()

	armsClientFactory = func(region, id, secret string) (armsRequester, error) {
		return func(apiName string, params map[string]string) ([]byte, error) {
			return []byte("{}"), nil
		}, nil
	}

	client, err := NewARMSClient(&model.Config{
		AccessKeyID: "test-id", AccessKeySecret: "test-secret", Region: "cn-beijing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("client should not be nil")
	}
}

func TestNewARMSClient_Error(t *testing.T) {
	orig := armsClientFactory
	defer func() { armsClientFactory = orig }()

	armsClientFactory = func(region, id, secret string) (armsRequester, error) {
		return nil, fmt.Errorf("bad credentials")
	}

	_, err := NewARMSClient(&model.Config{
		AccessKeyID: "bad", AccessKeySecret: "bad", Region: "cn-beijing",
	})
	if err == nil {
		t.Error("expected error")
	}
}

func TestListGrafanaWorkspaces_Success(t *testing.T) {
	client := newTestARMSClient(`{
		"Code": 200,
		"Data": [
			{
				"grafanaWorkspaceId": "grafana-cn-123",
				"grafanaWorkspaceName": "my-grafana",
				"grafanaWorkspaceDomain": "grafana-cn-123.grafana.aliyuncs.com:443",
				"protocol": "https",
				"status": "Running",
				"grafanaVersion": "10.0.x"
			}
		]
	}`, nil)

	workspaces, err := client.ListGrafanaWorkspaces()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(workspaces))
	}
	if workspaces[0].Name != "my-grafana" {
		t.Errorf("expected my-grafana, got %s", workspaces[0].Name)
	}
	if workspaces[0].Domain != "grafana-cn-123.grafana.aliyuncs.com:443" {
		t.Errorf("unexpected domain: %s", workspaces[0].Domain)
	}
}

func TestListGrafanaWorkspaces_APIError(t *testing.T) {
	client := newTestARMSClient("", fmt.Errorf("network error"))
	_, err := client.ListGrafanaWorkspaces()
	if err == nil {
		t.Error("expected error")
	}
}

func TestListGrafanaWorkspaces_InvalidJSON(t *testing.T) {
	client := newTestARMSClient("not json", nil)
	_, err := client.ListGrafanaWorkspaces()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGetPrometheusToken_Success(t *testing.T) {
	client := newTestARMSClient(`{"RequestId":"xxx","Token":"abc123token"}`, nil)

	token, err := client.GetPrometheusToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "abc123token" {
		t.Errorf("expected abc123token, got %s", token)
	}
}

func TestGetPrometheusToken_Error(t *testing.T) {
	client := newTestARMSClient("", fmt.Errorf("auth error"))
	_, err := client.GetPrometheusToken()
	if err == nil {
		t.Error("expected error")
	}
}

func TestListActivatedAlerts_Success(t *testing.T) {
	client := newTestARMSClient(`{
		"Page": {
			"Alerts": [
				{"AlertId":"alert-1","AlertName":"HighCPU","Status":"Active","Severity":"critical","StartsAt":1700000000000},
				{"AlertId":"alert-2","AlertName":"LowDisk","Status":"Active","Severity":"warning","StartsAt":1700001000000}
			],
			"Total": 2, "Page": 1, "PageSize": 50
		}
	}`, nil)

	alerts, total, err := client.ListActivatedAlerts(1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total 2, got %d", total)
	}
	if len(alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(alerts))
	}
	if alerts[0].AlertName != "HighCPU" {
		t.Errorf("expected HighCPU, got %s", alerts[0].AlertName)
	}
}

func TestListActivatedAlerts_Error(t *testing.T) {
	client := newTestARMSClient("", fmt.Errorf("api error"))
	_, _, err := client.ListActivatedAlerts(1, 50)
	if err == nil {
		t.Error("expected error")
	}
}

func TestFetchAllActivatedAlerts_SinglePage(t *testing.T) {
	client := newTestARMSClient(`{
		"Page": {"Alerts":[{"AlertId":"a1","AlertName":"Test"}],"Total":1}
	}`, nil)

	alerts, err := client.FetchAllActivatedAlerts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
}

func TestFetchAllActivatedAlerts_Error(t *testing.T) {
	client := newTestARMSClient("", fmt.Errorf("network error"))
	_, err := client.FetchAllActivatedAlerts()
	if err == nil {
		t.Error("expected error")
	}
}

func TestDiscoverGrafanaConfig_Success(t *testing.T) {
	client := newTestARMSClient(`{
		"Code": 200,
		"Data": [{
			"grafanaWorkspaceDomain":"grafana-cn-123.grafana.aliyuncs.com:443",
			"protocol":"https",
			"status":"Running"
		}]
	}`, nil)

	cfg, err := client.DiscoverGrafanaConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "https://grafana-cn-123.grafana.aliyuncs.com:443" {
		t.Errorf("unexpected endpoint: %s", cfg.Endpoint)
	}
}

func TestDiscoverGrafanaConfig_NoRunningWorkspace(t *testing.T) {
	client := newTestARMSClient(`{
		"Code":200,"Data":[{"grafanaWorkspaceDomain":"x.com","status":"Stopped"}]
	}`, nil)

	_, err := client.DiscoverGrafanaConfig()
	if err == nil {
		t.Error("expected error for no running workspace")
	}
}

func TestDiscoverGrafanaConfig_DefaultProtocol(t *testing.T) {
	client := newTestARMSClient(`{
		"Code":200,"Data":[{"grafanaWorkspaceDomain":"g.example.com:443","protocol":"","status":"Running"}]
	}`, nil)

	cfg, err := client.DiscoverGrafanaConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "https://g.example.com:443" {
		t.Errorf("expected https default, got %s", cfg.Endpoint)
	}
}

func TestPrometheusDirectURL(t *testing.T) {
	url := PrometheusDirectURL("cn-beijing", "mytoken123")
	expected := "http://cn-beijing.arms.aliyuncs.com:9090/api/v1/prometheus/mytoken123"
	if url != expected {
		t.Errorf("expected %s, got %s", expected, url)
	}
}

func TestPrometheusDirectQuery_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if q != "up" {
			t.Errorf("expected query=up, got %s", q)
		}
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer ts.Close()

	data, err := PrometheusDirectQuery(ts.URL, "up")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty response")
	}
}

func TestPrometheusDirectQuery_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	_, err := PrometheusDirectQuery(ts.URL, "up")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestPrometheusDirectQuery_ConnectionError(t *testing.T) {
	_, err := PrometheusDirectQuery("http://127.0.0.1:1", "up")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestARMSClient_RateLimit(t *testing.T) {
	calls := 0
	client := &ARMSClient{
		doRequest: func(apiName string, params map[string]string) ([]byte, error) {
			calls++
			return []byte(`{"Data":[]}`), nil
		},
		region:  "cn-beijing",
		sleepFn: func(d time.Duration) {},
	}

	client.ListGrafanaWorkspaces()
	client.ListGrafanaWorkspaces()
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}
