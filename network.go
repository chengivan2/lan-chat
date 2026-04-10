package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// MessagePayload represents the actual content sent over TCP
type MessagePayload struct {
	Sender    string `json:"sender"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// DeliveryResult represents the outcome of sending a message to a peer
type DeliveryResult struct {
	PeerIP   string
	Username string
	Success  bool
	Error    error
}

var aesKey []byte

// InitEncryption derives the 32-byte AES key from the room password
func InitEncryption(roomPassword string) {
	if roomPassword == "" {
		roomPassword = "LAN-CHAT-DEFAULT"
	}
	hash := sha256.Sum256([]byte(roomPassword))
	aesKey = make([]byte, 32)
	copy(aesKey, hash[:])
}

// Encrypt payload using AES-GCM
func Encrypt(plaintext []byte) ([]byte, error) {
	if aesKey == nil {
		return nil, errors.New("encryption key not initialized")
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Prepend nonce to the ciphertext
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt ciphertext using AES-GCM
func Decrypt(ciphertext []byte) ([]byte, error) {
	if aesKey == nil {
		return nil, errors.New("encryption key not initialized")
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, encryptedBody := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encryptedBody, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

// StartTCPServer binds to a free port in 49153-65535 range
func StartTCPServer(messageHandler func(MessagePayload)) (int, error) {
	port := 49153
	var listener net.Listener
	var err error

	for port <= 65535 {
		listener, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
		if err == nil {
			break
		}
		port++
	}

	if err != nil {
		return 0, fmt.Errorf("could not find a free port: %w", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			go handleIncomingTCP(conn, messageHandler)
		}
	}()

	return port, nil
}

func handleIncomingTCP(conn net.Conn, messageHandler func(MessagePayload)) {
	defer conn.Close()

	// For simple chat, we just read all until EOF. In a real stream, we'd use length-prefix.
	// Setting a deadline so we don't hang forever
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, err := io.ReadAll(conn)
	if err != nil {
		return
	}

	plaintext, err := Decrypt(data)
	if err != nil {
		// Failed to decrypt, might be wrong network password or junk data
		return
	}

	var msg MessagePayload
	if err := json.Unmarshal(plaintext, &msg); err != nil {
		return
	}

	messageHandler(msg)
}

// SendMessages attempts parallel delivery to all peers in the registry.
// If targetUsername is not empty, it only sends to peers matching that string.
func SendMessages(peers map[string]PeerInfo, sender string, content string, targetUsername string, resultsChan chan<- []DeliveryResult) {
	msg := MessagePayload{
		Sender:    sender,
		Content:   content,
		Timestamp: time.Now().Unix(),
	}

	plaintext, err := json.Marshal(msg)
	if err != nil {
		return
	}

	ciphertext, err := Encrypt(plaintext)
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]DeliveryResult, 0, len(peers))

	for ip, peerInfo := range peers {
		// Filter by targetUsername if specified
		if targetUsername != "" && peerInfo.Username != targetUsername {
			continue
		}

		wg.Add(1)
		go func(targetIP string, targetPort int, targetUser string) {
			defer wg.Done()

			res := DeliveryResult{
				PeerIP:   targetIP,
				Username: targetUser,
				Success:  false,
			}

			// Dial with a short timeout
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", targetIP, targetPort), 2*time.Second)
			if err == nil {
				defer conn.Close()
				conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				_, err = conn.Write(ciphertext)
				if err == nil {
					res.Success = true
				} else {
					res.Error = err
				}
			} else {
				res.Error = err
			}

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(ip, peerInfo.TCPPort, peerInfo.Username)
	}

	wg.Wait()
	resultsChan <- results
}
