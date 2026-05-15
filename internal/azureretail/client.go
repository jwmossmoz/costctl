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
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the public retail prices endpoint.
	DefaultBaseURL = "https://prices.azure.com/api/retail/prices"

	defaultUserAgent = "costctl (https://github.com/jwmossmoz/costctl)"
	defaultTimeout   = 30 * time.Second
)

// Client wraps the retail prices API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string
}

// New returns a Client with sensible defaults.
func New() *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: defaultTimeout},
		UserAgent:  defaultUserAgent,
	}
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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.UserAgent)
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("azureretail: GET %s: %w", endpoint, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("azureretail: reading response body: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("azureretail: GET %s: %s: %s",
				endpoint, resp.Status, truncate(string(body), 200))
		}
		var p page
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("azureretail: decoding response: %w", err)
		}
		all = append(all, p.Items...)
		endpoint = p.NextPageLink
	}
	return all, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
