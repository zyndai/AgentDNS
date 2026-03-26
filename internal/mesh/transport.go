package mesh

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/models"
)

// peerConn represents an active TCP connection to a peer.
type peerConn struct {
	conn       net.Conn
	registryID string
	name       string
	address    string // host:port the peer listens on for mesh
	mu         sync.Mutex
}

// send writes a typed message to the peer, protected by mutex.
func (pc *peerConn) send(msgType string, payload interface{}) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return sendTyped(pc.conn, msgType, payload)
}

// Transport manages the TCP mesh network: listening, connecting, and
// dispatching messages between peers.
type Transport struct {
	cfg      config.MeshConfig
	bloomCfg config.BloomConfig
	nodeName string
	peerMgr  *PeerManager
	gossip   *GossipHandler
	kp       *identity.Keypair
	store    interface{ CountAgents() (int, error) }
	listener net.Listener

	mu    sync.RWMutex
	conns map[string]*peerConn // registryID -> connection

	// Callbacks set by higher-level components.
	onSearch func(*models.SearchRequest) (*models.SearchResponse, error)
	onDHT    func(json.RawMessage) json.RawMessage // DHT message handler

	// DHT request-response correlation
	dhtPending   map[string]chan json.RawMessage // requestID → response channel
	dhtPendingMu sync.Mutex

	eventBus *events.Bus

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewTransport creates a new mesh transport layer.
func NewTransport(
	meshCfg config.MeshConfig,
	bloomCfg config.BloomConfig,
	nodeName string,
	peerMgr *PeerManager,
	gossipHandler *GossipHandler,
	kp *identity.Keypair,
	agentCounter interface{ CountAgents() (int, error) },
) *Transport {
	return &Transport{
		cfg:      meshCfg,
		bloomCfg: bloomCfg,
		nodeName: nodeName,
		peerMgr:  peerMgr,
		gossip:   gossipHandler,
		kp:       kp,
		store:    agentCounter,
		conns:      make(map[string]*peerConn),
		dhtPending: make(map[string]chan json.RawMessage),
		stopCh:     make(chan struct{}),
	}
}

// SetSearchHandler registers the callback invoked for incoming federated search requests.
func (t *Transport) SetSearchHandler(fn func(*models.SearchRequest) (*models.SearchResponse, error)) {
	t.onSearch = fn
}

// SetDHTHandler registers the callback invoked for incoming DHT messages.
func (t *Transport) SetDHTHandler(fn func(json.RawMessage) json.RawMessage) {
	t.onDHT = fn
}

// SendDHTRequest sends a DHT message to a specific peer and waits for a response.
// This implements the dht.Transport interface.
func (t *Transport) SendDHTRequest(peerAddr string, msgBytes json.RawMessage) (json.RawMessage, error) {
	// Extract request_id from the message for correlation
	var peek struct {
		RequestID string `json:"request_id"`
	}
	json.Unmarshal(msgBytes, &peek)

	// Register pending response channel
	ch := make(chan json.RawMessage, 1)
	t.dhtPendingMu.Lock()
	t.dhtPending[peek.RequestID] = ch
	t.dhtPendingMu.Unlock()

	defer func() {
		t.dhtPendingMu.Lock()
		delete(t.dhtPending, peek.RequestID)
		t.dhtPendingMu.Unlock()
	}()

	// Find peer by address and send
	t.mu.RLock()
	var targetConn *peerConn
	for _, pc := range t.conns {
		if pc.address == peerAddr {
			targetConn = pc
			break
		}
	}
	t.mu.RUnlock()

	if targetConn == nil {
		return nil, fmt.Errorf("no connection to peer %s", peerAddr)
	}

	env := Envelope{Type: MsgDHT, Payload: msgBytes}
	if err := writeMessage(targetConn.conn, &env); err != nil {
		return nil, fmt.Errorf("send DHT message: %w", err)
	}

	// Wait for response with timeout
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("DHT request timeout to %s", peerAddr)
	}
}

