package dht

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"sort"
	"sync"
	"time"
)

// Transport is the interface the DHT uses to send messages to peers.
type Transport interface {
	// SendDHT sends a DHT message to a peer and waits for a response.
	SendDHT(peerAddr string, msg Message) (Message, error)
}

// Config holds DHT configuration.
type Config struct {
	K                 int           // k-bucket size (default 20)
	Alpha             int           // lookup concurrency (default 3)
	RepublishInterval time.Duration // republish stored records
	ExpireAfter       time.Duration // expire records without republish
	LookupTimeout     time.Duration // per-lookup timeout
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		K:                 DefaultK,
		Alpha:             DefaultAlpha,
		RepublishInterval: 1 * time.Hour,
		ExpireAfter:       24 * time.Hour,
		LookupTimeout:     5 * time.Second,
	}
}

// storedRecord wraps a record with metadata for expiration.
type storedRecord struct {
	Record   AgentDHTRecord
	StoredAt time.Time
}

// DHT implements a Kademlia-style distributed hash table.
type DHT struct {
	selfID       NodeID
	selfAddr     string
	routingTable *RoutingTable
	config       Config
	transport    Transport

	// Local DHT store: key (hex) → record
	store   map[string]storedRecord
	storeMu sync.RWMutex

	stopCh chan struct{}
	logger *log.Logger
}

// New creates a new DHT node.
func New(selfID NodeID, selfAddr string, transport Transport, cfg Config) *DHT {
	if cfg.K == 0 {
		cfg.K = DefaultK
	}
	if cfg.Alpha == 0 {
		cfg.Alpha = DefaultAlpha
	}
	if cfg.LookupTimeout == 0 {
		cfg.LookupTimeout = 5 * time.Second
	}
	if cfg.RepublishInterval == 0 {
		cfg.RepublishInterval = 1 * time.Hour
	}
	if cfg.ExpireAfter == 0 {
		cfg.ExpireAfter = 24 * time.Hour
	}

	return &DHT{
		selfID:       selfID,
		selfAddr:     selfAddr,
		routingTable: NewRoutingTable(selfID, cfg.K),
		config:       cfg,
		transport:    transport,
		store:        make(map[string]storedRecord),
		stopCh:       make(chan struct{}),
		logger:       log.Default(),
	}
}

// SetLogger sets a custom logger.
func (d *DHT) SetLogger(l *log.Logger) {
	d.logger = l
}

// RoutingTable returns the routing table for external inspection.
func (d *DHT) RoutingTable() *RoutingTable {
	return d.routingTable
}

// Start begins background goroutines (republish, expiration).
func (d *DHT) Start() {
	go d.republishLoop()
	go d.expireLoop()
}

// Stop signals background goroutines to stop.
func (d *DHT) Stop() {
	close(d.stopCh)
}

// --- Core Operations ---

// Ping checks if a node is alive and adds it to routing table.
func (d *DHT) Ping(contact NodeContact) bool {
	msg := Message{
		Type:       MsgPing,
		SenderID:   d.selfID,
		SenderAddr: d.selfAddr,
		RequestID:  randomID(),
		Timestamp:  time.Now(),
	}

	resp, err := d.transport.SendDHT(contact.Address, msg)
	if err != nil {
		return false
	}
	if resp.Type == MsgPingReply {
		d.routingTable.AddNode(contact)
		return true
	}
	return false
}

// Store stores a record at the k closest nodes to the key.
func (d *DHT) Store(key NodeID, record AgentDHTRecord) error {
	// Store locally
	d.storeLocal(key, record)

	// Find k closest nodes
	closest := d.iterativeFindNode(key)
	if len(closest) == 0 {
		return nil // no peers, stored locally only
	}

	// Send STORE to each
	var wg sync.WaitGroup
	for _, node := range closest {
		wg.Add(1)
		go func(n NodeContact) {
			defer wg.Done()
			msg := Message{
				Type:      MsgStore,
				SenderID:  d.selfID,
				RequestID: randomID(),
				Key:       key,
				Record:    &record,
				Timestamp: time.Now(),
			}
			_, _ = d.transport.SendDHT(n.Address, msg)
		}(node)
	}
	wg.Wait()
	return nil
}

// FindValue looks up a record by key. Returns nil if not found.
func (d *DHT) FindValue(key NodeID) *AgentDHTRecord {
	// Check local store first
	if rec := d.getLocal(key); rec != nil {
		return rec
	}

	return d.iterativeFindValue(key)
}

// FindNode finds the k closest nodes to a target (for external callers).
func (d *DHT) FindNode(target NodeID) []NodeContact {
	return d.iterativeFindNode(target)
}

