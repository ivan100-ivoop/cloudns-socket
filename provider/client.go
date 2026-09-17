package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Checker exposes the single operation supported by this module.
type Checker interface {
	Check(ctx context.Context, domain string) (Result, error)
}

// Client is a reusable, registrar-agnostic domain availability checker.
type Client struct {
	cfg        Config
	httpClient *http.Client
	debugLog   *debugLogger
}

// New validates the configuration and returns a ready-to-use client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Method) == "" {
		return nil, errors.New("provider: method is required")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("provider: endpoint is required")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("provider: timeout must be greater than zero")
	}
	if cfg.DefaultResult == "" {
		cfg.DefaultResult = ResultError
	}
	if cfg.DefaultResult != ResultAvailable && cfg.DefaultResult != ResultNotAvailable && cfg.DefaultResult != ResultError {
		return nil, fmt.Errorf("provider: invalid default result %q", cfg.DefaultResult)
	}
	for i, rule := range cfg.OutcomeRules {
		if rule.Result != ResultAvailable && rule.Result != ResultNotAvailable && rule.Result != ResultError {
			return nil, fmt.Errorf("provider: invalid result in rule %d: %q", i, rule.Result)
		}
		if err := rule.validate(); err != nil {
			return nil, fmt.Errorf("provider: invalid rule %d: %w", i, err)
		}
	}

	httpClient := &http.Client{
		Timeout: cfg.Timeout,
	}
	if !cfg.FollowRedirects {
		httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	client := &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}
	if cfg.Debug {
		debugLog, err := newDebugLogger(cfg.DebugLogPath)
		if err != nil {
			return nil, err
		}
		client.debugLog = debugLog
	}
	return client, nil
}

// Close releases the debug log file. It is safe to call on a client without debug logging.
func (c *Client) Close() error {
	if c == nil || c.debugLog == nil {
		return nil
	}
	return c.debugLog.Close()
}

// Check executes the configured HTTP request for the given domain and returns
// a standardized result.
func (c *Client) Check(ctx context.Context, domain string) (Result, error) {
	if c == nil {
		return ResultError, errors.New("provider: nil client")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	parts, err := parseDomain(domain)
	if err != nil {
		return ResultError, err
	}
	return c.checkParts(ctx, parts)
}

// CheckParts checks a normalized domain with registry-selected extension parts.
func (c *Client) CheckParts(ctx context.Context, parts DomainParts) (Result, error) {
	if c == nil {
		return ResultError, errors.New("provider: nil client")
	}
	return c.checkParts(ctx, parts)
}

func (c *Client) checkParts(ctx context.Context, parts DomainParts) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	req, err := c.buildRequest(ctx, parts)
	if err != nil {
		c.debugf("request build failed domain=%q error=%v", parts.Domain, err)
		return ResultError, err
	}
	c.debugf("request method=%s url=%s headers=%v body=%q", req.Method, req.URL.String(), safeHeaders(req.Header), safeRequestBody(req))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.debugf("request failed error=%v", err)
		return ResultError, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		c.debugf("response body read failed error=%v", err)
		return ResultError, err
	}

	snapshot := Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
		Body:       string(bodyBytes),
	}
	result := c.classify(snapshot, parts)
	c.debugf("response status=%d headers=%v body=%q result=%s", snapshot.StatusCode, safeHeaders(resp.Header), safeResponseBody(resp.Header, snapshot.Body), result)
	return result, nil
}

