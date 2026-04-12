package models

import (
	"encoding/json"
	"testing"
)

func TestValidEntityTypes(t *testing.T) {
	tests := []struct {
		name      string
		entType   string
		wantValid bool
	}{
		{"agent is valid", "agent", true},
		{"service is valid", "service", true},
		{"empty is invalid", "", false},
		{"unknown type rejected", "bot", false},
		{"case sensitive", "Agent", false},
		{"numeric rejected", "123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidEntityTypes[tt.entType]
			if got != tt.wantValid {
				t.Errorf("ValidEntityTypes[%q] = %v, want %v", tt.entType, got, tt.wantValid)
			}
		})
	}
}

func TestValidPricingModels(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantValid bool
	}{
		{"per_request is valid", "per_request", true},
		{"subscription is valid", "subscription", true},
		{"free is valid", "free", true},
		{"empty is invalid", "", false},
		{"unknown model rejected", "usage_based", false},
		{"case sensitive", "Free", false},
		{"pay_per_use rejected", "pay_per_use", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidPricingModels[tt.model]
			if got != tt.wantValid {
				t.Errorf("ValidPricingModels[%q] = %v, want %v", tt.model, got, tt.wantValid)
			}
		})
	}
}

func TestEntityPricingJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		pricing EntityPricing
	}{
		{
			name: "per_request with rates",
			pricing: EntityPricing{
				Model:          "per_request",
				BasePriceUSD:   0.01,
				Currency:       "USD",
				PaymentMethods: []string{"x402", "stripe"},
				Rates:          map[string]float64{"standard": 0.01, "premium": 0.05},
			},
		},
		{
			name: "subscription model",
			pricing: EntityPricing{
				Model:          "subscription",
				BasePriceUSD:   29.99,
				Currency:       "USD",
				PaymentMethods: []string{"stripe"},
			},
		},
		{
			name: "free model minimal",
			pricing: EntityPricing{
				Model: "free",
			},
		},
		{
			name: "zero price",
			pricing: EntityPricing{
				Model:        "free",
				BasePriceUSD: 0,
				Currency:     "USDC",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.pricing)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var got EntityPricing
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if got.Model != tt.pricing.Model {
				t.Errorf("Model: got %q, want %q", got.Model, tt.pricing.Model)
			}
			if got.BasePriceUSD != tt.pricing.BasePriceUSD {
				t.Errorf("BasePriceUSD: got %f, want %f", got.BasePriceUSD, tt.pricing.BasePriceUSD)
			}
			if got.Currency != tt.pricing.Currency {
				t.Errorf("Currency: got %q, want %q", got.Currency, tt.pricing.Currency)
			}
			if len(got.PaymentMethods) != len(tt.pricing.PaymentMethods) {
				t.Errorf("PaymentMethods length: got %d, want %d", len(got.PaymentMethods), len(tt.pricing.PaymentMethods))
			}
			if len(got.Rates) != len(tt.pricing.Rates) {
				t.Errorf("Rates length: got %d, want %d", len(got.Rates), len(tt.pricing.Rates))
			}
		})
	}
}

func TestEntityPricingJSONFieldNames(t *testing.T) {
	pricing := EntityPricing{
		Model:          "per_request",
		BasePriceUSD:   0.01,
		Currency:       "USD",
		PaymentMethods: []string{"x402"},
		Rates:          map[string]float64{"default": 0.01},
	}

	data, err := json.Marshal(pricing)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	expectedKeys := []string{"model", "base_price_usd", "currency", "payment_methods", "rates"}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q not found", key)
		}
	}

	// Ensure no Go-style field names leak through
	badKeys := []string{"Model", "BasePriceUSD", "Currency", "PaymentMethods", "Rates"}
	for _, key := range badKeys {
		if _, ok := raw[key]; ok {
			t.Errorf("unexpected Go-style key %q in JSON output", key)
		}
	}
}

func TestValidateServiceFields(t *testing.T) {
	tests := []struct {
		name    string
		record  RegistryRecord
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid service with endpoint and pricing",
			record: RegistryRecord{
				ServiceEndpoint: "https://api.example.com/v1",
				EntityPricing: &EntityPricing{
					Model: "per_request",
				},
			},
			wantErr: false,
		},
		{
			name: "valid service with endpoint no pricing",
			record: RegistryRecord{
				ServiceEndpoint: "https://api.example.com/v1",
			},
			wantErr: false,
		},
		{
			name:    "service missing endpoint fails",
			record:  RegistryRecord{},
			wantErr: true,
			errMsg:  "service_endpoint is required",
		},
		{
			name: "service with invalid pricing model fails",
			record: RegistryRecord{
				ServiceEndpoint: "https://api.example.com/v1",
				EntityPricing: &EntityPricing{
					Model: "pay_as_you_go",
				},
			},
			wantErr: true,
			errMsg:  "invalid pricing model",
		},
		{
			name: "service with empty pricing model is ok",
			record: RegistryRecord{
				ServiceEndpoint: "https://api.example.com/v1",
				EntityPricing:   &EntityPricing{},
			},
			wantErr: false,
		},
		{
			name: "service with per_request pricing passes",
			record: RegistryRecord{
				ServiceEndpoint: "https://api.example.com/v1",
				EntityPricing: &EntityPricing{
					Model:        "per_request",
					BasePriceUSD: 0.01,
					Currency:     "USD",
				},
			},
			wantErr: false,
		},
		{
			name: "service with subscription pricing passes",
			record: RegistryRecord{
				ServiceEndpoint: "https://api.example.com/v1",
				EntityPricing: &EntityPricing{
					Model: "subscription",
				},
			},
			wantErr: false,
		},
		{
			name: "service with free pricing passes",
			record: RegistryRecord{
				ServiceEndpoint: "https://api.example.com/v1",
				EntityPricing: &EntityPricing{
					Model: "free",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceFields(&tt.record)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && !containsStr(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