// SetEventBus attaches an event bus for publishing peer connect/disconnect events.
func (t *Transport) SetEventBus(bus *events.Bus) {
	t.eventBus = bus
}

// Listen starts the TCP listener on the configured mesh port.
func (t *Transport) Listen() error {
	addr := fmt.Sprintf("0.0.0.0:%d", t.cfg.ListenPort)
	var ln net.Listener
	var err error
	if t.cfg.TLSEnabled {
		tlsConfig, tlsErr := t.kp.GenerateTLSConfig()
		if tlsErr != nil {
			return fmt.Errorf("mesh TLS config: %w", tlsErr)
		}
		ln, err = tls.Listen("tcp", addr, tlsConfig)
	} else {
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mesh listen on %s: %w", addr, err)
	}
	t.listener = ln
	log.Printf("mesh: listening on %s", addr)

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-t.stopCh:
					return
				default:
					log.Printf("mesh: accept error: %v", err)
					continue
				}
			}
			t.wg.Add(1)
			go func() {
				defer t.wg.Done()
				t.handleInbound(conn)
			}()
		}
	}()

	return nil
}

// Stop gracefully shuts down the transport.
func (t *Transport) Stop() {
	close(t.stopCh)
	if t.listener != nil {
		t.listener.Close()
	}
	t.mu.RLock()
	for _, pc := range t.conns {
		pc.conn.Close()
	}
	t.mu.RUnlock()
	t.wg.Wait()
}

// makeHello creates a HelloMessage for the local node.
func (t *Transport) makeHello() HelloMessage {
	agentCount, _ := t.store.CountAgents()
	return HelloMessage{
		RegistryID: t.kp.RegistryID(),
		Name:       t.nodeName,
		PublicKey:  t.kp.PublicKeyString(),
		AgentCount: agentCount,
		Version:    models.ProtocolVersion,
		ListenPort: t.cfg.ListenPort,
	}
}

// handleInbound processes an incoming peer connection.
// Performs the HELLO handshake, then enters the read loop.
func (t *Transport) handleInbound(conn net.Conn) {
	defer conn.Close()

	// Read the peer's HELLO
	env, err := readMessage(conn, 10*time.Second)
	if err != nil {
		log.Printf("mesh: inbound handshake read failed: %v", err)
		return
	}
	if env.Type != MsgHello {
		log.Printf("mesh: expected HELLO, got %s", env.Type)
		return
	}

	var hello HelloMessage
	if err := decodePayload(env, &hello); err != nil {
		log.Printf("mesh: decode HELLO: %v", err)
		return
	}

	// Don't connect to ourselves
	if hello.RegistryID == t.kp.RegistryID() {
		return
	}

	// Check if we already have this peer
	t.mu.RLock()
	_, exists := t.conns[hello.RegistryID]
	t.mu.RUnlock()
	if exists {
		// Already connected — send a HELLO reply and close.
		_ = sendTyped(conn, MsgHello, t.makeHello())
		return
	}

	// Send our HELLO back
	if err := sendTyped(conn, MsgHello, t.makeHello()); err != nil {
		log.Printf("mesh: send HELLO reply: %v", err)
		return
	}

	// Register the peer
	peerAddr := resolvePeerAddr(conn.RemoteAddr().String(), hello.ListenPort)
	t.registerPeer(hello, peerAddr, conn)
	defer t.unregisterPeer(hello.RegistryID, conn)

	// Enter the read loop
	t.readLoop(conn, hello.RegistryID)
}

