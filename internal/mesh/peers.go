package mesh

import (
	"log"
	"sync"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/models"
)

// PeerManager maintains the set of connected peer registries.
type PeerManager struct {
	mu       sync.RWMutex
	peers    map[string]*models.PeerInfo
	maxPeers int
	bloomCfg config.BloomConfig
	blooms   map[string]*BloomFilter // peer bloom filters
}

// NewPeerManager creates a new peer manager.
func NewPeerManager(meshCfg config.MeshConfig, bloomCfg config.BloomConfig) *PeerManager {
	pm := &PeerManager{
		peers:    make(map[string]*models.PeerInfo),
		maxPeers: meshCfg.MaxPeers,
		bloomCfg: bloomCfg,
		blooms:   make(map[string]*BloomFilter),
	}
	return pm
}

// AddPeer registers a new peer.
func (pm *PeerManager) AddPeer(peer *models.PeerInfo) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.peers) >= pm.maxPeers {
		// Evict the peer with oldest last_seen
		pm.evictOldest()
	}

	peer.ConnectedAt = time.Now().UTC().Format(time.RFC3339)
	peer.LastSeen = peer.ConnectedAt
	pm.peers[peer.RegistryID] = peer
	log.Printf("peer added: %s (%s)", peer.Name, peer.Address)
}

// RemovePeer removes a peer.
func (pm *PeerManager) RemovePeer(registryID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.peers, registryID)
	delete(pm.blooms, registryID)
}

// UpdatePeerLastSeen updates the last heartbeat time for a peer.
func (pm *PeerManager) UpdatePeerLastSeen(registryID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if peer, ok := pm.peers[registryID]; ok {
		peer.LastSeen = time.Now().UTC().Format(time.RFC3339)
	}
}

// GetPeers returns all connected peers.
func (pm *PeerManager) GetPeers() []*models.PeerInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peers := make([]*models.PeerInfo, 0, len(pm.peers))
	for _, p := range pm.peers {
		peers = append(peers, p)
	}
	return peers
}

// GetPeer returns a specific peer by ID.
func (pm *PeerManager) GetPeer(registryID string) *models.PeerInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.peers[registryID]
}

// PeerCount returns the number of connected peers.
func (pm *PeerManager) PeerCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.peers)
}

// SetPeerBloomFilter stores a peer's bloom filter (received during heartbeat exchange).
func (pm *PeerManager) SetPeerBloomFilter(registryID string, bf *BloomFilter) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.blooms[registryID] = bf
}

// GetRelevantPeers returns peers whose bloom filters match the given query tokens.
// Falls back to random selection if bloom filters aren't available.
func (pm *PeerManager) GetRelevantPeers(queryTokens []string, maxPeers int) []*models.PeerInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	type peerScore struct {
		peer  *models.PeerInfo
		score int
	}

	var scored []peerScore

	for id, peer := range pm.peers {
		bf, hasBf := pm.blooms[id]
		if hasBf {
			matches := bf.MatchCount(queryTokens)
			scored = append(scored, peerScore{peer: peer, score: matches})
		} else {
			// No bloom filter — give a neutral score so it's included in fallback
			scored = append(scored, peerScore{peer: peer, score: 1})
		}
	}

	// Sort by score descending
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// Return top peers with score >= 2 (or all if fewer)
	var result []*models.PeerInfo
	for _, ps := range scored {
		if len(result) >= maxPeers {
			break
		}
		if ps.score >= 1 { // include peers with at least 1 match
			result = append(result, ps.peer)
		}
	}

	return result
}

// evictOldest removes the peer with the oldest last_seen.
func (pm *PeerManager) evictOldest() {
	var oldestID string
	var oldestTime time.Time

	for id, peer := range pm.peers {
		t, err := time.Parse(time.RFC3339, peer.LastSeen)
		if err != nil {
			oldestID = id
			break
		}
		if oldestID == "" || t.Before(oldestTime) {
			oldestID = id
			oldestTime = t
		}
	}

	if oldestID != "" {
		delete(pm.peers, oldestID)
		delete(pm.blooms, oldestID)
		log.Printf("peer evicted: %s", oldestID)
	}
}
