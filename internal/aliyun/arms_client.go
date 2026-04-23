package aliyun

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/responses"
	"github.com/wangxiuwen/tssh/internal/model"
)

// armsRequester executes ARMS API requests — extracted for testability
type armsRequester func(apiName string, params map[string]string) ([]byte, error)

// ARMSClient wraps Aliyun ARMS API calls
type ARMSClient struct {
	doRequest armsRequester
	region    string
	mu        sync.Mutex
	lastReq   time.Time
	sleepFn   func(time.Duration)
}

// GrafanaWorkspace holds Grafana workspace info from ARMS API
type GrafanaWorkspace struct {
	ID       string `json:"grafanaWorkspaceId"`
	Name     string `json:"grafanaWorkspaceName"`
	Domain   string `json:"grafanaWorkspaceDomain"`
	Protocol string `json:"protocol"`
	Status   string `json:"status"`
	Version  string `json:"grafanaVersion"`
}

// ActivatedAlert holds a firing alert from ARMS API
type ActivatedAlert struct {
	AlertID         string            `json:"AlertId"`
	AlertName       string            `json:"AlertName"`
	Status          string            `json:"Status"`
	Message         string            `json:"Message"`
	Severity        string            `json:"Severity"`
	Count           int               `json:"Count"`
	StartsAt        int64             `json:"StartsAt"`
	EndsAt          int64             `json:"EndsAt"`
	IntegrationName string            `json:"IntegrationName"`
	IntegrationType string            `json:"IntegrationType"`
	ExpandFields    map[string]string `json:"ExpandFields"`
}

// TraceInfo is a summary entry from SearchTraceAppByPage — one per matching trace.
type TraceInfo struct {
	TraceID       string `json:"TraceID"`
	Pid           string `json:"Pid"`
	ServiceName   string `json:"ServiceName"`
	ServiceIp     string `json:"ServiceIp"`
	OperationName string `json:"OperationName"`
	SpanID        string `json:"SpanId"`
	Duration      int64  `json:"Duration"`
	Timestamp     int64  `json:"Timestamp"`
}

// TraceTag is a K/V pair on a span.
type TraceTag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

// TraceLogEvent is a log line associated with a span (e.g. exception stack).
type TraceLogEvent struct {
	Timestamp int64      `json:"Timestamp"`
	TagEntryList []TraceTag `json:"TagEntryList"`
}

// TraceSpan is a single span in a trace, returned by GetTrace.
type TraceSpan struct {
	TraceID       string          `json:"TraceID"`
	SpanID        string          `json:"SpanId"`
	RpcID         string          `json:"RpcId"`
	RpcType       int             `json:"RpcType"`
	OperationName string          `json:"OperationName"`
	ServiceName   string          `json:"ServiceName"`
	ServiceIp     string          `json:"ServiceIp"`
	Pid           string          `json:"Pid"`
	Duration      int64           `json:"Duration"`
	Timestamp     int64           `json:"Timestamp"`
	ResultCode    string          `json:"ResultCode"`
	HaveStack     bool            `json:"HaveStack"`
	TagEntryList  []TraceTag      `json:"TagEntryList"`
	LogEventList  []TraceLogEvent `json:"LogEventList"`
}

// TraceSearchOptions are filters for SearchTraceAppByPage.
// Tags are custom key/value filters; StartMs/EndMs are Unix millis (zero means use defaults).
type TraceSearchOptions struct {
	Pid           string
	OperationName string
	ServiceIp     string
	Tags          map[string]string
	MinDurationMs int64
	StartMs       int64
	EndMs         int64
	PageSize      int
	CurrentPage   int
}

// armsClientFactory creates the SDK requester — overridable in tests
var armsClientFactory = func(region, accessKeyID, accessKeySecret string) (armsRequester, error) {
	client, err := sdk.NewClientWithAccessKey(region, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, err
	}
	return func(apiName string, params map[string]string) ([]byte, error) {
		req := requests.NewCommonRequest()
		req.Method = "POST"
		req.Scheme = "https"
		req.Domain = "arms." + region + ".aliyuncs.com"
		req.Version = "2019-08-08"
		req.ApiName = apiName
		for k, v := range params {
			req.QueryParams[k] = v
		}
		resp, err := client.ProcessCommonRequest(req)
		if err != nil {
			return nil, err
		}
		return resp.GetHttpContentBytes(), nil
	}, nil
}

// NewARMSClient creates a new ARMS API client from config
func NewARMSClient(cfg *model.Config) (*ARMSClient, error) {
	requester, err := armsClientFactory(cfg.Region, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("ARMS client: %w", err)
	}
	return &ARMSClient{doRequest: requester, region: cfg.Region}, nil
}

func (c *ARMSClient) rateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if elapsed := now.Sub(c.lastReq); elapsed < apiMinInterval {
		if c.sleepFn != nil {
			c.sleepFn(apiMinInterval - elapsed)
		} else {
			time.Sleep(apiMinInterval - elapsed)
		}
	}
	c.lastReq = time.Now()
}

func (c *ARMSClient) call(apiName string, params map[string]string) ([]byte, error) {
	c.rateLimit()
	if params == nil {
		params = map[string]string{}
	}
	params["RegionId"] = c.region
	return c.doRequest(apiName, params)
}

