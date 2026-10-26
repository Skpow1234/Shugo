package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	// GCM nonce size is 12 bytes (96 bits) for optimal performance
	GCMNonceSize = 12
	// GCM tag size is 16 bytes (128 bits)
	GCMTagSize = 16
	// Key derivation constants
	KeyDerivationSalt = "shugo-cluster-salt-v1"
	KeyRotationPeriod = 24 * time.Hour // Rotate keys every 24 hours
)

// KeyManager handles encryption key generation, derivation, and rotation
type KeyManager struct {
	clusterKey []byte
	derivedKey []byte
	keyID      string
	createdAt  time.Time
}

// NewKeyManager creates a new key manager with proper key derivation
func NewKeyManager() (*KeyManager, error) {
	// Try to load cluster key from environment first
	clusterKey := os.Getenv("SHUGO_CLUSTER_KEY")
	if clusterKey == "" {
		// Generate a new cluster key if not provided
		clusterKeyBytes := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, clusterKeyBytes); err != nil {
			return nil, err
		}
		clusterKey = hex.EncodeToString(clusterKeyBytes)
		// In production, you'd want to securely store this key
		// For demo purposes, we'll just use it in memory
	}

	clusterKeyBytes, err := hex.DecodeString(clusterKey)
	if err != nil {
		return nil, err
	}
	return NewKeyManagerFromClusterKey(clusterKeyBytes)
}

// NewKeyManagerFromClusterKey builds a KeyManager from raw cluster key bytes.
// Use this so multiple nodes in a demo/test share the same derived encryption key.
func NewKeyManagerFromClusterKey(clusterKeyBytes []byte) (*KeyManager, error) {
	if len(clusterKeyBytes) == 0 {
		return nil, fmt.Errorf("cluster key must not be empty")
	}
	derivedKey := deriveKey(clusterKeyBytes, KeyDerivationSalt)
	return &KeyManager{
		clusterKey: append([]byte(nil), clusterKeyBytes...),
		derivedKey: derivedKey,
		keyID:      generateKeyID(derivedKey),
		createdAt:  time.Now(),
	}, nil
}

// deriveKey derives an encryption key from the cluster key using HMAC-SHA256
func deriveKey(clusterKey []byte, salt string) []byte {
	h := hmac.New(sha256.New, clusterKey)
	h.Write([]byte(salt))
	return h.Sum(nil)
}

// generateKeyID creates a unique identifier for the key
func generateKeyID(key []byte) string {
	hash := sha256.Sum256(key)
	return hex.EncodeToString(hash[:8]) // First 8 bytes as key ID
}

// GetEncryptionKey returns the current encryption key
func (km *KeyManager) GetEncryptionKey() []byte {
	return km.derivedKey
}

// GetKeyID returns the current key identifier
func (km *KeyManager) GetKeyID() string {
	return km.keyID
}

// ShouldRotate checks if the key should be rotated
func (km *KeyManager) ShouldRotate() bool {
	return time.Since(km.createdAt) > KeyRotationPeriod
}

// RotateKey generates a new derived key
func (km *KeyManager) RotateKey() error {
	// For demo simplicity, we'll just regenerate the derived key
	// In production, you'd want to implement proper key rotation with key versioning
	km.derivedKey = deriveKey(km.clusterKey, KeyDerivationSalt+time.Now().Format("2006-01-02"))
	km.keyID = generateKeyID(km.derivedKey)
	km.createdAt = time.Now()
	return nil
}

func GenerateID() string {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		// Fallback to time-based ID if random generation fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// NewEncryptionKey is deprecated - use KeyManager instead.
// This is kept for backward compatibility.
func NewEncryptionKey() ([]byte, error) {
	keyBuf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, keyBuf); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	return keyBuf, nil
}

const (
	// DefaultChunkSize is the plaintext size per AES-GCM chunk for streaming encryption.
	DefaultChunkSize = 64 * 1024
)

// chunkedMagic marks the streaming (chunked) AES-GCM container format.
var chunkedMagic = []byte("PVCH")

// CopyEncrypt encrypts src to dst using chunked AES-GCM so large files are not
// buffered entirely in memory. Wire format:
//
//	"PVCH" | chunkPlainMax(u32 BE) | repeated (ctLen(u32 BE)|nonce|ciphertext) | ctLen=0
//
// Each chunk uses a fresh nonce and AAD = chunk index (u64 BE) to prevent reordering.
func CopyEncrypt(key []byte, src io.Reader, dst io.Writer) (int, error) {
	return CopyEncryptChunked(key, src, dst, DefaultChunkSize)
}

