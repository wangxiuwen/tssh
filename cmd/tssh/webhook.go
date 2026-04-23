package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// sendWebhook sends execution results to a webhook URL (DingTalk/Feishu/Slack/generic)
func sendWebhook(webhookURL string, summary string, details string) error {
	var payload []byte

	if strings.Contains(webhookURL, "dingtalk") || strings.Contains(webhookURL, "oapi.dingtalk.com") {
		// DingTalk format
		msg := map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": "tssh 执行通知",
				"text":  fmt.Sprintf("### tssh 执行通知\n\n%s\n\n%s\n\n---\n*%s*", summary, details, time.Now().Format("2006-01-02 15:04:05")),
			},
		}
		payload, _ = json.Marshal(msg)
	} else if strings.Contains(webhookURL, "feishu") || strings.Contains(webhookURL, "open.feishu") {
		// Feishu format
		msg := map[string]interface{}{
			"msg_type": "interactive",
			"card": map[string]interface{}{
				"header": map[string]interface{}{
					"title": map[string]string{"tag": "plain_text", "content": "tssh 执行通知"},
				},
				"elements": []map[string]interface{}{
					{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": fmt.Sprintf("%s\n%s", summary, details)}},
				},
			},
		}
		payload, _ = json.Marshal(msg)
	} else if strings.Contains(webhookURL, "slack") || strings.Contains(webhookURL, "hooks.slack.com") {
		// Slack format
		msg := map[string]interface{}{
			"text": fmt.Sprintf("*tssh 执行通知*\n%s\n```%s```", summary, details),
		}
		payload, _ = json.Marshal(msg)
	} else {
		// Generic webhook
		msg := map[string]interface{}{
			"event":   "tssh.exec.complete",
			"summary": summary,
			"details": details,
			"time":    time.Now().Format(time.RFC3339),
		}
		payload, _ = json.Marshal(msg)
	}

	// Bounded timeout: http.DefaultClient blocks forever on slow/hung webhooks,
	// which would freeze `tssh exec --notify ...` indefinitely.
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("webhook failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}

	return nil
}
