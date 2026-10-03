// Package mockclient 调用模拟网络测试/分机迁移接口的 HTTP 客户端
package mockclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{base: base, http: &http.Client{Timeout: 5 * time.Second}}
}

type NetResult struct {
	OK        bool   `json:"ok"`
	Port      string `json:"port"`
	Reason    string `json:"reason"`
	LatencyMS int    `json:"latency_ms"`
	Detail    string `json:"detail"`
}

type ExtResult struct {
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail"`
}

func (c *Client) post(ctx context.Context, path string, body any) ([]byte, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("模拟接口不可达: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("模拟接口返回 %s", resp.Status)
	}
	var out bytes.Buffer
	if _, err := out.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (c *Client) NetworkTest(ctx context.Context, port string) (NetResult, error) {
	raw, err := c.post(ctx, "/mock/network/test", map[string]string{"port": port})
	if err != nil {
		return NetResult{}, err
	}
	var res NetResult
	err = json.Unmarshal(raw, &res)
	return res, err
}

func (c *Client) SetPort(ctx context.Context, port string, enabled bool, reason string) error {
	_, err := c.post(ctx, "/mock/network/set", map[string]any{
		"port": port, "enabled": enabled, "reason": reason,
	})
	return err
}

func (c *Client) MigrateExt(ctx context.Context, orderID, fromExt, toExt string) (ExtResult, error) {
	raw, err := c.post(ctx, "/mock/phone/migrate", map[string]string{
		"order_id": orderID, "from_ext": fromExt, "to_ext": toExt,
	})
	if err != nil {
		return ExtResult{}, err
	}
	var res ExtResult
	err = json.Unmarshal(raw, &res)
	return res, err
}

func (c *Client) ReleaseExt(orderID string) {
	// 取消/改期释放分机：通过一次以空目标的迁移释放不了占用，
	// 这里直接请求专用的内部释放端点（见 mocksvc）。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = c.post(ctx, "/mock/phone/release", map[string]string{"order_id": orderID})
}