// CopyEncryptChunked is like CopyEncrypt with an explicit plaintext chunk size.
func CopyEncryptChunked(key []byte, src io.Reader, dst io.Writer, chunkSize int) (int, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, err
	}

	written := 0
	if _, err := dst.Write(chunkedMagic); err != nil {
		return written, err
	}
	written += len(chunkedMagic)

	hdr := make([]byte, 4)
	binaryBigEndianPutUint32(hdr, uint32(chunkSize))
	if _, err := dst.Write(hdr); err != nil {
		return written, err
	}
	written += 4

	buf := make([]byte, chunkSize)
	lenBuf := make([]byte, 4)
	var chunkIndex uint64
	for {
		n, readErr := io.ReadFull(src, buf)
		if n > 0 {
			nonce := make([]byte, gcm.NonceSize())
			if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
				return written, err
			}
			aad := make([]byte, 8)
			binaryBigEndianPutUint64(aad, chunkIndex)
			ciphertext := gcm.Seal(nil, nonce, buf[:n], aad)

			// Per chunk: ctLen | nonce | ciphertext
			binaryBigEndianPutUint32(lenBuf, uint32(len(ciphertext)))
			if _, err := dst.Write(lenBuf); err != nil {
				return written, err
			}
			written += 4
			if _, err := dst.Write(nonce); err != nil {
				return written, err
			}
			written += len(nonce)
			nw, err := dst.Write(ciphertext)
			written += nw
			if err != nil {
				return written, err
			}
			chunkIndex++
		}

		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return written, readErr
		}
	}

	// End marker: ctLen = 0
	binaryBigEndianPutUint32(lenBuf, 0)
	if _, err := dst.Write(lenBuf); err != nil {
		return written, err
	}
	written += 4
	return written, nil
}

// CopyDecrypt decrypts data produced by CopyEncrypt (chunked) or the legacy
// single-blob format (nonce || ciphertext+tag) for backward compatibility.
func CopyDecrypt(key []byte, src io.Reader, dst io.Writer) (int, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, err
	}

	magic := make([]byte, len(chunkedMagic))
	if _, err := io.ReadFull(src, magic); err != nil {
		return 0, err
	}

	if string(magic) != string(chunkedMagic) {
		// Legacy format: magic bytes were the start of the 12-byte nonce.
		return copyDecryptLegacy(gcm, magic, src, dst)
	}

	hdr := make([]byte, 4)
	if _, err := io.ReadFull(src, hdr); err != nil {
		return 0, fmt.Errorf("read chunk size header: %w", err)
	}
	_ = binaryBigEndianUint32(hdr) // chunkPlainMax — informational

	written := 0
	var chunkIndex uint64
	nonce := make([]byte, gcm.NonceSize())
	lenBuf := make([]byte, 4)

	for {
		if _, err := io.ReadFull(src, lenBuf); err != nil {
			return written, fmt.Errorf("read chunk length: %w", err)
		}
		ctLen := binaryBigEndianUint32(lenBuf)
		if ctLen == 0 {
			return written, nil
		}
		if ctLen > MaxFrameCiphertext {
			return written, fmt.Errorf("chunk ciphertext too large: %d", ctLen)
		}

		if _, err := io.ReadFull(src, nonce); err != nil {
			return written, fmt.Errorf("read chunk nonce: %w", err)
		}

		ciphertext := make([]byte, ctLen)
		if _, err := io.ReadFull(src, ciphertext); err != nil {
			return written, fmt.Errorf("read chunk ciphertext: %w", err)
		}

		aad := make([]byte, 8)
		binaryBigEndianPutUint64(aad, chunkIndex)
		plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
		if err != nil {
			return written, fmt.Errorf("decrypt chunk %d: %w", chunkIndex, err)
		}
		nw, err := dst.Write(plaintext)
		written += nw
		if err != nil {
			return written, err
		}
		chunkIndex++
	}
}

// MaxFrameCiphertext caps a single chunk to limit memory use (10MB).
const MaxFrameCiphertext = 10 * 1024 * 1024

func copyDecryptLegacy(gcm cipher.AEAD, firstNonceBytes []byte, src io.Reader, dst io.Writer) (int, error) {
	nonce := make([]byte, gcm.NonceSize())
	copy(nonce, firstNonceBytes)
	remaining := gcm.NonceSize() - len(firstNonceBytes)
	if remaining > 0 {
		if _, err := io.ReadFull(src, nonce[len(firstNonceBytes):]); err != nil {
			return 0, err
		}
	}

	ciphertext, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return 0, err
	}
	return dst.Write(plaintext)
}

func binaryBigEndianPutUint32(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

func binaryBigEndianUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func binaryBigEndianPutUint64(b []byte, v uint64) {
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
}
