package dht

import (
	"sort"
	"sync"
	"time"
)

const (
	// NumBuckets is the number of k-buckets (one per bit of NodeID).
	NumBuckets = IDLength * 8 // 256
)

// KBucket holds up to K contacts with similar XOR distance to the local node.
type KBucket struct {
	entries []NodeContact
	k       int
}

func newKBucket(k int) *KBucket {
	return &KBucket{
		entries: make([]NodeContact, 0, k),
		k:       k,
	}
}

// Len returns the number of entries.
func (b *KBucket) Len() int { return len(b.entries) }

// IsFull returns true if the bucket is at capacity.
func (b *KBucket) IsFull() bool { return len(b.entries) >= b.k }

// Contains checks if a node ID is in this bucket.
func (b *KBucket) Contains(id NodeID) bool {
	for _, e := range b.entries {
		if e.ID == id {
			return true
		}
	}
	return false
}

// Get returns the contact for a node ID, or nil if not found.
func (b *KBucket) Get(id NodeID) *NodeContact {
	for i := range b.entries {
		if b.entries[i].ID == id {
			return &b.entries[i]
		}
	}
	return nil
}

// AddOrUpdate inserts a node or moves it to the tail (most recently seen).
// Returns the eviction candidate (head) if the bucket is full and the node is new.
func (b *KBucket) AddOrUpdate(contact NodeContact) *NodeContact {
	// If already in bucket, move to tail
	for i, e := range b.entries {
		if e.ID == contact.ID {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			contact.LastSeen = time.Now().Unix()
			b.entries = append(b.entries, contact)
			return nil
		}
	}

	// Not in bucket
	if !b.IsFull() {
		contact.LastSeen = time.Now().Unix()
		b.entries = append(b.entries, contact)
		return nil
	}

	// Bucket full — return head (least recently seen) as eviction candidate
	candidate := b.entries[0]
	return &candidate
}

// Evict removes the head entry (least recently seen) and appends the new contact.
func (b *KBucket) Evict(newContact NodeContact) {
	if len(b.entries) > 0 {
		b.entries = b.entries[1:]
	}
	newContact.LastSeen = time.Now().Unix()
	b.entries = append(b.entries, newContact)
}

// MoveToTail moves a node to the tail (most recently seen) if it exists.
func (b *KBucket) MoveToTail(id NodeID) {
	for i, e := range b.entries {
		if e.ID == id {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			e.LastSeen = time.Now().Unix()
			b.entries = append(b.entries, e)
			return
		}
	}
}

// Remove removes a node from the bucket.
func (b *KBucket) Remove(id NodeID) {
	for i, e := range b.entries {
		if e.ID == id {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			return
		}
	}
}

// Entries returns a copy of all entries.
func (b *KBucket) Entries() []NodeContact {
	out := make([]NodeContact, len(b.entries))
	copy(out, b.entries)
	return out
}

// RoutingTable implements a Kademlia routing table with 256 k-buckets.
type RoutingTable struct {
	selfID  NodeID
	buckets [NumBuckets]*KBucket
	k       int
	mu      sync.RWMutex
}

// NewRoutingTable creates a routing table for the given local node ID.
func NewRoutingTable(selfID NodeID, k int) *RoutingTable {
	rt := &RoutingTable{
		selfID: selfID,
		k:      k,
	}
	for i := 0; i < NumBuckets; i++ {
		rt.buckets[i] = newKBucket(k)
	}
	return rt
}

// BucketIndex returns the index of the k-bucket for the given node ID.
// Based on the number of leading zero bits of XOR(self, target).
func (rt *RoutingTable) BucketIndex(target NodeID) int {
	dist := Distance(rt.selfID, target)
	lz := dist.LeadingZeros()
	// Bucket index is (255 - leading zeros), clamped to [0, 255]
	idx := NumBuckets - 1 - lz
	if idx < 0 {
		idx = 0
	}
	return idx
}

// AddNode adds or updates a node in the appropriate k-bucket.
// Returns an eviction candidate if the bucket is full, or nil.
func (rt *RoutingTable) AddNode(contact NodeContact) *NodeContact {
	if contact.ID == rt.selfID {
		return nil // don't add self
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	idx := rt.BucketIndex(contact.ID)
	return rt.buckets[idx].AddOrUpdate(contact)
}

// RemoveNode removes a node from the routing table.
func (rt *RoutingTable) RemoveNode(id NodeID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	idx := rt.BucketIndex(id)
	rt.buckets[idx].Remove(id)
}

// EvictAndReplace evicts the LRU node from a bucket and adds the new contact.
func (rt *RoutingTable) EvictAndReplace(bucketIdx int, newContact NodeContact) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if bucketIdx >= 0 && bucketIdx < NumBuckets {
		rt.buckets[bucketIdx].Evict(newContact)
	}
}

// MarkSeen moves a node to the most-recently-seen position.
func (rt *RoutingTable) MarkSeen(id NodeID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	idx := rt.BucketIndex(id)
	rt.buckets[idx].MoveToTail(id)
}

// FindClosest returns the k closest nodes to the target from the routing table.
func (rt *RoutingTable) FindClosest(target NodeID, count int) []NodeContact {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	// Collect all contacts
	var all []NodeContact
	for _, bucket := range rt.buckets {
		all = append(all, bucket.Entries()...)
	}

	// Sort by XOR distance to target
	sort.Slice(all, func(i, j int) bool {
		di := Distance(all[i].ID, target)
		dj := Distance(all[j].ID, target)
		return Less(di, dj)
	})

	if len(all) > count {
		all = all[:count]
	}
	return all
}

// Size returns the total number of nodes in the routing table.
func (rt *RoutingTable) Size() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	n := 0
	for _, bucket := range rt.buckets {
		n += bucket.Len()
	}
	return n
}

// AllNodes returns all nodes in the routing table.
func (rt *RoutingTable) AllNodes() []NodeContact {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var all []NodeContact
	for _, bucket := range rt.buckets {
		all = append(all, bucket.Entries()...)
	}
	return all
}
