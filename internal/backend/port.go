package backend

import (
	"fmt"
	"net"
)

const portSearchLimit = 50

// PortAvailable reports whether 127.0.0.1:port can be bound.
func PortAvailable(port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

// NextAvailablePort checks actual TCP bindings, independently of HTTP readiness.
func NextAvailablePort(start int, available func(int) bool) (int, error) {
	if start < 1 || start > 65535 {
		return 0, fmt.Errorf("invalid port %d", start)
	}
	last := start + portSearchLimit - 1
	if last > 65535 {
		last = 65535
	}
	for port := start; port <= last; port++ {
		if available(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port from %d through %d", start, last)
}
