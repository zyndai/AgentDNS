package dht

import (
	"encoding/json"
	"fmt"
)

// MeshTransport adapts the mesh transport layer to the DHT Transport interface.
type MeshTransport struct {
	// SendFunc sends raw DHT bytes to a peer address and returns the response.
	SendFunc func(peerAddr string, msgBytes json.RawMessage) (json.RawMessage, error)
}

// SendDHT sends a DHT message to a peer via the mesh transport.
func (mt *MeshTransport) SendDHT(peerAddr string, msg Message) (Message, error) {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return Message{}, fmt.Errorf("marshal DHT message: %w", err)
	}

	respBytes, err := mt.SendFunc(peerAddr, msgBytes)
	if err != nil {
		return Message{}, err
	}

	var resp Message
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return Message{}, fmt.Errorf("unmarshal DHT response: %w", err)
	}
	return resp, nil
}

// HandleRawMessage is the callback that processes incoming DHT messages.
// Wire this to transport.SetDHTHandler().
func HandleRawMessage(d *DHT) func(json.RawMessage) json.RawMessage {
	return func(raw json.RawMessage) json.RawMessage {
		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil
		}
		resp := d.HandleMessage(msg)
		respBytes, _ := json.Marshal(resp)
		return respBytes
	}
}
