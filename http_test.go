package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestStatusPagesEscape ensures the status pages escape what a node reports,
// such as its user agent, which they wrote out as HTML, so a node could put a
// script on them.
func TestStatusPagesEscape(t *testing.T) {
	s := testSeeder(t, "escape-test.example.org", net.IPv4(10, 0, 0, 1))
	key := net.JoinHostPort("10.0.0.1", "8333")
	s.theList[key].Version = 70016
	s.theList[key].StrVersion = "/evil:1.0<script>alert(1)</script>/"

	pages := []struct {
		handler http.HandlerFunc
		url     string
	}{
		{statusCGHandler, "/statusCG?s=escape-test.example.org"},
		{nodeHandler, "/node?s=escape-test.example.org&nd=" + url.QueryEscape(key)},
	}
	for _, page := range pages {
		w := httptest.NewRecorder()
		page.handler(w, httptest.NewRequest("GET", page.url, nil))
		body := w.Body.String()
		if strings.Contains(body, "<script>") ||
			!strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {

			t.Errorf("%s: user agent not escaped:\n%s", page.url, body)
		}
	}

	// The node page names the node, which it left out.
	w := httptest.NewRecorder()
	nodeHandler(w, httptest.NewRequest("GET", pages[1].url, nil))
	if !strings.Contains(w.Body.String(), "<th>Node 10.0.0.1:8333</th>") {
		t.Errorf("node page doesn't name the node:\n%s", w.Body.String())
	}

	// The node summaries keep their markup.
	w = httptest.NewRecorder()
	statusCGHandler(w, httptest.NewRequest("GET", pages[0].url, nil))
	if !strings.Contains(w.Body.String(), "<b>Remote Version:</b> 70016/evil:1.0&lt;script&gt;") {
		t.Errorf("summary markup lost:\n%s", w.Body.String())
	}
}
