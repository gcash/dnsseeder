package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gcash/bchd/wire"
	"github.com/miekg/dns"
)

// TestRestartFromCache ensures a seeder restarted from its cache serves the
// cached good nodes right away, instead of none until its first DNS update a
// minute later, loads the nodes the cache caught mid-crawl as not being
// crawled, as they were never crawled again, and saves its cache when it
// stops.
func TestRestartFromCache(t *testing.T) {
	config.dataDir = t.TempDir()
	config.dns = make(map[string][]dns.RR)
	jnw := JNetwork{
		Name: "Restart Test",
		ID:   "0xe3e1f3e8",
		// Nothing listens on port 1, so crawls of the node fail at once.
		Port:       1,
		DNSName:    "restart-test.example.org",
		NameServer: "ns.example.org",
	}

	config.seeders = make(map[string]*dnsseeder)
	s, err := initNetwork(jnw)
	if err != nil {
		t.Fatal(err)
	}
	if !s.addNa(wire.NewNetAddressIPPort(net.IPv4(127, 0, 0, 1), 1, 0)) {
		t.Fatal("node not added")
	}
	for _, nd := range s.theList {
		nd.Status = statusCG
		nd.CrawlActive = true
	}
	s.dumpCache()

	config.seeders = make(map[string]*dnsseeder)
	s, err = initNetwork(jnw)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.theList) != 1 {
		t.Fatalf("%d nodes loaded", len(s.theList))
	}
	for k, nd := range s.theList {
		if nd.Status != statusCG || nd.CrawlActive {
			t.Errorf("%s loaded with status %d, crawl active %v", k,
				nd.Status, nd.CrawlActive)
		}
	}
	cache := filepath.Join(config.dataDir, "Restart Test.cache.json")
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go s.runSeeder(done, &wg)

	deadline := time.Now().Add(5 * time.Second)
	for {
		config.dnsmtx.RLock()
		n := len(config.dns["restart-test.example.org.A"])
		config.dnsmtx.RUnlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the cached node is not served")
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(done)
	wg.Wait()
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("cache not saved on stop: %v", err)
	}
	files, err := os.ReadDir(config.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		var names []string
		for _, f := range files {
			names = append(names, f.Name())
		}
		t.Errorf("data directory holds %q", names)
	}
}

// TestCrawlInitialIPsFirst ensures a seeder crawls the nodes its config lists
// in InitialIPs as soon as it starts.  They only joined the new nodes, which
// it crawls 20 at a time in random order, so behind a long list they waited.
func TestCrawlInitialIPsFirst(t *testing.T) {
	crawled := make(chan struct{}, 1)
	addr := fakeNode(t, func(conn net.Conn, _ *wire.MsgVersion) {
		select {
		case crawled <- struct{}{}:
		default:
		}
	})
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	config.dataDir = t.TempDir()
	config.seeders = make(map[string]*dnsseeder)
	config.dns = make(map[string][]dns.RR)
	s, err := initNetwork(JNetwork{
		Name:       "Initial Test",
		ID:         "0xe8f3e1e3",
		Port:       uint16(port),
		DNSName:    "initial-test.example.org",
		NameServer: "ns.example.org",
		InitialIPs: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 500 other new nodes on loopback addresses, which refuse connections.
	for i := 0; i < 500; i++ {
		ip := net.IPv4(127, 0, byte(1+i/250), byte(1+i%250))
		if !s.addNa(wire.NewNetAddressIPPort(ip, uint16(port), 0)) {
			t.Fatalf("node %s not added", ip)
		}
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go s.runSeeder(done, &wg)
	t.Cleanup(func() {
		close(done)
		wg.Wait()
	})

	select {
	case <-crawled:
	case <-time.After(3 * time.Second):
		t.Fatal("the initial node wasn't crawled at the start")
	}
}