// AddNode adds a node to the routing table (called when we learn about a new peer).
func (d *DHT) AddNode(contact NodeContact) {
	eviction := d.routingTable.AddNode(contact)
	if eviction != nil {
		// Ping the eviction candidate; if it responds, keep it. Otherwise, replace.
		if !d.Ping(*eviction) {
			idx := d.routingTable.BucketIndex(contact.ID)
			d.routingTable.EvictAndReplace(idx, contact)
		}
	}
}

// HandleMessage processes an incoming DHT message and returns a response.
func (d *DHT) HandleMessage(msg Message) Message {
	// Add sender to routing table
	d.AddNode(NodeContact{
		ID:       msg.SenderID,
		Address:  msg.SenderAddr,
		LastSeen: time.Now().Unix(),
	})

	switch msg.Type {
	case MsgPing:
		return Message{
			Type:      MsgPingReply,
			SenderID:  d.selfID,
			RequestID: msg.RequestID,
			OK:        true,
			Timestamp: time.Now(),
		}

	case MsgStore:
		if msg.Record != nil {
			d.storeLocal(msg.Key, *msg.Record)
		}
		return Message{
			Type:      MsgStoreReply,
			SenderID:  d.selfID,
			RequestID: msg.RequestID,
			OK:        true,
			Timestamp: time.Now(),
		}

	case MsgFindNode:
		closest := d.routingTable.FindClosest(msg.Target, d.config.K)
		return Message{
			Type:      MsgFindNodeReply,
			SenderID:  d.selfID,
			RequestID: msg.RequestID,
			Nodes:     closest,
			Timestamp: time.Now(),
		}

	case MsgFindValue:
		// Check local store
		if rec := d.getLocal(msg.Key); rec != nil {
			return Message{
				Type:      MsgFindValueReply,
				SenderID:  d.selfID,
				RequestID: msg.RequestID,
				Value:     rec,
				Timestamp: time.Now(),
			}
		}
		// Not found locally — return closest nodes
		closest := d.routingTable.FindClosest(msg.Key, d.config.K)
		return Message{
			Type:      MsgFindValueReply,
			SenderID:  d.selfID,
			RequestID: msg.RequestID,
			Nodes:     closest,
			Timestamp: time.Now(),
		}

	default:
		return Message{
			Type:      MsgPingReply,
			SenderID:  d.selfID,
			RequestID: msg.RequestID,
			Timestamp: time.Now(),
		}
	}
}

// --- Iterative Lookups ---

func (d *DHT) iterativeFindNode(target NodeID) []NodeContact {
	ctx, cancel := context.WithTimeout(context.Background(), d.config.LookupTimeout)
	defer cancel()

	// Seed with α closest nodes from local routing table
	closest := d.routingTable.FindClosest(target, d.config.Alpha)
	if len(closest) == 0 {
		return nil
	}

	queried := make(map[NodeID]bool)
	queried[d.selfID] = true

	result := make([]NodeContact, 0, d.config.K)
	result = append(result, closest...)

	for {
		// Pick unqueried nodes from result
		var toQuery []NodeContact
		for _, n := range result {
			if !queried[n.ID] {
				toQuery = append(toQuery, n)
			}
		}
		if len(toQuery) == 0 {
			break
		}
		if len(toQuery) > d.config.Alpha {
			toQuery = toQuery[:d.config.Alpha]
		}

		// Query in parallel
		type queryResult struct {
			nodes []NodeContact
		}
		ch := make(chan queryResult, len(toQuery))

		for _, n := range toQuery {
			queried[n.ID] = true
			go func(node NodeContact) {
				msg := Message{
					Type:      MsgFindNode,
					SenderID:  d.selfID,
					RequestID: randomID(),
					Target:    target,
					Timestamp: time.Now(),
				}
				resp, err := d.transport.SendDHT(node.Address, msg)
				if err != nil {
					ch <- queryResult{}
					return
				}
				ch <- queryResult{nodes: resp.Nodes}
			}(n)
		}

		// Collect results
		newNodes := false
		for range toQuery {
			select {
			case <-ctx.Done():
				goto done
			case qr := <-ch:
				for _, n := range qr.nodes {
					if n.ID == d.selfID || queried[n.ID] {
						continue
					}
					result = append(result, n)
					newNodes = true
				}
			}
		}

		if !newNodes {
			break
		}

		// Sort by distance and keep k closest
		sort.Slice(result, func(i, j int) bool {
			di := Distance(result[i].ID, target)
			dj := Distance(result[j].ID, target)
			return Less(di, dj)
		})
		if len(result) > d.config.K {
			result = result[:d.config.K]
		}
	}

done:
	// Deduplicate
	seen := make(map[NodeID]bool)
	var deduped []NodeContact
	for _, n := range result {
		if !seen[n.ID] {
			seen[n.ID] = true
			deduped = append(deduped, n)
		}
	}

	sort.Slice(deduped, func(i, j int) bool {
		di := Distance(deduped[i].ID, target)
		dj := Distance(deduped[j].ID, target)
		return Less(di, dj)
	})
	if len(deduped) > d.config.K {
		deduped = deduped[:d.config.K]
	}
	return deduped
}

