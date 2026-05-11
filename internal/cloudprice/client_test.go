package cloudprice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("test-key")
	c.BaseURL = srv.URL
	return c, srv
}

func TestPriceHistory_HappyPath(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/v1/price_history_vm" {
			t.Errorf("path = %q", got)
		}
		q := r.URL.Query()
		for _, k := range []string{"vmname", "tier", "regions", "subscription-key"} {
			if q.Get(k) == "" {
				t.Errorf("missing query param %q", k)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"currency": "USD",
			"regions": ["westus2"],
			"tier": "spot",
			"paymentType": "payasyougo",
			"numberOfItems": 2,
			"listHistoryPriceValues": [
				{"name":"Standard_F8s_v2","linuxPrice":0.10,"windowsPrice":0.20,
				 "regionId":"westus2","regionName":"West US 2",
				 "modifiedDate":"2026-05-08 00:00:00"},
				{"name":"Standard_F8s_v2","linuxPrice":0.09,"windowsPrice":0.18,
				 "regionId":"westus2","regionName":"West US 2",
				 "modifiedDate":"2026-04-01 00:00:00"}
			]
		}`))
	})

	got, err := c.PriceHistory(context.Background(), "Standard_F8s_v2", "westus2", "spot")
	if err != nil {
		t.Fatalf("PriceHistory: %v", err)
	}
	if got.Tier != "spot" || got.NumberOfItems != 2 {
		t.Errorf("unexpected response: %+v", got)
	}
	if len(got.Items) != 2 || got.Items[0].LinuxPrice != 0.10 {
		t.Errorf("items mis-parsed: %+v", got.Items)
	}
}

func TestPriceHistory_Unauthorized(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"statusCode":401}`, http.StatusUnauthorized)
	})
	_, err := c.PriceHistory(context.Background(), "X", "westus2", "spot")
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v; want ErrUnauthorized", err)
	}
}

func TestPriceHistory_NotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"statusCode":404}`, http.StatusNotFound)
	})
	_, err := c.PriceHistory(context.Background(), "X", "westus2", "spot")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v; want ErrNotFound", err)
	}
}

func TestPriceHistory_RejectsEmptyInput(t *testing.T) {
	c := New("k")
	if _, err := c.PriceHistory(context.Background(), "", "westus2", "spot"); err == nil {
		t.Error("expected error for empty sku, got nil")
	}
	c2 := New("")
	if _, err := c2.PriceHistory(context.Background(), "X", "westus2", "spot"); err == nil {
		t.Error("expected error for empty key, got nil")
	}
}
