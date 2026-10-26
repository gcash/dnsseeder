package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogOutputs ensures the seeder writes its errors through errLog, to
// standard error, and its other lines through the log package, which main
// sends to standard output.  Everything went to standard error, which GKE
// ranks as errors.
func TestLogOutputs(t *testing.T) {
	var info, errs bytes.Buffer
	log.SetOutput(&info)
	errLog.SetOutput(&errs)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		errLog.SetOutput(os.Stderr)
	})

	config.dataDir = t.TempDir()
	config.seeders = make(map[string]*dnsseeder)
	s := &dnsseeder{name: "Log Test", theList: map[string]*node{}}
	s.dumpCache()
	if _, err := initNetwork(JNetwork{Name: "Log Test", ID: "0xe8f3e1e3", Port: 8333,
		DNSName: "log-test.example.org", NameServer: "ns.example.org"}); err != nil {
		t.Fatal(err)
	}

	// A cache that can't be written.
	config.dataDir = filepath.Join(t.TempDir(), "missing")
	s.dumpCache()

	if !strings.Contains(info.String(), "Loaded 0 nodes from Log Test cache") ||
		strings.Contains(info.String(), "error") {

		t.Errorf("standard output got %q", info.String())
	}
	if !strings.Contains(errs.String(), "error writing cache") ||
		strings.Contains(errs.String(), "Loaded") {

		t.Errorf("standard error got %q", errs.String())
	}
}