func (d *DHT) iterativeFindValue(key NodeID) *AgentDHTRecord {
	ctx, cancel := context.WithTimeout(context.Background(), d.config.LookupTimeout)
	defer cancel()

	closest := d.routingTable.FindClosest(key, d.config.Alpha)
	if len(closest) == 0 {
		return nil
	}

	queried := make(map[NodeID]bool)
	queried[d.selfID] = true

	candidates := make([]NodeContact, 0, d.config.K)
	candidates = append(candidates, closest...)

	for {
		var toQuery []NodeContact
		for _, n := range candidates {
			if !queried[n.ID] {
				toQuery = append(toQuery, n)
			}
		}
		if len(toQuery) == 0 {
			return nil
		}
		if len(toQuery) > d.config.Alpha {
			toQuery = toQuery[:d.config.Alpha]
		}

		type queryResult struct {
			value *AgentDHTRecord
			nodes []NodeContact
		}
		ch := make(chan queryResult, len(toQuery))

		for _, n := range toQuery {
			queried[n.ID] = true
			go func(node NodeContact) {
				msg := Message{
					Type:      MsgFindValue,
					SenderID:  d.selfID,
					RequestID: randomID(),
					Key:       key,
					Timestamp: time.Now(),
				}
				resp, err := d.transport.SendDHT(node.Address, msg)
				if err != nil {
					ch <- queryResult{}
					return
				}
				if resp.Value != nil {
					ch <- queryResult{value: resp.Value}
				} else {
					ch <- queryResult{nodes: resp.Nodes}
				}
			}(n)
		}

		newNodes := false
		for range toQuery {
			select {
			case <-ctx.Done():
				return nil
			case qr := <-ch:
				if qr.value != nil {
					// Cache the found value locally
					d.storeLocal(key, *qr.value)
					return qr.value
				}
				for _, n := range qr.nodes {
					if n.ID == d.selfID || queried[n.ID] {
						continue
					}
					candidates = append(candidates, n)
					newNodes = true
				}
			}
		}

		if !newNodes {
			return nil
		}

		sort.Slice(candidates, func(i, j int) bool {
			di := Distance(candidates[i].ID, key)
			dj := Distance(candidates[j].ID, key)
			return Less(di, dj)
		})
		if len(candidates) > d.config.K {
			candidates = candidates[:d.config.K]
		}
	}
}

// --- Local Store ---

func (d *DHT) storeLocal(key NodeID, record AgentDHTRecord) {
	d.storeMu.Lock()
	defer d.storeMu.Unlock()
	d.store[key.Hex()] = storedRecord{
		Record:   record,
		StoredAt: time.Now(),
	}
}

func (d *DHT) getLocal(key NodeID) *AgentDHTRecord {
	d.storeMu.RLock()
	defer d.storeMu.RUnlock()
	if sr, ok := d.store[key.Hex()]; ok {
		rec := sr.Record
		return &rec
	}
	return nil
}

// --- Background Goroutines ---

func (d *DHT) republishLoop() {
	ticker := time.NewTicker(d.config.RepublishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.republish()
		}
	}
}

func (d *DHT) republish() {
	d.storeMu.RLock()
	entries := make(map[string]storedRecord, len(d.store))
	for k, v := range d.store {
		entries[k] = v
	}
	d.storeMu.RUnlock()

	for hexKey, sr := range entries {
		var key NodeID
		b, _ := hex.DecodeString(hexKey)
		copy(key[:], b)
		// Re-store to k closest nodes
		closest := d.iterativeFindNode(key)
		for _, node := range closest {
			msg := Message{
				Type:      MsgStore,
				SenderID:  d.selfID,
				RequestID: randomID(),
				Key:       key,
				Record:    &sr.Record,
				Timestamp: time.Now(),
			}
			go func(addr string) {
				_, _ = d.transport.SendDHT(addr, msg)
			}(node.Address)
		}
	}
}

func (d *DHT) expireLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.expire()
		}
	}
}

func (d *DHT) expire() {
	d.storeMu.Lock()
	defer d.storeMu.Unlock()

	cutoff := time.Now().Add(-d.config.ExpireAfter)
	for k, sr := range d.store {
		if sr.StoredAt.Before(cutoff) {
			delete(d.store, k)
		}
	}
}

// StoreCount returns the number of records in the local DHT store.
func (d *DHT) StoreCount() int {
	d.storeMu.RLock()
	defer d.storeMu.RUnlock()
	return len(d.store)
}

// --- Helpers ---

func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
