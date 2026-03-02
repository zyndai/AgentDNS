// Package trust implements the EigenTrust reputation algorithm for agent scoring.
package trust

import (
	"math"
	"time"

	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/store"
)

// EigenTrust computes global trust scores using the EigenTrust algorithm.
// Trust is transitive but attenuated: if Registry A trusts Registry B,
// and Registry B observes an agent, that observation carries weight
// proportional to A's trust in B.
type EigenTrust struct {
	store      store.Store
	iterations int
	// registryTrust maps registry_id -> trust weight (0-1)
	registryTrust map[string]float64
}

// NewEigenTrust creates a new EigenTrust calculator.
func NewEigenTrust(st store.Store, iterations int) *EigenTrust {
	if iterations <= 0 {
		iterations = 5
	}
	return &EigenTrust{
		store:         st,
		iterations:    iterations,
		registryTrust: make(map[string]float64),
	}
}

// SetRegistryTrust sets the trust weight for a peer registry.
// Weight should be between 0 and 1.
func (et *EigenTrust) SetRegistryTrust(registryID string, weight float64) {
	if weight < 0 {
		weight = 0
	}
	if weight > 1 {
		weight = 1
	}
	et.registryTrust[registryID] = weight
}

// ComputeTrustScore calculates the aggregated trust score for an agent.
// Formula: Trust(agent) = Σ trust_weight(registry_i) × reputation_i(agent)
func (et *EigenTrust) ComputeTrustScore(agentID string) (*models.TrustScore, error) {
	attestations, err := et.store.GetAttestations(agentID)
	if err != nil {
		return nil, err
	}

	if len(attestations) == 0 {
		return &models.TrustScore{
			AgentID:          agentID,
			Score:            0,
			Confidence:       0,
			AttestationCount: 0,
			ComputedAt:       time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	var weightedScore float64
	var totalWeight float64
	var totalInvocations int64

	for _, att := range attestations {
		// Get registry trust weight (default 0.5 for unknown registries)
		weight := 0.5
		if w, ok := et.registryTrust[att.ObserverRegistry]; ok {
			weight = w
		}

		// Compute per-attestation reputation score
		repScore := computeReputationScore(att)

		weightedScore += weight * repScore
		totalWeight += weight
		totalInvocations += att.Invocations
	}

	// Normalize
	finalScore := 0.0
	if totalWeight > 0 {
		finalScore = weightedScore / totalWeight
	}

	// Confidence based on number of attestations and invocations
	confidence := computeConfidence(len(attestations), totalInvocations)

	return &models.TrustScore{
		AgentID:          agentID,
		Score:            clamp(finalScore, 0, 1),
		Confidence:       clamp(confidence, 0, 1),
		AttestationCount: len(attestations),
		ComputedAt:       time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// computeReputationScore computes a 0-1 score from a single attestation.
func computeReputationScore(att *models.ReputationAttestation) float64 {
	if att.Invocations == 0 {
		return 0
	}

	// Success rate component (0-1)
	successRate := float64(att.Successes) / float64(att.Invocations)

	// Rating component (normalize from 0-5 to 0-1)
	ratingScore := att.AvgRating / 5.0
	if ratingScore > 1 {
		ratingScore = 1
	}

	// Latency component (lower is better, normalize with sigmoid)
	// 100ms -> 0.95, 1000ms -> 0.5, 5000ms -> 0.1
	latencyScore := 1.0 / (1.0 + math.Exp((att.AvgLatencyMs-1000)/300))

	// Weighted combination
	return 0.4*successRate + 0.3*ratingScore + 0.3*latencyScore
}

// computeConfidence returns how confident we are in the trust score.
// More attestations and more invocations = higher confidence.
func computeConfidence(attestationCount int, totalInvocations int64) float64 {
	// Attestation confidence: saturates at ~10 attestations
	attConf := 1.0 - math.Exp(-float64(attestationCount)/3.0)

	// Invocation confidence: saturates at ~1000 invocations
	invConf := 1.0 - math.Exp(-float64(totalInvocations)/300.0)

	return 0.5*attConf + 0.5*invConf
}

func clamp(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