// ConnectTo dials a peer address, performs the HELLO handshake, and enters
// the read loop in the background.
// Returns the peer's registry ID on success, or an error.
func (t *Transport) ConnectTo(address string) (string, error) {
	var conn net.Conn
	var err error
	if t.cfg.TLSEnabled {
		tlsConfig, tlsErr := t.kp.GenerateTLSConfig()
		if tlsErr != nil {
			return "", fmt.Errorf("mesh TLS config: %w", tlsErr)
		}
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", address, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", address, 10*time.Second)
	}
	if err != nil {
		return "", fmt.Errorf("dial %s: %w", address, err)
	}

	// Send our HELLO
	if err := sendTyped(conn, MsgHello, t.makeHello()); err != nil {
		conn.Close()
		return "", fmt.Errorf("send HELLO to %s: %w", address, err)
	}

	// Read peer's HELLO reply
	env, err := readMessage(conn, 10*time.Second)
	if err != nil {
		conn.Close()
		return "", fmt.Errorf("read HELLO from %s: %w", address, err)
	}
	if env.Type != MsgHello {
		conn.Close()
		return "", fmt.Errorf("expected HELLO from %s, got %s", address, env.Type)
	}

	var peerHello HelloMessage
	if err := decodePayload(env, &peerHello); err != nil {
		conn.Close()
		return "", fmt.Errorf("decode HELLO from %s: %w", address, err)
	}

	// Don't connect to ourselves
	if peerHello.RegistryID == t.kp.RegistryID() {
		conn.Close()
		return "", fmt.Errorf("refusing connection to self")
	}

	// Check if we already have this peer
	t.mu.RLock()
	_, exists := t.conns[peerHello.RegistryID]
	t.mu.RUnlock()
	if exists {
		conn.Close()
		return peerHello.RegistryID, nil // already connected
	}

	// Register the peer
	peerAddr := resolvePeerAddr(address, peerHello.ListenPort)
	t.registerPeer(peerHello, peerAddr, conn)

	// Start read loop in background
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer t.unregisterPeer(peerHello.RegistryID, conn)
		t.readLoop(conn, peerHello.RegistryID)
	}()

	return peerHello.RegistryID, nil
}

// registerPeer adds a peer connection to the transport and peer manager.
func (t *Transport) registerPeer(hello HelloMessage, address string, conn net.Conn) {
	pc := &peerConn{
		conn:       conn,
		registryID: hello.RegistryID,
		name:       hello.Name,
		address:    address,
	}

	t.mu.Lock()
	t.conns[hello.RegistryID] = pc
	t.mu.Unlock()

	t.peerMgr.AddPeer(&models.PeerInfo{
		RegistryID: hello.RegistryID,
		Name:       hello.Name,
		Address:    address,
		PublicKey:  hello.PublicKey,
		AgentCount: hello.AgentCount,
	})

	idPrefix := hello.RegistryID
	if len(idPrefix) > 24 {
		idPrefix = idPrefix[:24]
	}
	log.Printf("mesh: peer connected: %s (%s) at %s", hello.Name, idPrefix, address)

	if t.eventBus != nil {
		t.eventBus.Publish(events.EventPeerConnected, events.PeerEventData{
			RegistryID: hello.RegistryID,
			Name:       hello.Name,
			Address:    address,
		})
	}
}

// unregisterPeer removes a peer connection.
func (t *Transport) unregisterPeer(registryID string, conn net.Conn) {
	conn.Close()

	t.mu.Lock()
	pc := t.conns[registryID]
	delete(t.conns, registryID)
	t.mu.Unlock()

	t.peerMgr.RemovePeer(registryID)

	idPrefix := registryID
	if len(idPrefix) > 24 {
		idPrefix = idPrefix[:24]
	}
	log.Printf("mesh: peer disconnected: %s", idPrefix)

	if t.eventBus != nil {
		name := ""
		address := ""
		if pc != nil {
			name = pc.name
			address = pc.address
		}
		t.eventBus.Publish(events.EventPeerDisconnected, events.PeerEventData{
			RegistryID: registryID,
			Name:       name,
			Address:    address,
		})
	}
}

