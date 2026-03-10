package mesh

import (
	"log"
	"math"
	"time"
)

// BootstrapConnect dials all configured bootstrap peers with exponential backoff.
// Runs in a loop until all peers are connected or the transport is stopped.
func (t *Transport) BootstrapConnect() {
	if len(t.cfg.BootstrapPeers) == 0 {
		log.Printf("mesh: no bootstrap peers configured")
		return
	}

	log.Printf("mesh: bootstrapping with %d peers: %v", len(t.cfg.BootstrapPeers), t.cfg.BootstrapPeers)

	for _, addr := range t.cfg.BootstrapPeers {
		t.wg.Add(1)
		go func(peerAddr string) {
			defer t.wg.Done()
			t.connectWithBackoff(peerAddr)
		}(addr)
	}
}

// connectWithBackoff dials a peer address with exponential backoff.
// Retries indefinitely until connected or the transport is stopped.
func (t *Transport) connectWithBackoff(address string) {
	const (
		initialDelay = 1 * time.Second
		maxDelay     = 60 * time.Second
		maxAttempts  = 0 // 0 = unlimited
	)

	attempt := 0
	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		_, err := t.ConnectTo(address)
		if err == nil {
			log.Printf("mesh: bootstrap connected to %s", address)
			return
		}

		attempt++
		delay := time.Duration(float64(initialDelay) * math.Pow(2, float64(attempt-1)))
		if delay > maxDelay {
			delay = maxDelay
		}

		log.Printf("mesh: bootstrap connect to %s failed (attempt %d): %v — retrying in %s", address, attempt, err, delay)

		select {
		case <-t.stopCh:
			return
		case <-time.After(delay):
		}
	}
}

// ReconnectLoop monitors connected peers and attempts to reconnect
// to bootstrap peers if they disconnect. Runs until Stop().
func (t *Transport) ReconnectLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			// Check if any bootstrap peers are disconnected
			for _, addr := range t.cfg.BootstrapPeers {
				if !t.isKnownAddress(addr) {
					t.wg.Add(1)
					go func(a string) {
						defer t.wg.Done()
						if _, err := t.ConnectTo(a); err != nil {
							log.Printf("mesh: reconnect to bootstrap %s: %v", a, err)
						}
					}(addr)
				}
			}
		}
	}
}
