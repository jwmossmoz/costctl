// Package cloudprice is a thin client for cloudprice.net's AzurePrice API v1.
//
// The base URL is https://data.cloudprice.net. Authentication is via a
// subscription key passed as the `subscription-key` query parameter.
// Sign up and find your key at https://developer.cloudprice.net/.
package cloudprice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	// DefaultBaseURL is the gateway host. Override for tests via Client.BaseURL.
	DefaultBaseURL = "https://data.cloudprice.net"

	defaultUserAgent = "costctl (https://github.com/jwmossmoz/costctl)"
	defaultTimeout   = 30 * time.Second

	// Retry knobs for HTTP 429 responses. Cloudprice returns 429 under modest
	// concurrent load (~6 parallel requests is enough to trip it). Defaults
	// recover transparently for typical batch workloads.
	defaultMaxRetries  = 4
	defaultBaseBackoff = 500 * time.Millisecond
	defaultMaxBackoff  = 10 * time.Second
)

// Errors surfaced by the client.
var (
	ErrUnauthorized = errors.New("cloudprice: unauthorized (check subscription key)")
	ErrNotFound     = errors.New("cloudprice: resource not found")
)

// Client talks to data.cloudprice.net.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	UserAgent  string

	// MaxRetries on HTTP 429. 0 means "use default" (defaultMaxRetries).
	// Set to -1 to disable retries entirely.
	MaxRetries int
	// BaseBackoff is the initial wait before a retry; doubles each attempt.
	// 0 means "use default" (defaultBaseBackoff).
	BaseBackoff time.Duration
	// MaxBackoff caps the exponential growth. 0 means "use default"
	// (defaultMaxBackoff). Honored Retry-After headers can still exceed this.
	MaxBackoff time.Duration

	// UseCache enables transparent on-disk caching of 200 responses.
	UseCache bool
	// CacheTTL is the freshness window. 0 means use DefaultCacheTTL.
	CacheTTL time.Duration
	// CacheDir overrides the default $XDG_CACHE_HOME/costctl path.
	CacheDir string
}

// New returns a Client wired with sensible defaults. Cache is enabled with
// the 24-hour DefaultCacheTTL — cloudprice updates daily, so this is safe.
func New(apiKey string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: defaultTimeout},
		UserAgent:  defaultUserAgent,
		UseCache:   true,
		CacheTTL:   DefaultCacheTTL,
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

// backoffFor returns the wait time for the given retry attempt (0-indexed),
// preferring an upstream Retry-After header when present.
func (c *Client) backoffFor(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	d := c.baseBackoff() << attempt
	if d > c.maxBackoff() {
		d = c.maxBackoff()
	}
	return d
}

// HistoryItem is one row of price history.
type HistoryItem struct {
	Name         string  `json:"name"`
	LinuxPrice   float64 `json:"linuxPrice"`
	WindowsPrice float64 `json:"windowsPrice"`
	RegionID     string  `json:"regionId"`
	RegionName   string  `json:"regionName"`
	ModifiedDate string  `json:"modifiedDate"`
	Upload       int     `json:"_upload"`
}

// HistoryResponse is the parsed response from /api/v1/price_history_vm.
type HistoryResponse struct {
	Currency      string        `json:"currency"`
	Regions       []string      `json:"regions"`
	Tier          string        `json:"tier"`
	PaymentType   string        `json:"paymentType"`
	FromDate      string        `json:"fromDate"`
	ToDate        string        `json:"toDate"`
	NumberOfItems int           `json:"numberOfItems"`
	LastUpdate    string        `json:"lastUpdateDate"`
	Items         []HistoryItem `json:"listHistoryPriceValues"`
}

