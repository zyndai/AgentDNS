package models

// HeartbeatMessage is sent by agents over WebSocket to prove liveness.
type HeartbeatMessage struct {
	Timestamp string `json:"timestamp"` // RFC3339
	Signature string `json:"signature"` // ed25519:<base64> over timestamp bytes
}
