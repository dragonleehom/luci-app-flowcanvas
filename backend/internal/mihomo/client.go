package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

type Client struct {
	baseURL    *url.URL
	secret     string
	httpClient *http.Client
}

func NewClient(rawBaseURL, secret string, httpClient *http.Client) (*Client, error) {
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse mihomo controller URL: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("mihomo controller URL must use http or https")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{baseURL: baseURL, secret: secret, httpClient: httpClient}, nil
}

func (c *Client) Proxies(ctx context.Context) (ProxyCatalogResponse, error) {
	var response ProxyCatalogResponse
	requestURL := c.endpoint("/proxies", nil)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return response, fmt.Errorf("build mihomo proxies request: %w", err)
	}
	c.authorize(request.Header)
	res, err := c.httpClient.Do(request)
	if err != nil {
		return response, fmt.Errorf("request mihomo proxies: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
		return response, fmt.Errorf("mihomo proxies returned %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return response, fmt.Errorf("decode mihomo proxies response: %w", err)
	}
	return response, nil
}

type ConfigReloadRequest struct {
	Path    string `json:"path"`
	Payload string `json:"payload"`
}

func (c *Client) ReloadConfig(ctx context.Context, path, payload string) error {
	body, err := json.Marshal(ConfigReloadRequest{Path: path, Payload: payload})
	if err != nil {
		return fmt.Errorf("encode Mihomo config reload request: %w", err)
	}
	query := url.Values{}
	query.Set("force", "true")
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint("/configs", query), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Mihomo config reload request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	c.authorize(request.Header)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Mihomo config reload: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		return fmt.Errorf("Mihomo config reload returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	return nil
}

func (c *Client) StreamConnections(
	ctx context.Context,
	interval time.Duration,
	onSnapshot func(ConnectionSnapshot) error,
) error {
	if interval <= 0 {
		interval = time.Second
	}
	query := url.Values{}
	query.Set("interval", strconv.FormatInt(interval.Milliseconds(), 10))
	requestURL := c.websocketEndpoint("/connections", query)
	header := http.Header{}
	c.authorize(header)
	conn, _, err := websocket.Dial(ctx, requestURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return fmt.Errorf("dial mihomo connections websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "flowcanvas watcher stopped")

	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read mihomo connections websocket: %w", err)
		}
		var snapshot ConnectionSnapshot
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			return fmt.Errorf("decode mihomo connections snapshot: %w", err)
		}
		if err := onSnapshot(snapshot); err != nil {
			return err
		}
	}
}

func (c *Client) endpoint(path string, query url.Values) string {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

func (c *Client) websocketEndpoint(path string, query url.Values) string {
	endpoint := *c.baseURL
	switch endpoint.Scheme {
	case "https":
		endpoint.Scheme = "wss"
	default:
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

func (c *Client) authorize(header http.Header) {
	if c.secret != "" {
		header.Set("Authorization", "Bearer "+c.secret)
	}
}
