package dht

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// --- NodeID Tests ---

func TestNodeIDFromPublicKey(t *testing.T) {
	pubKey := []byte("test-public-key-32-bytes-long!!!")
	id := NodeIDFromPublicKey(pubKey)
	if id.IsZero() {
		t.Fatal("expected non-zero NodeID")
	}
}

func TestDistance(t *testing.T) {
	var a, b NodeID
	a[0] = 0xFF
	b[0] = 0x00
	d := Distance(a, b)
	if d[0] != 0xFF {
		t.Fatalf("expected XOR=0xFF, got 0x%02x", d[0])
	}

	// Distance to self is zero
	d = Distance(a, a)
	if !d.IsZero() {
		t.Fatal("distance to self should be zero")
	}
}

func TestLeadingZeros(t *testing.T) {
	var id NodeID
	if id.LeadingZeros() != 256 {
		t.Fatalf("expected 256, got %d", id.LeadingZeros())
	}

	id[0] = 0x01 // 00000001 = 7 leading zeros in first byte
	if id.LeadingZeros() != 7 {
		t.Fatalf("expected 7, got %d", id.LeadingZeros())
	}

	id[0] = 0x80 // 10000000 = 0 leading zeros in first byte
	if id.LeadingZeros() != 0 {
		t.Fatalf("expected 0, got %d", id.LeadingZeros())
	}
}

func TestLess(t *testing.T) {
	var a, b NodeID
	a[31] = 1
	b[31] = 2
	if !Less(a, b) {
		t.Fatal("a should be less than b")
	}
	if Less(b, a) {
		t.Fatal("b should not be less than a")
	}
	if Less(a, a) {
		t.Fatal("a should not be less than itself")
	}
}

func TestNodeIDFromAgentID(t *testing.T) {
	id, err := NodeIDFromAgentID("agdns:7f3a9c2e12345678")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id[0] != 0x7f || id[1] != 0x3a {
		t.Fatalf("unexpected prefix: %x %x", id[0], id[1])
	}

	id, err = NodeIDFromAgentID("agdns:dev:abcdef0123456789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id[0] != 0xab {
		t.Fatalf("unexpected prefix: %x", id[0])
	}
}

// --- Routing Table Tests ---

func TestRoutingTableAddAndFind(t *testing.T) {
	var selfID NodeID
	selfID[0] = 0x00
	rt := NewRoutingTable(selfID, 20) // large k to avoid eviction

	// Add some nodes spread across key space
	for i := byte(1); i <= 10; i++ {
		var id NodeID
		id[0] = i * 0x10 // spread them out: 0x10, 0x20, ..., 0xA0
		rt.AddNode(NodeContact{
			ID:      id,
			Address: fmt.Sprintf("127.0.0.1:%d", 4000+int(i)),
		})
	}

	if rt.Size() != 10 {
		t.Fatalf("expected 10 nodes, got %d", rt.Size())
	}

	// Find closest to target 0x50
	var target NodeID
	target[0] = 0x50
	closest := rt.FindClosest(target, 3)
	if len(closest) != 3 {
		t.Fatalf("expected 3 closest, got %d", len(closest))
	}

	// The closest should be 0x50 (distance 0)
	if closest[0].ID[0] != 0x50 {
		t.Fatalf("expected closest to be 0x50, got 0x%02x", closest[0].ID[0])
	}
}

func TestRoutingTableSelfExclusion(t *testing.T) {
	var selfID NodeID
	selfID[0] = 0x42
	rt := NewRoutingTable(selfID, 20)

	// Adding self should be ignored
	rt.AddNode(NodeContact{ID: selfID, Address: "localhost:4001"})
	if rt.Size() != 0 {
		t.Fatal("self should not be in routing table")
	}
}

func TestKBucketEviction(t *testing.T) {
	bucket := newKBucket(2) // small bucket for testing

	var id1, id2, id3 NodeID
	id1[0] = 0x01
	id2[0] = 0x02
	id3[0] = 0x03

	bucket.AddOrUpdate(NodeContact{ID: id1, Address: "a"})
	bucket.AddOrUpdate(NodeContact{ID: id2, Address: "b"})

	// Bucket is full, adding id3 should return eviction candidate
	candidate := bucket.AddOrUpdate(NodeContact{ID: id3, Address: "c"})
	if candidate == nil {
		t.Fatal("expected eviction candidate")
	}
	if candidate.ID != id1 {
		t.Fatalf("expected eviction candidate id1, got %s", candidate.ID.Short())
	}
}

