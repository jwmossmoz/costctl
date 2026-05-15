package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/jwmossmoz/costctl/internal/azureretail"
	"github.com/jwmossmoz/costctl/internal/cloudprice"
	"github.com/jwmossmoz/costctl/internal/tcadmin"
)

type fakeAzureRetailSpotClient struct {
	items map[string][]azureretail.Item
	err   error
}

func (f fakeAzureRetailSpotClient) SpotPrices(_ context.Context, sku string) ([]azureretail.Item, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items[sku], nil
}

type fakeGCPCurrentClient struct {
	responses map[string]*cloudprice.GCPCurrentResponse
	err       error
}

func (f fakeGCPCurrentClient) GCPCurrent(_ context.Context, machineType string) (*cloudprice.GCPCurrentResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.responses[machineType], nil
}

func TestBuildAzureWorkerPoolReportMarksConfiguredRows(t *testing.T) {
	oldNewClient := newAzureRetailSpotClient
	t.Cleanup(func() { newAzureRetailSpotClient = oldNewClient })
	newAzureRetailSpotClient = func() azureRetailSpotClient {
		return fakeAzureRetailSpotClient{items: map[string][]azureretail.Item{
			"Standard_E8ads_v6": {
				{ArmSkuName: "Standard_E8ads_v6", ArmRegionName: "uksouth", RetailPrice: 0.12, CurrencyCode: "USD", UnitOfMeasure: "1 Hour"},
				{ArmSkuName: "Standard_E8ads_v6", ArmRegionName: "westus2", RetailPrice: 0.19, CurrencyCode: "USD", UnitOfMeasure: "1 Hour"},
			},
		}}
	}

	target := tcadmin.WorkerPoolTarget{
		ID:         "gecko-t/win11-64-25h2-amd",
		ProviderID: "azure2",
		Cloud:      tcadmin.CloudAzure,
		Launches: []tcadmin.LaunchConfig{
			{Region: "uksouth", AzureSKU: "Standard_E8ads_v6"},
		},
		AzureSKUs: []string{"Standard_E8ads_v6"},
		Regions:   []string{"uksouth"},
	}

	report, err := buildAzureWorkerPoolReport(context.Background(), target, "stdin", "any", false)
	if err != nil {
		t.Fatalf("buildAzureWorkerPoolReport: %v", err)
	}
	if len(report.Prices) != 2 {
		t.Fatalf("prices = %d, want 2", len(report.Prices))
	}
	if !report.Prices[0].Configured {
		t.Fatalf("cheapest row should be configured: %+v", report.Prices[0])
	}

	report, err = buildAzureWorkerPoolReport(context.Background(), target, "stdin", "any", true)
	if err != nil {
		t.Fatalf("buildAzureWorkerPoolReport configured-only: %v", err)
	}
	if len(report.Prices) != 1 || report.Prices[0].Region != "uksouth" {
		t.Fatalf("configured-only prices = %+v", report.Prices)
	}
}

func TestBuildGCPWorkerPoolReportRequiresKeyAndMarksConfiguredRows(t *testing.T) {
	oldNewClient := newWorkerPoolGCPClient
	t.Cleanup(func() { newWorkerPoolGCPClient = oldNewClient })

	var resp cloudprice.GCPCurrentResponse
	resp.Data.Currency = "USD"
	resp.Data.OSandSoftware = "Linux"
	resp.Data.Prices = []cloudprice.GCPPrice{
		{Region: "us-central1", PriceSpot: 0.03, PriceOnDemand: 0.09},
		{Region: "us-east1", PriceSpot: 0.04, PriceOnDemand: 0.10},
	}
	newWorkerPoolGCPClient = func(key string) gcpCurrentClient {
		if key != "test-key" {
			return fakeGCPCurrentClient{err: errors.New("wrong key")}
		}
		return fakeGCPCurrentClient{responses: map[string]*cloudprice.GCPCurrentResponse{
			"c3d-standard-8-lssd": &resp,
		}}
	}

	target := tcadmin.WorkerPoolTarget{
		ID:         "gecko-t/t-linux-docker-amd",
		ProviderID: "fxci-level1-gcp",
		Cloud:      tcadmin.CloudGCP,
		Launches: []tcadmin.LaunchConfig{
			{Region: "us-central1", MachineType: "c3d-standard-8-lssd"},
		},
		GCPMachineTypes: []string{"c3d-standard-8-lssd"},
		Regions:         []string{"us-central1"},
	}

	report, err := buildGCPWorkerPoolReport(context.Background(), target, "stdin", "test-key", false)
	if err != nil {
		t.Fatalf("buildGCPWorkerPoolReport: %v", err)
	}
	if len(report.Prices) != 2 {
		t.Fatalf("prices = %d, want 2", len(report.Prices))
	}
	if !report.Prices[0].Configured {
		t.Fatalf("configured row should be marked: %+v", report.Prices[0])
	}
}
