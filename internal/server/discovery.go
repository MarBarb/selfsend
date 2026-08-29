package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const discoveryPort = 38081

type discoveryPacket struct {
	Type       string `json:"type"`
	Nonce      string `json:"nonce"`
	InstanceID string `json:"instance_id,omitempty"`
	Name       string `json:"name,omitempty"`
	State      string `json:"state,omitempty"`
	Port       int    `json:"port,omitempty"`
	Receiver   bool   `json:"receiver,omitempty"`
}

type DiscoveredServer struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	URL        string `json:"url"`
	Receiver   bool   `json:"receiver"`
}

func (a *App) runDiscovery(ctx context.Context) {
	address := &net.UDPAddr{IP: net.IPv4zero, Port: discoveryPort}
	connection, err := net.ListenUDP("udp4", address)
	if err != nil {
		a.logger.Warn("local discovery is unavailable", "error", err)
		return
	}
	defer connection.Close()
	enableUDPBroadcast(connection)
	go func() { <-ctx.Done(); _ = connection.Close() }()
	buffer := make([]byte, 2048)
	for {
		count, sender, err := connection.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		var packet discoveryPacket
		if json.Unmarshal(buffer[:count], &packet) != nil || packet.Type != "discover" || packet.Nonce == "" {
			continue
		}
		info, err := a.store.ServerInfo(ctx)
		if err != nil {
			continue
		}
		receiver, receiverErr := loadReceiver(a.config.DataDir)
		name := info.InstanceName
		if receiverErr == nil && receiver.HostName != "" {
			name = receiver.HostName
		}
		port := listenPort(a.config.ListenAddr)
		response := discoveryPacket{Type: "offer", Nonce: packet.Nonce, InstanceID: info.InstanceID, Name: name, State: info.State, Port: port, Receiver: receiverErr == nil && (receiver.State == "waiting" || receiver.State == "uploading")}
		data, _ := json.Marshal(response)
		_, _ = connection.WriteToUDP(data, sender)
	}
}

func (a *App) handleDiscoverServers(w http.ResponseWriter, r *http.Request) {
	nonce, _ := randomID()
	request, _ := json.Marshal(discoveryPacket{Type: "discover", Nonce: nonce})
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"servers": []DiscoveredServer{}})
		return
	}
	defer connection.Close()
	_ = connection.SetWriteBuffer(64 << 10)
	targets := broadcastAddresses()
	for _, target := range targets {
		_, _ = connection.WriteToUDP(request, target)
	}
	_ = connection.SetReadDeadline(time.Now().Add(1400 * time.Millisecond))
	current, _ := a.store.ServerInfo(r.Context())
	seen := make(map[string]bool)
	servers := make([]DiscoveredServer, 0)
	buffer := make([]byte, 2048)
	for {
		count, sender, err := connection.ReadFromUDP(buffer)
		if err != nil {
			break
		}
		var packet discoveryPacket
		if json.Unmarshal(buffer[:count], &packet) != nil || packet.Type != "offer" || packet.Nonce != nonce || packet.InstanceID == "" || packet.InstanceID == current.InstanceID || seen[packet.InstanceID] {
			continue
		}
		seen[packet.InstanceID] = true
		host := sender.IP.String()
		if sender.IP.To4() == nil {
			host = "[" + host + "]"
		}
		servers = append(servers, DiscoveredServer{InstanceID: packet.InstanceID, Name: packet.Name, State: packet.State, URL: "http://" + host + ":" + strconv.Itoa(packet.Port), Receiver: packet.Receiver})
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func listenPort(address string) int {
	_, portValue, err := net.SplitHostPort(address)
	if err != nil {
		if strings.HasPrefix(address, ":") {
			portValue = strings.TrimPrefix(address, ":")
		}
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port <= 0 {
		return 8080
	}
	return port
}

func broadcastAddresses() []*net.UDPAddr {
	unique := map[string]bool{}
	addresses := make([]*net.UDPAddr, 0)
	add := func(ip net.IP) {
		key := ip.String()
		if unique[key] {
			return
		}
		unique[key] = true
		addresses = append(addresses, &net.UDPAddr{IP: ip, Port: discoveryPort})
	}
	add(net.IPv4bcast)
	interfaces, _ := net.Interfaces()
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		interfaceAddresses, _ := networkInterface.Addrs()
		for _, address := range interfaceAddresses {
			network, ok := address.(*net.IPNet)
			if !ok {
				continue
			}
			ip := network.IP.To4()
			if ip == nil || len(network.Mask) != 4 {
				continue
			}
			broadcast := make(net.IP, 4)
			for index := 0; index < 4; index++ {
				broadcast[index] = ip[index] | ^network.Mask[index]
			}
			add(broadcast)
		}
	}
	return addresses
}

func enableUDPBroadcast(connection *net.UDPConn) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fileDescriptor uintptr) {
		_ = unix.SetsockoptInt(int(fileDescriptor), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
	})
}
