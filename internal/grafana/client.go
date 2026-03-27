package grafana

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wangxiuwen/tssh/internal/model"
)

// Client wraps Grafana HTTP API calls.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

// NewClient creates a Grafana API client from config.
func NewClient(cfg *model.GrafanaConfig) *Client {
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "https://" + endpoint
	}
	return &Client{
		endpoint: endpoint,
		token:    cfg.Token,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

// Alert represents a firing alert from Grafana Alertmanager API.
type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
	Status      AlertStatus       `json:"status"`
	Fingerprint string            `json:"fingerprint"`
	GeneratorURL string           `json:"generatorURL"`
}

// AlertStatus holds the alert state.
type AlertStatus struct {
	State       string   `json:"state"`
	SilencedBy  []string `json:"silencedBy"`
	InhibitedBy []string `json:"inhibitedBy"`
}

// Dashboard represents a Grafana dashboard search result.
type Dashboard struct {
	ID          int      `json:"id"`
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	URI         string   `json:"uri"`
	URL         string   `json:"url"`
	Type        string   `json:"type"`
	Tags        []string `json:"tags"`
	FolderTitle string   `json:"folderTitle"`
	FolderUID   string   `json:"folderUid"`
}

// Datasource represents a Grafana data source.
type Datasource struct {
	ID        int    `json:"id"`
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	IsDefault bool   `json:"isDefault"`
}

// PromQueryResult represents a Prometheus instant query response.
type PromQueryResult struct {
	Status string       `json:"status"`
	Data   PromData     `json:"data"`
	Error  string       `json:"error,omitempty"`
}

// PromData holds Prometheus query data.
type PromData struct {
	ResultType string       `json:"resultType"`
	Result     []PromSample `json:"result"`
}

// PromSample represents a single Prometheus sample.
type PromSample struct {
	Metric map[string]string `json:"metric"`
	Value  [2]interface{}    `json:"value"` // [timestamp, value_string]
}

// FetchAlerts returns currently firing alerts.
func (c *Client) FetchAlerts() ([]Alert, error) {
	var alerts []Alert
	if err := c.get("/api/alertmanager/grafana/api/v2/alerts", &alerts); err != nil {
		return nil, fmt.Errorf("获取告警失败: %w", err)
	}
	return alerts, nil
}

// SearchDashboards searches dashboards by optional query string.
func (c *Client) SearchDashboards(query string) ([]Dashboard, error) {
	path := "/api/search?type=dash-db"
	if query != "" {
		path += "&query=" + url.QueryEscape(query)
	}
	var dashboards []Dashboard
	if err := c.get(path, &dashboards); err != nil {
		return nil, fmt.Errorf("搜索仪表盘失败: %w", err)
	}
	return dashboards, nil
}

// FetchDatasources returns all configured data sources.
func (c *Client) FetchDatasources() ([]Datasource, error) {
	var datasources []Datasource
	if err := c.get("/api/datasources", &datasources); err != nil {
		return nil, fmt.Errorf("获取数据源失败: %w", err)
	}
	return datasources, nil
}

// PrometheusQuery executes an instant PromQL query via Grafana datasource proxy.
func (c *Client) PrometheusQuery(dsID int, query string) (*PromQueryResult, error) {
	path := fmt.Sprintf("/api/datasources/proxy/%d/api/v1/query?query=%s", dsID, url.QueryEscape(query))
	var result PromQueryResult
	if err := c.get(path, &result); err != nil {
		return nil, fmt.Errorf("Prometheus 查询失败: %w", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("Prometheus 查询错误: %s", result.Error)
	}
	return &result, nil
}

// PrometheusLabelValues returns values for a Prometheus label.
func (c *Client) PrometheusLabelValues(dsID int, label string) ([]string, error) {
	path := fmt.Sprintf("/api/datasources/proxy/%d/api/v1/label/%s/values", dsID, url.PathEscape(label))
	var result struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := c.get(path, &result); err != nil {
		return nil, fmt.Errorf("获取标签值失败: %w", err)
	}
	return result.Data, nil
}

// DashboardURL returns the full URL for a dashboard.
func (c *Client) DashboardURL(dash Dashboard) string {
	return c.endpoint + dash.URL
}

func (c *Client) get(path string, out interface{}) error {
	req, err := http.NewRequest("GET", c.endpoint+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.Unmarshal(body, out)
}
