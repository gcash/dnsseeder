package main

import (
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// unansweredAddr returns the address of a loopback listener whose accept
// queue is full, so Linux drops the packets of new connections to it, as a
// node that drops them does.
func unansweredAddr(t *testing.T) string {
	t.Helper()

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	// The queue of a listener with a backlog of 0 holds one connection.
	if err := syscall.Listen(fd, 0); err != nil {
		t.Fatal(err)
	}
	sa, err := syscall.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(sa.(*syscall.SockaddrInet4).Port))

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return addr
}

// TestCrawlConnectTimeout ensures a crawl gives up on a node that doesn't
// answer its connection after 5 seconds.  It waited until Linux gave up,
// after about two minutes.
func TestCrawlConnectTimeout(t *testing.T) {
	addr := unansweredAddr(t)
	s := &dnsseeder{name: "Test", theList: map[string]*node{}}

	start := time.Now()
	_, cerr := crawlIP(s, &result{node: addr})
	if cerr == nil {
		t.Fatal("crawl of a node that doesn't answer succeeded")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("crawl gave up after %v", elapsed)
	}
	t.Logf("crawl gave up after %v: %v", time.Since(start).Round(time.Millisecond), cerr)
}