// readLoop reads and dispatches messages from a peer connection.
func (t *Transport) readLoop(conn net.Conn, peerID string) {
	idPrefix := peerID
	if len(idPrefix) > 24 {
		idPrefix = idPrefix[:24]
	}

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		// Heartbeats come every ~30s; 90s timeout = 3 missed heartbeats.
		env, err := readMessage(conn, 90*time.Second)
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
				log.Printf("mesh: read from %s: %v", idPrefix, err)
				return
			}
		}

		switch env.Type {
		case MsgHeartbeat:
			t.handleHeartbeat(peerID, env)
		case MsgGossip:
			t.handleGossipMessage(peerID, env)
		case MsgSearch:
			t.handleSearchRequest(peerID, conn, env)
		case MsgSearchAck:
			log.Printf("mesh: unexpected SearchAck from %s", idPrefix)
		case MsgDHT:
			t.handleDHTMessage(peerID, conn, env)
		default:
			log.Printf("mesh: unknown message type %q from %s", env.Type, idPrefix)
		}
	}
}

// handleHeartbeat processes an incoming heartbeat from a peer.
func (t *Transport) handleHeartbeat(peerID string, env *Envelope) {
	var hb HeartbeatMessage
	if err := decodePayload(env, &hb); err != nil {
		log.Printf("mesh: decode heartbeat: %v", err)
		return
	}

	t.peerMgr.UpdatePeerLastSeen(peerID)

	// Update the peer's bloom filter
	if len(hb.BloomFilter) > 0 && hb.BloomSize > 0 {
		bf := &BloomFilter{
			bits:    make([]bool, hb.BloomSize),
			size:    hb.BloomSize,
			hashNum: hb.BloomHashes,
		}
		bf.FromBytes(hb.BloomFilter)
		t.peerMgr.SetPeerBloomFilter(peerID, bf)
	}

	// Peer exchange: try to connect to peers we don't know about
	for _, addr := range hb.PeerAddrs {
		if !t.isKnownAddress(addr) {
			t.wg.Add(1)
			go func(a string) {
				defer t.wg.Done()
				if _, err := t.ConnectTo(a); err != nil {
					log.Printf("mesh: peer exchange dial %s: %v", a, err)
				}
			}(addr)
		}
	}
}

// handleGossipMessage processes an incoming gossip announcement.
func (t *Transport) handleGossipMessage(peerID string, env *Envelope) {
	var gm GossipMessage
	if err := decodePayload(env, &gm); err != nil {
		log.Printf("mesh: decode gossip: %v", err)
		return
	}

	if gm.Announcement == nil {
		return
	}

	// Process through the gossip handler (dedup, store, index callback)
	shouldForward := t.gossip.HandleAnnouncement(gm.Announcement)
	if shouldForward {
		t.BroadcastExcept(gm.Announcement, peerID)
	}
}

// handleSearchRequest processes an incoming federated search request.
func (t *Transport) handleSearchRequest(peerID string, conn net.Conn, env *Envelope) {
	var sm SearchMessage
	if err := decodePayload(env, &sm); err != nil {
		log.Printf("mesh: decode search request: %v", err)
		return
	}

	ack := SearchAckMessage{
		RequestID: sm.RequestID,
		Results:   []models.SearchResult{},
	}

	if t.onSearch != nil {
		// Execute local-only search to avoid infinite recursion
		localReq := *sm.Request
		localReq.Federated = false

		resp, err := t.onSearch(&localReq)
		if err == nil && resp != nil {
			ack.Results = resp.Results
			ack.Stats = resp.SearchStats
		}
	}

	t.mu.RLock()
	pc, ok := t.conns[peerID]
	t.mu.RUnlock()
	if ok {
		if err := pc.send(MsgSearchAck, &ack); err != nil {
			idPrefix := peerID
			if len(idPrefix) > 24 {
				idPrefix = idPrefix[:24]
			}
			log.Printf("mesh: send search ack to %s: %v", idPrefix, err)
		}
	}
}

