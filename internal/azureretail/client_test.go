package azureretail

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpotPrices_BuildsFilterAndParsesItems(t *testing.T) {
	var gotFilter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("$filter")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"Items": [
				{"armSkuName":"Standard_F8s_v2","armRegionName":"westus2",
				 "retailPrice":0.13,"currencyCode":"USD","unitOfMeasure":"1 Hour",
				 "meterName":"F8s v2 Spot","productName":"Virtual Machines FS Series",
				 "serviceName":"Virtual Machines","type":"Consumption"}
			]
		}`))
	}))
	t.Cleanup(srv.Close)

	c := New()
	c.BaseURL = srv.URL

	items, err := c.SpotPrices(context.Background(), "Standard_F8s_v2")
	if err != nil {
		t.Fatalf("SpotPrices: %v", err)
	}
	if len(items) != 1 || items[0].RetailPrice != 0.13 {
		t.Fatalf("items mis-parsed: %+v", items)
	}
	for _, want := range []string{
		"armSkuName eq 'Standard_F8s_v2'",
		"serviceName eq 'Virtual Machines'",
		"priceType eq 'Consumption'",
		"contains(meterName, 'Spot')",
	} {
		if !strings.Contains(gotFilter, want) {
			t.Errorf("filter missing %q; got %q", want, gotFilter)
		}
	}
}

func TestSpotPrices_FollowsNextPageLink(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "":
			next := srv.URL + "/?page=2"
			_, _ = fmt.Fprintf(w, `{"Items":[{"armSkuName":"A","retailPrice":0.1}],"NextPageLink":%q}`, next)
		case "2":
			_, _ = w.Write([]byte(`{"Items":[{"armSkuName":"B","retailPrice":0.2}]}`))
		}
	}))
	t.Cleanup(srv.Close)

	c := New()
	c.BaseURL = srv.URL

	items, err := c.SpotPrices(context.Background(), "X")
	if err != nil {
		t.Fatalf("SpotPrices: %v", err)
	}
	if len(items) != 2 || items[0].ArmSkuName != "A" || items[1].ArmSkuName != "B" {
		t.Fatalf("pagination mis-handled: %+v", items)
	}
}

func TestSpotPrices_NonOKStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New()
	c.BaseURL = srv.URL
	if _, err := c.SpotPrices(context.Background(), "X"); err == nil {
		t.Error("expected error on 500, got nil")
	}
}