// ListGrafanaWorkspaces returns all Grafana workspaces in the region
func (c *ARMSClient) ListGrafanaWorkspaces() ([]GrafanaWorkspace, error) {
	data, err := c.call("ListGrafanaWorkspace", nil)
	if err != nil {
		return nil, fmt.Errorf("ListGrafanaWorkspace: %w", err)
	}
	var result struct {
		Data []GrafanaWorkspace `json:"Data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result.Data, nil
}

// GetPrometheusToken returns the Prometheus API token for the current account
func (c *ARMSClient) GetPrometheusToken() (string, error) {
	data, err := c.call("GetPrometheusApiToken", nil)
	if err != nil {
		return "", fmt.Errorf("GetPrometheusApiToken: %w", err)
	}
	var result struct {
		Token string `json:"Token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	return result.Token, nil
}

// ListActivatedAlerts returns currently firing alerts
func (c *ARMSClient) ListActivatedAlerts(page, pageSize int) ([]ActivatedAlert, int, error) {
	data, err := c.call("ListActivatedAlerts", map[string]string{
		"CurrentPage": fmt.Sprintf("%d", page),
		"PageSize":    fmt.Sprintf("%d", pageSize),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("ListActivatedAlerts: %w", err)
	}
	var result struct {
		Page struct {
			Alerts   []ActivatedAlert `json:"Alerts"`
			Total    int              `json:"Total"`
		} `json:"Page"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, fmt.Errorf("parse response: %w", err)
	}
	return result.Page.Alerts, result.Page.Total, nil
}

// FetchAllActivatedAlerts returns all firing alerts across all pages
func (c *ARMSClient) FetchAllActivatedAlerts() ([]ActivatedAlert, error) {
	var all []ActivatedAlert
	page := 1
	for {
		alerts, total, err := c.ListActivatedAlerts(page, 50)
		if err != nil {
			return nil, err
		}
		all = append(all, alerts...)
		if len(all) >= total {
			break
		}
		page++
	}
	return all, nil
}

// DiscoverGrafanaConfig auto-discovers Grafana endpoint from ARMS API
func (c *ARMSClient) DiscoverGrafanaConfig() (*model.GrafanaConfig, error) {
	workspaces, err := c.ListGrafanaWorkspaces()
	if err != nil {
		return nil, err
	}
	for _, ws := range workspaces {
		if ws.Status == "Running" && ws.Domain != "" {
			protocol := ws.Protocol
			if protocol == "" {
				protocol = "https"
			}
			return &model.GrafanaConfig{
				Endpoint: protocol + "://" + ws.Domain,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到运行中的 Grafana workspace")
}

// PrometheusDirectURL constructs a direct Prometheus query URL using ARMS token
func PrometheusDirectURL(region, token string) string {
	return fmt.Sprintf("http://%s.arms.aliyuncs.com:9090/api/v1/prometheus/%s", region, token)
}

// PrometheusDirectQuery executes a PromQL query directly against ARMS Prometheus
func PrometheusDirectQuery(baseURL, query string) ([]byte, error) {
	u := baseURL + "/api/v1/query?query=" + url.QueryEscape(query)
	resp, err := http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("prometheus query: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// SearchTraces searches trace summaries by tag/pid/time filters.
// Returns at most opts.PageSize entries. If opts.StartMs/EndMs are zero,
// defaults to the last hour.
func (c *ARMSClient) SearchTraces(opts TraceSearchOptions) ([]TraceInfo, int, error) {
	now := time.Now()
	start := opts.StartMs
	end := opts.EndMs
	if end == 0 {
		end = now.UnixMilli()
	}
	if start == 0 {
		start = now.Add(-1 * time.Hour).UnixMilli()
	}
	page := opts.CurrentPage
	if page <= 0 {
		page = 1
	}
	size := opts.PageSize
	if size <= 0 {
		size = 50
	}

	params := map[string]string{
		"StartTime":   fmt.Sprintf("%d", start),
		"EndTime":     fmt.Sprintf("%d", end),
		"CurrentPage": fmt.Sprintf("%d", page),
		"PageSize":    fmt.Sprintf("%d", size),
	}
	if opts.Pid != "" {
		params["Pid"] = opts.Pid
	}
	if opts.OperationName != "" {
		params["OperationName"] = opts.OperationName
	}
	if opts.ServiceIp != "" {
		params["ServiceIp"] = opts.ServiceIp
	}
	if opts.MinDurationMs > 0 {
		params["MinDuration"] = fmt.Sprintf("%d", opts.MinDurationMs)
	}
	i := 1
	for k, v := range opts.Tags {
		params[fmt.Sprintf("Tag.%d.Key", i)] = k
		params[fmt.Sprintf("Tag.%d.Value", i)] = v
		i++
	}

	data, err := c.call("SearchTraceAppByPage", params)
	if err != nil {
		return nil, 0, fmt.Errorf("SearchTraceAppByPage: %w", err)
	}
	var result struct {
		PageBean struct {
			Total      int         `json:"Total"`
			TraceInfos []TraceInfo `json:"TraceInfos"`
		} `json:"PageBean"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, fmt.Errorf("parse response: %w", err)
	}
	return result.PageBean.TraceInfos, result.PageBean.Total, nil
}

// GetTrace fetches all spans for a given TraceID.
func (c *ARMSClient) GetTrace(traceID string) ([]TraceSpan, error) {
	if traceID == "" {
		return nil, fmt.Errorf("TraceID 不能为空")
	}
	data, err := c.call("GetTrace", map[string]string{"TraceID": traceID})
	if err != nil {
		return nil, fmt.Errorf("GetTrace: %w", err)
	}
	var result struct {
		Spans []TraceSpan `json:"Spans"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result.Spans, nil
}

// Keep import references
var (
	_ = (*responses.CommonResponse)(nil)
)