// handleDHTMessage processes an incoming DHT message.
func (t *Transport) handleDHTMessage(peerID string, conn net.Conn, env *Envelope) {
	// Check if this is a response to a pending request
	var peek struct {
		RequestID string `json:"request_id"`
		Type      string `json:"type"`
	}
	json.Unmarshal(env.Payload, &peek)

	// Check if it's a response to one of our pending requests
	isReply := peek.Type == "dht_ping_reply" || peek.Type == "dht_store_reply" ||
		peek.Type == "dht_find_node_reply" || peek.Type == "dht_find_value_reply"

	if isReply && peek.RequestID != "" {
		t.dhtPendingMu.Lock()
		ch, ok := t.dhtPending[peek.RequestID]
		t.dhtPendingMu.Unlock()
		if ok {
			select {
			case ch <- env.Payload:
			default:
			}
			return
		}
	}

	// It's a request — process and send response
	if t.onDHT != nil {
		respBytes := t.onDHT(env.Payload)
		if respBytes != nil {
			respEnv := Envelope{Type: MsgDHT, Payload: respBytes}
			t.mu.RLock()
			pc, ok := t.conns[peerID]
			t.mu.RUnlock()
			if ok {
				pc.mu.Lock()
				writeMessage(pc.conn, &respEnv)
				pc.mu.Unlock()
			}
		}
	}
}

// Broadcast sends a gossip announcement to all connected peers.
func (t *Transport) Broadcast(ann *models.GossipAnnouncement) {
	t.BroadcastExcept(ann, "")
}

// BroadcastExcept sends a gossip announcement to all peers except the given one.
func (t *Transport) BroadcastExcept(ann *models.GossipAnnouncement, exceptID string) {
	gm := GossipMessage{Announcement: ann}

	t.mu.RLock()
	peers := make([]*peerConn, 0, len(t.conns))
	for id, pc := range t.conns {
		if id != exceptID {
			peers = append(peers, pc)
		}
	}
	t.mu.RUnlock()

	for _, pc := range peers {
		if err := pc.send(MsgGossip, &gm); err != nil {
			idPrefix := pc.registryID
			if len(idPrefix) > 24 {
				idPrefix = idPrefix[:24]
			}
			log.Printf("mesh: broadcast to %s: %v", idPrefix, err)
		}
	}
}

// ConnectedPeerCount returns the number of active TCP connections.
func (t *Transport) ConnectedPeerCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.conns)
}

// GetPeerAddresses returns the mesh addresses of all connected peers.
func (t *Transport) GetPeerAddresses() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	addrs := make([]string, 0, len(t.conns))
	for _, pc := range t.conns {
		if pc.address != "" {
			addrs = append(addrs, pc.address)
		}
	}
	return addrs
}

// SendSearchRequest sends a federated search request to a specific peer and
// reads the response synchronously. Called from the federated search fan-out.
func (t *Transport) SendSearchRequest(peerID string, msg *SearchMessage) (*SearchAckMessage, error) {
	t.mu.RLock()
	pc, ok := t.conns[peerID]
	t.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("peer not connected")
	}

	// Hold the write lock while we do the request-response exchange
	// to prevent interleaving with other messages.
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if err := sendTyped(pc.conn, MsgSearch, msg); err != nil {
		return nil, fmt.Errorf("send search: %w", err)
	}

	env, err := readMessage(pc.conn, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("read search response: %w", err)
	}
	if env.Type != MsgSearchAck {
		return nil, fmt.Errorf("expected SearchAck, got %s", env.Type)
	}

	var ack SearchAckMessage
	if err := decodePayload(env, &ack); err != nil {
		return nil, fmt.Errorf("decode search ack: %w", err)
	}

	return &ack, nil
}

// isKnownAddress checks if we're already connected to a peer at the given address.
func (t *Transport) isKnownAddress(addr string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, pc := range t.conns {
		if pc.address == addr {
			return true
		}
	}
	return false
}

// resolvePeerAddr resolves the peer's mesh listen address from the remote
// connection address and the peer's declared listen port.
func resolvePeerAddr(remoteAddr string, listenPort int) string {
	if listenPort > 0 {
		host, _, err := net.SplitHostPort(remoteAddr)
		if err == nil {
			return fmt.Sprintf("%s:%d", host, listenPort)
		}
	}
	return remoteAddr
}
