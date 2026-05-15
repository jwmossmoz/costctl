// Package azureretail is a thin client for the Azure Retail Prices API.
//
// Docs: https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices
// The endpoint is unauthenticated and returns only currently effective prices —
// not historical data. Pair with package cloudprice for history.
package azureretail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the public retail prices endpoint.
	DefaultBaseURL = "https://prices.azure.com/api/retail/prices"

	defaultUserAgent = "costctl (https://github.com/jwmossmoz/costctl)"
	defaultTimeout   = 30 * time.Second

	defaultMaxRetries  = 4
	defaultBaseBackoff = 500 * time.Millisecond
	defaultMaxBackoff  = 10 * time.Second
)

// Client wraps the retail prices API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string

	// MaxRetries on transient throttling/service-availability responses. 0 means
	// "use default" (defaultMaxRetries). Set to -1 to disable retries entirely.
	MaxRetries int
	// BaseBackoff is the initial wait before a retry; doubles each attempt.
	// 0 means "use default" (defaultBaseBackoff).
	BaseBackoff time.Duration
	// MaxBackoff caps exponential backoff. 0 means "use default" (defaultMaxBackoff).
	// Honored upstream retry headers can still exceed this.
	MaxBackoff time.Duration
}

// New returns a Client with sensible defaults.
func New() *Client {
	return &Client{
		BaseURL:     DefaultBaseURL,
		HTTPClient:  &http.Client{Timeout: defaultTimeout},
		UserAgent:   defaultUserAgent,
		BaseBackoff: defaultBaseBackoff,
		MaxBackoff:  defaultMaxBackoff,
	}
}

func (c *Client) maxRetries() int {
	if c.MaxRetries == 0 {
		return defaultMaxRetries
	}
	if c.MaxRetries < 0 {
		return 0
	}
	return c.MaxRetries
}

func (c *Client) baseBackoff() time.Duration {
	if c.BaseBackoff <= 0 {
		return defaultBaseBackoff
	}
	return c.BaseBackoff
}

func (c *Client) maxBackoff() time.Duration {
	if c.MaxBackoff <= 0 {
		return defaultMaxBackoff
	}
	return c.MaxBackoff
}

func (c *Client) backoffFor(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if d, ok := parseRetryAfter(retryAfter); ok {
			return d
		}
	}
	d := c.baseBackoff() << attempt
	if d > c.maxBackoff() {
		d = c.maxBackoff()
	}
	return d
}

// Item mirrors one record from the API's `Items` array.
type Item struct {
	CurrencyCode       string  `json:"currencyCode"`
	RetailPrice        float64 `json:"retailPrice"`
	UnitPrice          float64 `json:"unitPrice"`
	ArmRegionName      string  `json:"armRegionName"`
	Location           string  `json:"location"`
	EffectiveStartDate string  `json:"effectiveStartDate"`
	MeterID            string  `json:"meterId"`
	MeterName          string  `json:"meterName"`
	ProductName        string  `json:"productName"`
	SkuName            string  `json:"skuName"`
	ServiceName        string  `json:"serviceName"`
	ServiceFamily      string  `json:"serviceFamily"`
	UnitOfMeasure      string  `json:"unitOfMeasure"`
	Type               string  `json:"type"`
	ArmSkuName         string  `json:"armSkuName"`
}

type page struct {
	Items        []Item `json:"Items"`
	NextPageLink string `json:"NextPageLink"`
}

// SpotPrices returns all current Spot meters for the given armSkuName.
// Each VM family typically has two meters per region (Linux + Windows).
func (c *Client) SpotPrices(ctx context.Context, armSkuName string) ([]Item, error) {
	filter := strings.Join([]string{
		fmt.Sprintf("armSkuName eq '%s'", armSkuName),
		"serviceName eq 'Virtual Machines'",
		"priceType eq 'Consumption'",
		"contains(meterName, 'Spot')",
	}, " and ")
	return c.query(ctx, filter)
}

func (c *Client) query(ctx context.Context, filter string) ([]Item, error) {
	endpoint := c.BaseURL + "?$filter=" + url.QueryEscape(filter)
	var all []Item
	for endpoint != "" {
		p, err := c.getPage(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		all = append(all, p.Items...)
		endpoint = p.NextPageLink
	}
	return all, nil
}

func (c *Client) getPage(ctx context.Context, endpoint string) (page, error) {
	maxRetries := c.maxRetries()
	for attempt := 0; ; attempt++ {
		body, status, retryAfter, err := c.doOnce(ctx, endpoint)
		if err != nil {
			return page{}, err
		}
		if status == http.StatusOK {
			var p page
			if err := json.Unmarshal(body, &p); err != nil {
				return page{}, fmt.Errorf("azureretail: decoding response: %w", err)
			}
			return p, nil
		}
		if isRetryableStatus(status) && attempt < maxRetries {
			wait := c.backoffFor(attempt, retryAfter)
			select {
			case <-ctx.Done():
				return page{}, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if isRetryableStatus(status) {
			return page{}, fmt.Errorf("azureretail: GET %s: HTTP %d after %d retries: %s",
				endpoint, status, maxRetries, truncate(string(body), 200))
		}
		return page{}, fmt.Errorf("azureretail: GET %s: HTTP %d: %s",
			endpoint, status, truncate(string(body), 200))
	}
}

func (c *Client) doOnce(ctx context.Context, endpoint string) ([]byte, int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("azureretail: GET %s: %w", endpoint, err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, resp.StatusCode, retryAfter(resp.Header),
			fmt.Errorf("azureretail: reading response body: %w", err)
	}
	return body, resp.StatusCode, retryAfter(resp.Header), nil
}

func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
}

func retryAfter(h http.Header) string {
	for _, name := range []string{
		"x-ms-ratelimit-microsoft.consumption-retry-after",
		"Retry-After",
	} {
		if v := h.Get(name); v != "" {
			return v
		}
	}
	return ""
}

func parseRetryAfter(v string) (time.Duration, bool) {
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0, true
		}
		return d, true
	}
	return 0, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
