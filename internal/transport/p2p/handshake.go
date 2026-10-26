package p2p

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

// HandshakeFunc performs authentication between peers
type HandshakeFunc func(Peer) error

// NOPHandshakeFunc is a no-operation handshake for backward compatibility
func NOPHandshakeFunc(Peer) error { return nil }

// HandshakeMessage represents the data exchanged during handshake
type HandshakeMessage struct {
	NodeID    string
	Timestamp int64
	Signature []byte
}

// AuthenticatedHandshakeFunc creates a handshake function that verifies peer identity
func AuthenticatedHandshakeFunc(nodeID string) HandshakeFunc {
	return func(peer Peer) error {
		authToken := os.Getenv("SHUGO_AUTH_TOKEN")
		if authToken == "" {
			return fmt.Errorf("SHUGO_AUTH_TOKEN is required")
		}

		// Create handshake message
		msg := HandshakeMessage{
			NodeID:    nodeID,
			Timestamp: time.Now().Unix(),
		}

		// Sign the message
		msg.Signature = SignHandshakeMessage(msg, authToken)

		// Send handshake
		if err := sendHandshake(peer, msg); err != nil {
			return fmt.Errorf("failed to send handshake: %w", err)
		}

		// Receive and verify handshake
		peerMsg, err := receiveHandshake(peer)
		if err != nil {
			return fmt.Errorf("failed to receive handshake: %w", err)
		}

		// Verify peer signature
		if !VerifyHandshakeMessage(peerMsg, authToken) {
			return fmt.Errorf("invalid handshake signature from peer %s", peer.RemoteAddr())
		}

		// Check timestamp (allow 30 second clock skew)
		if time.Now().Unix()-peerMsg.Timestamp > 30 {
			return fmt.Errorf("handshake timestamp too old from peer %s", peer.RemoteAddr())
		}

		slog.Info("authenticated handshake with peer", slog.String("peer", peer.RemoteAddr().String()), slog.String("node", peerMsg.NodeID))
		return nil
	}
}

// SignHandshakeMessage creates a signature for the handshake message
func SignHandshakeMessage(msg HandshakeMessage, authToken string) []byte {
	h := hmac.New(sha256.New, []byte(authToken))
	h.Write([]byte(msg.NodeID))

	// Write timestamp as 8 bytes
	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, uint64(msg.Timestamp))
	h.Write(timestampBytes)

	return h.Sum(nil)
}

// VerifyHandshakeMessage verifies the signature of a handshake message
func VerifyHandshakeMessage(msg HandshakeMessage, authToken string) bool {
	expectedSignature := SignHandshakeMessage(msg, authToken)
	return hmac.Equal(msg.Signature, expectedSignature)
}

// sendHandshake sends a handshake message to a peer
func sendHandshake(peer Peer, msg HandshakeMessage) error {
	// Write message length
	msgBytes := SerializeHandshakeMessage(msg)
	length := uint32(len(msgBytes))
	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, length)

	if _, err := peer.Write(lengthBytes); err != nil {
		return err
	}

	// Write message
	if _, err := peer.Write(msgBytes); err != nil {
		return err
	}

	return nil
}

// receiveHandshake receives a handshake message from a peer
func receiveHandshake(peer Peer) (HandshakeMessage, error) {
	// Read message length
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(peer, lengthBytes); err != nil {
		return HandshakeMessage{}, err
	}
	length := binary.BigEndian.Uint32(lengthBytes)

	// Read message
	msgBytes := make([]byte, length)
	if _, err := io.ReadFull(peer, msgBytes); err != nil {
		return HandshakeMessage{}, err
	}

	return DeserializeHandshakeMessage(msgBytes)
}

// SerializeHandshakeMessage converts a handshake message to bytes
func SerializeHandshakeMessage(msg HandshakeMessage) []byte {
	// Simple serialization: nodeID length + nodeID + timestamp + signature length + signature
	nodeIDBytes := []byte(msg.NodeID)
	nodeIDLen := uint16(len(nodeIDBytes))
	sigLen := uint16(len(msg.Signature))

	totalLen := 2 + len(nodeIDBytes) + 8 + 2 + len(msg.Signature)
	result := make([]byte, totalLen)

	offset := 0

	// NodeID length
	binary.BigEndian.PutUint16(result[offset:], nodeIDLen)
	offset += 2

	// NodeID
	copy(result[offset:], nodeIDBytes)
	offset += len(nodeIDBytes)

	// Timestamp
	binary.BigEndian.PutUint64(result[offset:], uint64(msg.Timestamp))
	offset += 8

	// Signature length
	binary.BigEndian.PutUint16(result[offset:], sigLen)
	offset += 2

	// Signature
	copy(result[offset:], msg.Signature)

	return result
}

// DeserializeHandshakeMessage converts bytes to a handshake message
func DeserializeHandshakeMessage(data []byte) (HandshakeMessage, error) {
	if len(data) < 12 { // minimum: 2 + 0 + 8 + 2 + 0
		return HandshakeMessage{}, fmt.Errorf("handshake message too short")
	}

	offset := 0

	// NodeID length
	nodeIDLen := binary.BigEndian.Uint16(data[offset:])
	offset += 2

	if offset+int(nodeIDLen) > len(data) {
		return HandshakeMessage{}, fmt.Errorf("invalid nodeID length")
	}

	// NodeID
	nodeID := string(data[offset : offset+int(nodeIDLen)])
	offset += int(nodeIDLen)

	if offset+8 > len(data) {
		return HandshakeMessage{}, fmt.Errorf("message too short for timestamp")
	}

	// Timestamp
	timestamp := int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	if offset+2 > len(data) {
		return HandshakeMessage{}, fmt.Errorf("message too short for signature length")
	}

	// Signature length
	sigLen := binary.BigEndian.Uint16(data[offset:])
	offset += 2

	if offset+int(sigLen) != len(data) {
		return HandshakeMessage{}, fmt.Errorf("invalid signature length")
	}

	// Signature
	signature := make([]byte, sigLen)
	copy(signature, data[offset:])

	return HandshakeMessage{
		NodeID:    nodeID,
		Timestamp: timestamp,
		Signature: signature,
	}, nil
}