func TestKBucketMoveToTail(t *testing.T) {
	bucket := newKBucket(5)

	var id1, id2, id3 NodeID
	id1[0] = 0x01
	id2[0] = 0x02
	id3[0] = 0x03

	bucket.AddOrUpdate(NodeContact{ID: id1, Address: "a"})
	bucket.AddOrUpdate(NodeContact{ID: id2, Address: "b"})
	bucket.AddOrUpdate(NodeContact{ID: id3, Address: "c"})

	// Move id1 to tail
	bucket.AddOrUpdate(NodeContact{ID: id1, Address: "a"})

	entries := bucket.Entries()
	if entries[2].ID != id1 {
		t.Fatal("id1 should be at tail after re-add")
	}
}

// --- DHT Integration Test (in-process) ---

type mockTransport struct {
	nodes map[string]*DHT // address → DHT node
	mu    sync.RWMutex
}

func newMockTransport() *mockTransport {
	return &mockTransport{nodes: make(map[string]*DHT)}
}

func (t *mockTransport) register(addr string, node *DHT) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nodes[addr] = node
}

func (t *mockTransport) SendDHT(peerAddr string, msg Message) (Message, error) {
	t.mu.RLock()
	node, ok := t.nodes[peerAddr]
	t.mu.RUnlock()

	if !ok {
		return Message{}, fmt.Errorf("peer not found: %s", peerAddr)
	}
	return node.HandleMessage(msg), nil
}

func TestDHTStoreAndFindValue(t *testing.T) {
	transport := newMockTransport()

	// Create 5 DHT nodes
	nodes := make([]*DHT, 5)
	for i := 0; i < 5; i++ {
		var id NodeID
		id[0] = byte(i + 1) * 0x20 // spread across key space
		addr := fmt.Sprintf("127.0.0.1:%d", 5000+i)

		nodes[i] = New(id, addr, transport, DefaultConfig())
		transport.register(addr, nodes[i])
	}

	// Connect all nodes to each other
	for i, n := range nodes {
		for j, m := range nodes {
			if i != j {
				n.AddNode(NodeContact{
					ID:       m.selfID,
					Address:  m.selfAddr,
					LastSeen: time.Now().Unix(),
				})
			}
		}
	}

	// Store a record on node 0
	var key NodeID
	key[0] = 0x50 // between nodes
	record := AgentDHTRecord{
		AgentID:  "agdns:test1234",
		Name:     "TestAgent",
		AgentURL: "http://localhost:5000",
		StoredAt: time.Now().Format(time.RFC3339),
	}

	err := nodes[0].Store(key, record)
	if err != nil {
		t.Fatalf("store failed: %v", err)
	}

	// Find value from a different node
	found := nodes[4].FindValue(key)
	if found == nil {
		t.Fatal("expected to find value")
	}
	if found.AgentID != "agdns:test1234" {
		t.Fatalf("expected agdns:test1234, got %s", found.AgentID)
	}
}

func TestDHTFindNode(t *testing.T) {
	transport := newMockTransport()

	nodes := make([]*DHT, 3)
	for i := 0; i < 3; i++ {
		var id NodeID
		id[0] = byte(i+1) * 0x30
		addr := fmt.Sprintf("127.0.0.1:%d", 6000+i)
		nodes[i] = New(id, addr, transport, DefaultConfig())
		transport.register(addr, nodes[i])
	}

	// Wire them up
	for i, n := range nodes {
		for j, m := range nodes {
			if i != j {
				n.AddNode(NodeContact{
					ID:      m.selfID,
					Address: m.selfAddr,
				})
			}
		}
	}

	var target NodeID
	target[0] = 0x45
	closest := nodes[0].FindNode(target)
	if len(closest) == 0 {
		t.Fatal("expected closest nodes")
	}
}

func TestDHTHandlePing(t *testing.T) {
	var selfID NodeID
	selfID[0] = 0x01
	transport := newMockTransport()
	node := New(selfID, "localhost:4001", transport, DefaultConfig())

	msg := Message{
		Type:       MsgPing,
		SenderID:   NodeID{0x02},
		SenderAddr: "localhost:4002",
		RequestID:  "test-123",
		Timestamp:  time.Now(),
	}

	resp := node.HandleMessage(msg)
	if resp.Type != MsgPingReply {
		t.Fatalf("expected ping reply, got %s", resp.Type)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}
}