// PriceHistory fetches ~90 days of price change-points for one (sku, region, tier).
// tier is one of "spot", "standard", "low".
// Pass region="" to accept the API default (currently westus2).
func (c *Client) PriceHistory(ctx context.Context, sku, region, tier string) (*HistoryResponse, error) {
	if c.APIKey == "" {
		return nil, errors.New("cloudprice: APIKey is empty")
	}
	if sku == "" {
		return nil, errors.New("cloudprice: sku is required")
	}
	q := url.Values{}
	q.Set("vmname", sku)
	if tier != "" {
		q.Set("tier", tier)
	}
	if region != "" {
		q.Set("regions", region)
	}
	q.Set("subscription-key", c.APIKey)

	endpoint := c.BaseURL + "/api/v1/price_history_vm?" + q.Encode()
	var out HistoryResponse
	if err := c.doJSON(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- GCP (CloudPrice API v2) -------------------------------------------------

// GCPHistoryItem is one row of the GCP compute history response. Each row
// carries all four price tiers; pick the one you want at render time.
type GCPHistoryItem struct {
	Region             string  `json:"Region"`
	InstanceFamilyName string  `json:"InstanceFamilyName"`
	InstanceType       string  `json:"InstanceType"`
	OSandSoftware      string  `json:"OSandSoftware"`
	CreatedYYYYMMDD    int     `json:"CreatedYYYYMMDD"`
	CreatedDateTime    string  `json:"CreatedDateTime"`
	PriceOnDemand      float64 `json:"PriceOnDemand"`
	PriceCommit1Yr     float64 `json:"PriceCommit1Yr"`
	PriceCommit3Yr     float64 `json:"PriceCommit3Yr"`
	PriceSpot          float64 `json:"PriceSpot"`
}

// GCPHistoryResponse is the v2 history envelope. `Data` may be empty when the
// API returns no rows for the given filter.
type GCPHistoryResponse struct {
	Status                    string `json:"Status"`
	ExecutionTimeMilliseconds int    `json:"ExecutionTimeMilliseconds"`
	Data                      struct {
		Currency      string           `json:"Currency"`
		Region        string           `json:"Region"`
		OSandSoftware string           `json:"OSandSoftware"`
		StartDate     string           `json:"StartDate"`
		EndDate       string           `json:"EndDate"`
		Items         []GCPHistoryItem `json:"Items"`
	} `json:"Data"`
}

// GCPCurrentResponse is the v2 single-instance pricing envelope.
type GCPCurrentResponse struct {
	Status string `json:"Status"`
	Data   struct {
		Currency      string     `json:"Currency"`
		OSandSoftware string     `json:"OSandSoftware"`
		UpdatedAt     string     `json:"UpdatedAt"`
		Prices        []GCPPrice `json:"Prices"`
	} `json:"Data"`
}

// GCPPrice is a per-region current-price row.
type GCPPrice struct {
	Region             string  `json:"Region"`
	InstanceFamilyName string  `json:"InstanceFamilyName"`
	InstanceType       string  `json:"InstanceType"`
	PriceOnDemand      float64 `json:"PriceOnDemand"`
	PriceCommit1Yr     float64 `json:"PriceCommit1Yr"`
	PriceCommit3Yr     float64 `json:"PriceCommit3Yr"`
	PriceSpot          float64 `json:"PriceSpot"`
}

// GCPHistory fetches per-day price snapshots for one machine type in one region.
// startDate is an inclusive YYYYMMDD (e.g. "20260211"). Empty means "API default"
// which today is the last ~3 days only.
func (c *Client) GCPHistory(ctx context.Context, machineType, region, startDate string) (*GCPHistoryResponse, error) {
	if c.APIKey == "" {
		return nil, errors.New("cloudprice: APIKey is empty")
	}
	if machineType == "" {
		return nil, errors.New("cloudprice: machineType is required")
	}
	q := url.Values{}
	if region != "" {
		q.Set("region", region)
	}
	if startDate != "" {
		q.Set("startDate", startDate)
	}
	q.Set("subscription-key", c.APIKey)
	endpoint := fmt.Sprintf("%s/api/v2/gcp/compute/instances/%s/history?%s",
		c.BaseURL, url.PathEscape(machineType), q.Encode())

	var out GCPHistoryResponse
	if err := c.doJSON(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GCPCurrent fetches current prices for one machine type across all regions.
func (c *Client) GCPCurrent(ctx context.Context, machineType string) (*GCPCurrentResponse, error) {
	if c.APIKey == "" {
		return nil, errors.New("cloudprice: APIKey is empty")
	}
	if machineType == "" {
		return nil, errors.New("cloudprice: machineType is required")
	}
	q := url.Values{}
	q.Set("subscription-key", c.APIKey)
	endpoint := fmt.Sprintf("%s/api/v2/gcp/compute/instances/%s?%s",
		c.BaseURL, url.PathEscape(machineType), q.Encode())

	var out GCPCurrentResponse
	if err := c.doJSON(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// doJSON executes a GET, consults the disk cache, maps status codes, retries
// 429s with backoff, and decodes JSON into v.
func (c *Client) doJSON(ctx context.Context, endpoint string, v any) error {
	if body, status, ok := c.readCache(endpoint); ok && status == http.StatusOK {
		if err := json.Unmarshal(body, v); err != nil {
			return fmt.Errorf("cloudprice: decoding cached response: %w", err)
		}
		return nil
	}

	maxRetries := c.maxRetries()
	for attempt := 0; ; attempt++ {
		body, status, retryAfter, err := c.doOnce(ctx, endpoint)
		if err != nil {
			return err
		}
		switch status {
		case http.StatusOK:
			c.writeCache(endpoint, status, body)
			if err := json.Unmarshal(body, v); err != nil {
				return fmt.Errorf("cloudprice: decoding response: %w", err)
			}
			return nil
		case http.StatusUnauthorized:
			return ErrUnauthorized
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusTooManyRequests:
			if attempt >= maxRetries {
				return fmt.Errorf("cloudprice: 429 Too Many Requests after %d retries: %s",
					maxRetries, truncate(string(body), 200))
			}
			wait := c.backoffFor(attempt, retryAfter)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		default:
			return fmt.Errorf("cloudprice: GET: HTTP %d: %s",
				status, truncate(string(body), 200))
		}
	}
}

// doOnce performs one GET and returns (body, status, retryAfter, err).
// Caller decides retry policy.
func (c *Client) doOnce(ctx context.Context, endpoint string) ([]byte, int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("cloudprice: GET: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, resp.Header.Get("Retry-After"), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
