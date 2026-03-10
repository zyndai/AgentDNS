package mesh

import (
	"log"
	"strings"
	"time"
)

// HeartbeatLoop sends periodic heartbeats to all connected peers.
// Includes bloom filter, agent count, and peer addresses for peer exchange.
// Runs until Stop() is called.
func (t *Transport) HeartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Build and maintain a local bloom filter of our agent tokens
	localBloom := NewBloomFilter(t.bloomCfg.ExpectedTokens, t.bloomCfg.FalsePositiveRate)

	// Rebuild the bloom filter periodically
	bloomTicker := time.NewTicker(time.Duration(t.bloomCfg.UpdateIntervalSeconds) * time.Second)
	defer bloomTicker.Stop()

	for {
		select {
		case <-t.stopCh:
			return

		case <-bloomTicker.C:
			localBloom = t.rebuildBloom()

		case <-ticker.C:
			t.sendHeartbeats(localBloom)
		}
	}
}

// sendHeartbeats sends a heartbeat message to all connected peers.
func (t *Transport) sendHeartbeats(bloom *BloomFilter) {
	agentCount, _ := t.store.CountAgents()
	peerAddrs := t.GetPeerAddresses()

	hb := HeartbeatMessage{
		RegistryID: t.kp.RegistryID(),
		AgentCount: agentCount,
		PeerAddrs:  peerAddrs,
	}

	// Attach bloom filter if available
	if bloom != nil {
		hb.BloomFilter = bloom.Bytes()
		hb.BloomSize = bloom.Size()
		hb.BloomHashes = bloom.hashNum
	}

	t.mu.RLock()
	peers := make([]*peerConn, 0, len(t.conns))
	for _, pc := range t.conns {
		peers = append(peers, pc)
	}
	t.mu.RUnlock()

	for _, pc := range peers {
		if err := pc.send(MsgHeartbeat, &hb); err != nil {
			idPrefix := pc.registryID
			if len(idPrefix) > 24 {
				idPrefix = idPrefix[:24]
			}
			log.Printf("mesh: heartbeat to %s failed: %v", idPrefix, err)
		}
	}
}

// rebuildBloom creates a new bloom filter from all local agents and gossip entries.
func (t *Transport) rebuildBloom() *BloomFilter {
	bloom := NewBloomFilter(t.bloomCfg.ExpectedTokens, t.bloomCfg.FalsePositiveRate)

	// We access the store to get agent metadata for building the bloom filter.
	// The store interface on the transport only has CountAgents, so we use
	// the PeerManager's bloom config to build from known agent names/tags.
	// In practice, the bloom filter is populated as agents are registered.
	//
	// For now, we build from the gossip handler's store which has the full
	// Store interface.
	if gh := t.gossip; gh != nil && gh.store != nil {
		agents, err := gh.store.ListAgents("", 100000, 0)
		if err == nil {
			for _, agent := range agents {
				// Add name tokens
				for _, token := range tokenize(agent.Name) {
					bloom.Add(strings.ToLower(token))
				}
				// Add category
				bloom.Add(strings.ToLower(agent.Category))
				// Add tags
				for _, tag := range agent.Tags {
					bloom.Add(strings.ToLower(tag))
				}
				// Add summary tokens
				for _, token := range tokenize(agent.Summary) {
					bloom.Add(strings.ToLower(token))
				}
			}
		}

		// Also include gossip entries
		entries, err := gh.store.SearchGossipByKeyword("", "", nil, 100000)
		if err == nil {
			for _, entry := range entries {
				for _, token := range tokenize(entry.Name) {
					bloom.Add(strings.ToLower(token))
				}
				bloom.Add(strings.ToLower(entry.Category))
				for _, tag := range entry.Tags {
					bloom.Add(strings.ToLower(tag))
				}
				for _, token := range tokenize(entry.Summary) {
					bloom.Add(strings.ToLower(token))
				}
			}
		}
	}

	return bloom
}

// tokenize splits text into lowercase tokens for bloom filter population.
// Matches the tokenizer used in the keyword search index.
func tokenize(text string) []string {
	var tokens []string
	var current []byte

	for i := 0; i < len(text); i++ {
		c := text[i]
		if isAlphaNum(c) || c == '-' || c == '_' {
			current = append(current, c)
		} else {
			if len(current) >= 2 {
				tokens = append(tokens, strings.ToLower(string(current)))
			}
			current = current[:0]
		}
	}
	if len(current) >= 2 {
		tokens = append(tokens, strings.ToLower(string(current)))
	}

	return tokens
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
