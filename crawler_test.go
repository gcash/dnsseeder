package main

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gcash/bchd/wire"
)

// fakeNode runs a mainnet node on a loopback port for the crawler to connect
// to, and returns its address.  It completes the handshake on each
// connection, runs serve, if not nil, with the crawler's version message, and
// then waits for the crawler to disconnect.
func fakeNode(t *testing.T, serve func(conn net.Conn, version *wire.MsgVersion)) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				version, err := fakeHandshake(conn)
				if err != nil {
					return
				}
				if serve != nil {
					serve(conn, version)
				}
				for {
					_, _, err := wire.ReadMessage(conn, wire.ProtocolVersion, wire.MainNet)
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return l.Addr().String()
}

// fakeHandshake answers the version message the crawler sends on conn with a
// version and a verack, reads until the crawler's verack, and returns the
// crawler's version.  It speaks the wire protocol directly, as a bchd peer in
// the same process refuses the crawler's peer as a connection to itself.
func fakeHandshake(conn net.Conn) (*wire.MsgVersion, error) {
	msg, _, err := wire.ReadMessage(conn, wire.ProtocolVersion, wire.MainNet)
	if err != nil {
		return nil, err
	}
	version, ok := msg.(*wire.MsgVersion)
	if !ok {
		return nil, fmt.Errorf("got %s instead of version", msg.Command())
	}

	addr := wire.NewNetAddressIPPort(net.IPv4(127, 0, 0, 1), 8333, wire.SFNodeNetwork)
	ours := wire.NewMsgVersion(addr, addr, 0x5eed, 900000)
	ours.Services = wire.SFNodeNetwork
	for _, m := range []wire.Message{ours, wire.NewMsgVerAck()} {
		if err := wire.WriteMessage(conn, m, wire.ProtocolVersion, wire.MainNet); err != nil {
			return nil, err
		}
	}

	for {
		msg, _, err := wire.ReadMessage(conn, wire.ProtocolVersion, wire.MainNet)
		if err != nil {
			return nil, err
		}
		if _, ok := msg.(*wire.MsgVerAck); ok {
			return version, nil
		}
	}
}

// TestCrawlFullSeeder ensures a crawl while the seeder's list is full releases
// the list's lock.  It kept the read lock, so the seeder's next write to the
// list waited forever.
func TestCrawlFullSeeder(t *testing.T) {
	addr := fakeNode(t, nil)
	s := &dnsseeder{name: "Test", theList: map[string]*node{"other": {}}}

	nas, cerr := crawlIP(s, &result{node: addr})
	if nas != nil || cerr != nil {
		t.Fatalf("crawl of a full seeder returned %v, %v", nas, cerr)
	}

	if !s.mtx.TryLock() {
		t.Fatal("the crawl left the list locked")
	}
	s.mtx.Unlock()
}

// goroutinesIn returns the number of goroutines with fn on their stack.
func goroutinesIn(fn string) int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, fn) {
			n++
		}
	}
	return n
}

// testAddrMsg returns an addr message with the addresses of ips.
func testAddrMsg(ips ...net.IP) *wire.MsgAddr {
	msg := wire.NewMsgAddr()
	for _, ip := range ips {
		_ = msg.AddAddress(wire.NewNetAddressIPPort(ip, 8333, wire.SFNodeNetwork))
	}
	return msg
}

// TestCrawlExtraAddr ensures an addr message that arrives after the crawl
// took the one it waits for doesn't block the crawled node's goroutine in the
// crawl's listener, where it then stayed for good.
func TestCrawlExtraAddr(t *testing.T) {
	addr := fakeNode(t, func(conn net.Conn, _ *wire.MsgVersion) {
		msg := testAddrMsg(net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8))
		for i := 0; i < 2; i++ {
			if err := wire.WriteMessage(conn, msg, wire.ProtocolVersion, wire.MainNet); err != nil {
				return
			}
		}
	})
	s := &dnsseeder{name: "Test", theList: map[string]*node{}, maxSize: 100}

	// One crawl in about 13 left a goroutine in the listener.
	for i := 0; i < 100; i++ {
		if _, cerr := crawlIP(s, &result{node: addr}); cerr != nil {
			t.Fatal(cerr)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		n := goroutinesIn("dnsseeder.crawlIP.func")
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines blocked in the crawl's listeners "+
				"after 100 crawls", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCrawlCollectsAddr ensures a crawl collects the addresses of a node's
// addr messages until one has more than one address, as BCHN sends its own
// address first and its answer to getaddr later.  The crawl kept only the
// first message, the node's own address.
func TestCrawlCollectsAddr(t *testing.T) {
	addr := fakeNode(t, func(conn net.Conn, _ *wire.MsgVersion) {
		msgs := []*wire.MsgAddr{
			testAddrMsg(net.IPv4(127, 0, 0, 1)),
			testAddrMsg(net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8),
				net.IPv4(9, 10, 11, 12)),
		}
		for _, msg := range msgs {
			if err := wire.WriteMessage(conn, msg, wire.ProtocolVersion, wire.MainNet); err != nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
	s := &dnsseeder{name: "Test", theList: map[string]*node{}, maxSize: 100}

	start := time.Now()
	nas, cerr := crawlIP(s, &result{node: addr})
	if cerr != nil {
		t.Fatal(cerr)
	}
	if len(nas) != 4 {
		t.Errorf("crawl returned %d addresses, want 4", len(nas))
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("crawl took %v", elapsed)
	}
}

// TestCrawlNoRelay ensures a crawl asks the node not to announce transactions
// to it, which it didn't, so nodes announced their transactions to crawls
// that only ask for addresses.
func TestCrawlNoRelay(t *testing.T) {
	versions := make(chan *wire.MsgVersion, 1)
	addr := fakeNode(t, func(conn net.Conn, version *wire.MsgVersion) {
		versions <- version
		msg := testAddrMsg(net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8))
		_ = wire.WriteMessage(conn, msg, wire.ProtocolVersion, wire.MainNet)
	})
	s := &dnsseeder{name: "Test", theList: map[string]*node{}, maxSize: 100}

	if _, cerr := crawlIP(s, &result{node: addr}); cerr != nil {
		t.Fatal(cerr)
	}
	if version := <-versions; !version.DisableRelayTx {
		t.Error("the crawl asked the node to announce transactions")
	}
}
