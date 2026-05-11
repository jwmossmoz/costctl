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
	"time"
)

const (
	// DefaultBaseURL is the gateway host. Override for tests via Client.BaseURL.
	DefaultBaseURL = "https://data.cloudprice.net"

	defaultUserAgent = "costctl (https://github.com/jwmossmoz/costctl)"
	defaultTimeout   = 30 * time.Second
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
}

// New returns a Client wired with sensible defaults.
func New(apiKey string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: defaultTimeout},
		UserAgent:  defaultUserAgent,
	}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudprice: GET price_history_vm: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("cloudprice: GET price_history_vm: %s: %s",
			resp.Status, truncate(string(body), 200))
	}

	var out HistoryResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("cloudprice: decoding response: %w", err)
	}
	return &out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
