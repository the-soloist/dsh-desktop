package backend

import (
	"net"
	"testing"
)

func TestNextAvailablePortSkipsOccupiedPorts(t *testing.T) {
	got, err := NextAvailablePort(3080, func(port int) bool {
		return port == 3082
	})
	if err != nil || got != 3082 {
		t.Fatalf("nextAvailablePort() = %d, %v", got, err)
	}
}

func TestPortAvailableRejectsNonHTTPListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if PortAvailable(port) {
		t.Fatal("non-HTTP listener was treated as an available port")
	}
}

func TestNextAvailablePortBounds(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		if _, err := NextAvailablePort(port, func(int) bool { t.Fatal("invalid port checked"); return true }); err == nil {
			t.Fatalf("accepted invalid port %d", port)
		}
	}
	checks := 0
	_, err := NextAvailablePort(65535, func(int) bool { checks++; return false })
	if err == nil || checks != 1 {
		t.Fatalf("port search overflowed: checks=%d, err=%v", checks, err)
	}
}
