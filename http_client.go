package capstan

// http_client.go — shared HTTP plumbing for all provider implementations.
//
// Before this file existed, each of the four providers (hetzner, do, linode,
// vultr) carried a private (get, post, del) trio that was 95% identical:
// same Bearer auth, same Accept/Content-Type headers, same error wrapping
// shape, same body handling. That's ~60 lines × 4 providers of pure
// duplication, and every new provider would pay it again.
//
// httpClient takes baseURL on each call (not at construction) so existing
// tests can keep using the `provider.spec = &ProviderSpec{BaseURL: srv.URL}`
// override pattern. Switching to spec-at-construction would force every
// test to also update the httpClient — not worth the API neatness.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type httpClient struct {
	name   string // provider identifier for error prefix
	token  string
	client *http.Client
}

func newHTTPClient(name, token string) *httpClient {
	return &httpClient{
		name:   name,
		token:  token,
		client: &http.Client{},
	}
}

func (c *httpClient) GET(ctx context.Context, baseURL, path string, out any) error {
	return c.do(ctx, "GET", baseURL, path, nil, out)
}

func (c *httpClient) POST(ctx context.Context, baseURL, path string, payload, out any) error {
	return c.do(ctx, "POST", baseURL, path, payload, out)
}

func (c *httpClient) DELETE(ctx context.Context, baseURL, path string) error {
	return c.do(ctx, "DELETE", baseURL, path, nil, nil)
}

func (c *httpClient) do(ctx context.Context, method, baseURL, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != "DELETE" {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("capstan: %s %s %s: %w", c.name, method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("capstan: %s %s %s: %d %s", c.name, method, path, resp.StatusCode, respBody)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(respBody, out)
}
