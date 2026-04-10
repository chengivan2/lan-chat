package main

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// PeerInfo holds information about a discovered peer on the network
type PeerInfo struct {
	Username string
	TCPPort  int
	LastSeen time.Time
}

// Global thread-safe registry of active peers
var Registry = struct {
	sync.RWMutex
	Peers map[string]PeerInfo
}{Peers: make(map[string]PeerInfo)}

// HeartbeatPayload represents the data sent over UDP broadcast
type HeartbeatPayload struct {
	Username string `json:"username"`
	TCPPort  int    `json:"tcp_port"`
}

const UDP_PORT = 49152

// StartDiscovery handles both listening for external heartbeats and broadcasting our own
func StartDiscovery(username string, tcpPort int, updateUI chan<- struct{}) {
	go listenUDP(updateUI)
	go broadcastLoop(username, tcpPort)
	go startPruner(updateUI)
}

func listenUDP(updateUI chan<- struct{}) {
	// Bind to 0.0.0.0 to listen across all interfaces (2.4/5Ghz bridged by router on LAN)
	addr := net.UDPAddr{
		Port: UDP_PORT,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return
	}
	defer conn.Close()

	buf := make([]byte, 2048)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		// Decrypt the heartbeat payload
		plaintext, err := Decrypt(buf[:n])
		if err != nil {
			// Decryption failed. Normal if they are in a different LAN room with different password.
			continue
		}

		var hb HeartbeatPayload
		if err := json.Unmarshal(plaintext, &hb); err != nil {
			continue
		}

		ipStr := remoteAddr.IP.String()

		Registry.Lock()
		// If new peer, notify UI
		isNew := false
		if _, exists := Registry.Peers[ipStr]; !exists {
			isNew = true
		}

		Registry.Peers[ipStr] = PeerInfo{
			Username: hb.Username,
			TCPPort:  hb.TCPPort,
			LastSeen: time.Now(),
		}
		Registry.Unlock()

		if isNew {
			// Try to trigger a non-blocking update
			select {
			case updateUI <- struct{}{}:
			default:
			}
		}
	}
}

func broadcastLoop(username string, tcpPort int) {
	hb := HeartbeatPayload{
		Username: username,
		TCPPort:  tcpPort,
	}

	plaintext, err := json.Marshal(hb)
	if err != nil {
		return
	}

	buf, err := Encrypt(plaintext)
	if err != nil {
		return
	}

	for {
		bcasts := getBroadcastAddresses()
		for _, bcast := range bcasts {
			addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", bcast, UDP_PORT))
			if err != nil {
				continue
			}
			
			conn, err := net.DialUDP("udp", nil, addr)
			if err == nil {
				conn.Write(buf)
				conn.Close()
			}
		}
		// Send heartbeat every 10 seconds
		time.Sleep(10 * time.Second)
	}
}

func startPruner(updateUI chan<- struct{}) {
	for {
		time.Sleep(5 * time.Second)
		now := time.Now()
		
		Registry.Lock()
		pruned := false
		for ip, peer := range Registry.Peers {
			if now.Sub(peer.LastSeen) > 30*time.Second {
				delete(Registry.Peers, ip)
				pruned = true
			}
		}
		Registry.Unlock()

		if pruned {
			select {
			case updateUI <- struct{}{}:
			default:
			}
		}
	}
}

// Calculate UDP subnet broadcast IPs
func getBroadcastAddresses() []string {
	var addrs []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{"255.255.255.255"}
	}
	for _, i := range ifaces {
		// Ignore interfaces that are down or do not support broadcast
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrsExt, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrsExt {
			if ipNet, ok := addr.(*net.IPNet); ok {
				if ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
					ip := ipNet.IP.To4()
					mask := ipNet.Mask
					bcast := make(net.IP, 4)
					for i := range ip {
						bcast[i] = ip[i] | ^mask[i]
					}
					addrs = append(addrs, bcast.String())
				}
			}
		}
	}
	if len(addrs) == 0 {
		return []string{"255.255.255.255"}
	}
	return addrs
}

// GetActivePeers is a helper to securely read from the registry
func GetActivePeers() map[string]PeerInfo {
	Registry.RLock()
	defer Registry.RUnlock()

	// Return a copy so we don't bleed reference
	copyMap := make(map[string]PeerInfo)
	for k, v := range Registry.Peers {
		copyMap[k] = v
	}
	return copyMap
}
