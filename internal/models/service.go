package models

import "fmt"

// ServicePricing describes how a service charges for usage.
type ServicePricing struct {
	Model          string             `json:"model"`
	BasePriceUSD   float64            `json:"base_price_usd"`
	Currency       string             `json:"currency"`
	PaymentMethods []string           `json:"payment_methods"`
	Rates          map[string]float64 `json:"rates,omitempty"`
}

// ValidPricingModels enumerates accepted pricing model values.
var ValidPricingModels = map[string]bool{
	"per_request":  true,
	"subscription": true,
	"free":         true,
}

// ValidEntityTypes enumerates accepted entity_type values.
var ValidEntityTypes = map[string]bool{
	"agent":   true,
	"service": true,
}

// ValidateServiceFields checks service-specific invariants on a RegistryRecord.
// service_endpoint is required for service entities; pricing model must be recognized.
func ValidateServiceFields(r *RegistryRecord) error {
	if r.ServiceEndpoint == "" {
		return fmt.Errorf("service_endpoint is required for entity_type 'service'")
	}
	if r.ServicePricing != nil && r.ServicePricing.Model != "" {
		if !ValidPricingModels[r.ServicePricing.Model] {
			return fmt.Errorf("invalid pricing model: %s (must be per_request, subscription, or free)", r.ServicePricing.Model)
		}
	}
	return nil
}