func (c *Client) buildRequest(ctx context.Context, parts DomainParts) (*http.Request, error) {
	data := renderData{
		Domain:    parts.Domain,
		Name:      parts.Name,
		Extension: parts.Extension,
		Values:    c.cfg.Variables,
		RawInput:  parts.Domain,
	}
	endpoint, err := renderString(c.cfg.Endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("provider: render endpoint: %w", err)
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("provider: parse endpoint: %w", err)
	}

	query := u.Query()
	for key, value := range c.cfg.QueryParams {
		rendered, err := renderString(value, data)
		if err != nil {
			return nil, fmt.Errorf("provider: render query param %q: %w", key, err)
		}
		query.Set(key, rendered)
	}
	u.RawQuery = query.Encode()

	var bodyReader io.Reader
	headers := cloneStringMap(c.cfg.Headers)
	if len(c.cfg.FormParams) > 0 && strings.TrimSpace(c.cfg.Body) == "" {
		form := url.Values{}
		for key, value := range c.cfg.FormParams {
			for _, item := range value {
				rendered, err := renderString(item, data)
				if err != nil {
					return nil, fmt.Errorf("provider: render form param %q: %w", key, err)
				}
				form.Add(key, rendered)
			}
		}
		bodyReader = strings.NewReader(form.Encode())
		if _, ok := headers["Content-Type"]; !ok {
			// Form submissions need an explicit content type when it is not configured.
			headers["Content-Type"] = "application/x-www-form-urlencoded"
		}
	} else if strings.TrimSpace(c.cfg.Body) != "" {
		renderedBody, err := renderString(c.cfg.Body, data)
		if err != nil {
			return nil, fmt.Errorf("provider: render body: %w", err)
		}
		bodyReader = bytes.NewBufferString(renderedBody)
	}

	req, err := http.NewRequestWithContext(ctx, c.cfg.Method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("provider: create request: %w", err)
	}

	for key, value := range headers {
		rendered, err := renderString(value, data)
		if err != nil {
			return nil, fmt.Errorf("provider: render header %q: %w", key, err)
		}
		req.Header.Set(key, rendered)
	}

	return req, nil
}

func (c *Client) classify(resp Response, parts DomainParts) Result {
	for _, rule := range c.cfg.OutcomeRules {
		if rule.matches(resp, parts) {
			return rule.Result
		}
	}
	return c.cfg.DefaultResult
}

func (r OutcomeRule) validate() error {
	for _, code := range r.StatusCodes {
		if code < 100 || code > 999 {
			return fmt.Errorf("invalid status code %d", code)
		}
	}
	for _, rng := range r.StatusCodeRanges {
		if rng.Min < 100 || rng.Max > 999 || rng.Min > rng.Max {
			return fmt.Errorf("invalid status code range %+v", rng)
		}
	}
	return nil
}

func (r OutcomeRule) matches(resp Response, parts DomainParts) bool {
	if len(r.StatusCodes) > 0 && !containsInt(r.StatusCodes, resp.StatusCode) {
		return false
	}
	if len(r.StatusCodeRanges) > 0 {
		matched := false
		for _, rng := range r.StatusCodeRanges {
			if resp.StatusCode >= rng.Min && resp.StatusCode <= rng.Max {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for key, expected := range r.HeaderEquals {
		if actual := firstHeader(resp.Headers, key); actual != expected {
			return false
		}
	}
	for key, expectedFragment := range r.HeaderContains {
		if actual := firstHeader(resp.Headers, key); !strings.Contains(actual, expectedFragment) {
			return false
		}
	}
	if r.BodyEquals != "" && resp.Body != r.BodyEquals {
		return false
	}
	for _, fragment := range r.BodyContains {
		if !strings.Contains(resp.Body, fragment) {
			return false
		}
	}
	for _, fragment := range r.BodyNotContains {
		if strings.Contains(resp.Body, fragment) {
			return false
		}
	}
	if strings.TrimSpace(r.BodyRegex) != "" {
		matched, err := matchRegex(r.BodyRegex, resp.Body)
		if err != nil || !matched {
			return false
		}
	}
	if r.JSONValue != nil {
		matched, err := matchJSONValue(resp.Body, r.JSONValue, renderData{
			Domain:    parts.Domain,
			Name:      parts.Name,
			Extension: parts.Extension,
		})
		if err != nil || !matched {
			return false
		}
	}
	return true
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstHeader(headers map[string][]string, key string) string {
	for headerKey, values := range headers {
		if strings.EqualFold(headerKey, key) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// renderData is the template context used across all request parts.
type renderData struct {
	Domain    string
	Name      string
	Extension string
	Values    map[string]string
	RawInput  string
}

func withDefaultValues(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}
