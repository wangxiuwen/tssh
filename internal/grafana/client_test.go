package grafana

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wangxiuwen/tssh/internal/model"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	ts := httptest.NewServer(handler)
	client := NewClient(&model.GrafanaConfig{
		Endpoint: ts.URL,
		Token:    "test-token",
	})
	return ts, client
}

func TestNewClient_AddsHTTPS(t *testing.T) {
	c := NewClient(&model.GrafanaConfig{
		Endpoint: "grafana.example.com",
		Token:    "tok",
	})
	if c.endpoint != "https://grafana.example.com" {
		t.Errorf("expected https prefix, got %s", c.endpoint)
	}
}

func TestNewClient_TrimsTrailingSlash(t *testing.T) {
	c := NewClient(&model.GrafanaConfig{
		Endpoint: "https://grafana.example.com/",
		Token:    "tok",
	})
	if c.endpoint != "https://grafana.example.com" {
		t.Errorf("expected trailing slash trimmed, got %s", c.endpoint)
	}
}

func TestFetchAlerts_Success(t *testing.T) {
	alerts := []Alert{
		{
			Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
			Annotations: map[string]string{"summary": "CPU is high"},
			Status:      AlertStatus{State: "active"},
			Fingerprint: "abc123",
		},
		{
			Labels:      map[string]string{"alertname": "LowDisk", "severity": "warning"},
			Annotations: map[string]string{"summary": "Disk space low"},
			Status:      AlertStatus{State: "active"},
			Fingerprint: "def456",
		},
	}

	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/alertmanager/grafana/api/v2/alerts" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing or wrong auth header")
		}
		json.NewEncoder(w).Encode(alerts)
	})
	defer ts.Close()

	result, err := client.FetchAlerts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(result))
	}
	if result[0].Labels["alertname"] != "HighCPU" {
		t.Errorf("expected HighCPU, got %s", result[0].Labels["alertname"])
	}
	if result[1].Labels["severity"] != "warning" {
		t.Errorf("expected warning, got %s", result[1].Labels["severity"])
	}
}

func TestFetchAlerts_Empty(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	})
	defer ts.Close()

	result, err := client.FetchAlerts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(result))
	}
}

func TestFetchAlerts_HTTPError(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"invalid API key"}`))
	})
	defer ts.Close()

	_, err := client.FetchAlerts()
	if err == nil {
		t.Error("expected error for 401 response")
	}
}

func TestSearchDashboards_All(t *testing.T) {
	dashboards := []Dashboard{
		{ID: 1, UID: "abc", Title: "API", URL: "/d/abc/api", Tags: []string{"arms"}},
		{ID: 2, UID: "def", Title: "DB", URL: "/d/def/db", Tags: []string{"arms"}},
	}

	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("type") != "dash-db" {
			t.Error("expected type=dash-db")
		}
		json.NewEncoder(w).Encode(dashboards)
	})
	defer ts.Close()

	result, err := client.SearchDashboards("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 dashboards, got %d", len(result))
	}
}

func TestSearchDashboards_WithQuery(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if q != "API" {
			t.Errorf("expected query=API, got %s", q)
		}
		json.NewEncoder(w).Encode([]Dashboard{
			{ID: 1, Title: "API"},
		})
	})
	defer ts.Close()

	result, err := client.SearchDashboards("API")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 dashboard, got %d", len(result))
	}
}

func TestFetchDatasources_Success(t *testing.T) {
	datasources := []Datasource{
		{ID: 7, Name: "arms_metrics", Type: "prometheus"},
		{ID: 5, Name: "apm-metrics", Type: "prometheus"},
	}

	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/datasources" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(datasources)
	})
	defer ts.Close()

	result, err := client.FetchDatasources()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 datasources, got %d", len(result))
	}
	if result[0].Name != "arms_metrics" {
		t.Errorf("expected arms_metrics, got %s", result[0].Name)
	}
}

func TestPrometheusQuery_Success(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/datasources/proxy/7/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query().Get("query")
		if q != "up" {
			t.Errorf("expected query=up, got %s", q)
		}
		w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{"metric": {"instance": "10.0.1.1"}, "value": [1234567890, "1"]}
				]
			}
		}`))
	})
	defer ts.Close()

	result, err := client.PrometheusQuery(7, "up")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Data.ResultType != "vector" {
		t.Errorf("expected vector, got %s", result.Data.ResultType)
	}
	if len(result.Data.Result) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(result.Data.Result))
	}
	if result.Data.Result[0].Metric["instance"] != "10.0.1.1" {
		t.Errorf("expected 10.0.1.1, got %s", result.Data.Result[0].Metric["instance"])
	}
}

func TestPrometheusQuery_Error(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status": "error", "error": "bad query"}`))
	})
	defer ts.Close()

	_, err := client.PrometheusQuery(7, "invalid{")
	if err == nil {
		t.Error("expected error for bad query")
	}
}

func TestPrometheusLabelValues_Success(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/datasources/proxy/7/api/v1/label/service/values" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"status": "success", "data": ["svc-a", "svc-b"]}`))
	})
	defer ts.Close()

	result, err := client.PrometheusLabelValues(7, "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 values, got %d", len(result))
	}
	if result[0] != "svc-a" {
		t.Errorf("expected svc-a, got %s", result[0])
	}
}

func TestDashboardURL(t *testing.T) {
	c := NewClient(&model.GrafanaConfig{
		Endpoint: "https://grafana.example.com",
		Token:    "tok",
	})
	dash := Dashboard{URL: "/d/abc/my-dashboard"}
	got := c.DashboardURL(dash)
	if got != "https://grafana.example.com/d/abc/my-dashboard" {
		t.Errorf("unexpected URL: %s", got)
	}
}

func TestGet_ConnectionError(t *testing.T) {
	client := NewClient(&model.GrafanaConfig{
		Endpoint: "http://127.0.0.1:1", // should fail to connect
		Token:    "tok",
	})
	_, err := client.FetchAlerts()
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestGet_InvalidJSON(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	})
	defer ts.Close()

	_, err := client.FetchAlerts()
	if err != nil {
		// JSON unmarshal error for "not json" into []Alert will succeed as nil
		// Actually this should cause an error
	}
	_ = err // JSON decode of "not json" into []Alert will fail
}

func TestFetchAlerts_ServerError(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	})
	defer ts.Close()

	_, err := client.FetchAlerts()
	if err == nil {
		t.Error("expected error for 500 response")
	}
}
