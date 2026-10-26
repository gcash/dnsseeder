package main

import (
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/miekg/dns"
)

// startHTTP runs in a goroutine and provides the web interface
// to the dnsseeder
func startHTTP(port string) {

	http.HandleFunc("/dns", dnsWebHandler)
	http.HandleFunc("/node", nodeHandler)
	http.HandleFunc("/statusRG", statusRGHandler)
	http.HandleFunc("/statusCG", statusCGHandler)
	http.HandleFunc("/statusWG", statusWGHandler)
	http.HandleFunc("/statusNG", statusNGHandler)
	http.HandleFunc("/summary", summaryHandler)
	http.HandleFunc("/", emptyHandler)
	// listen only on localhost
	err := http.ListenAndServe("127.0.0.1:"+port, nil)
	if err != nil {
		errLog.Fatal("ListenAndServe: ", err)
	}

}

// dnsWebHandler processes all requests and returns output in the requested format
func dnsWebHandler(w http.ResponseWriter, r *http.Request) {

	st := time.Now()

	// skip the s= from the raw query
	n := r.FormValue("s")
	s := getSeederByName(n)
	if s == nil {
		writeHeader(w, r)
		_, _ = fmt.Fprintf(w, "No seeder found: %s", html.EscapeString(n))
		writeFooter(w, r, st)
		return
	}

	t1 := `
	<center>
	<table border=1>
	  <tr>
	  <th>Standard Ports</th>
	  <th>Non Standard Ports</th>
	  </tr>
	  <tr>
	  <td>
	  `
	t2 := `  {{range .}}
	       {{.}}<br>
	     {{end}}
		 `
	t3 := `
	  </td>
	  <td>
	  `
	t4 := `
	  </td>
	  </tr>
	</table>
	</center>
	`

	t, err := template.New("records template").Parse(t2)
	if err != nil {
		errLog.Printf("error parsing records template %v\n", err)
	}

	writeHeader(w, r)
	_, _ = fmt.Fprintf(w, "<b>Currently serving the following DNS records</b>")

	for _, name := range s.names {
		config.dnsmtx.RLock()
		// if the dns map does not have a key for the request it will return an empty slice
		v4std := recordStrings(config.dns[name.host+".A"])
		v4non := recordStrings(config.dns["nonstd."+name.host+".A"])
		v6std := recordStrings(config.dns[name.host+".AAAA"])
		v6non := recordStrings(config.dns["nonstd."+name.host+".AAAA"])
		config.dnsmtx.RUnlock()

		_, _ = fmt.Fprintf(w, "<p><center><b>%s</b></center></p>", html.EscapeString(name.host))
		tables := []struct {
			title    string
			std, non []string
		}{
			{"IPv4", v4std, v4non},
			{"IPv6", v6std, v6non},
		}
		for _, table := range tables {
			_, _ = fmt.Fprintf(w, "<p><center><b>%s</b></center></p>", table.title)
			_, _ = fmt.Fprint(w, t1)
			if err := t.Execute(w, table.std); err != nil {
				errLog.Printf("error executing records template %v\n", err)
			}
			_, _ = fmt.Fprint(w, t3)
			if err := t.Execute(w, table.non); err != nil {
				errLog.Printf("error executing records template %v\n", err)
			}
			_, _ = fmt.Fprint(w, t4)
		}
	}
	writeFooter(w, r, st)
}

// recordStrings returns the text of the records rrs for the dns page
func recordStrings(rrs []dns.RR) []string {
	if len(rrs) == 0 {
		return []string{"No records Available"}
	}
	strs := make([]string, len(rrs))
	for i, rr := range rrs {
		strs[i] = rr.String()
	}
	return strs
}

// emptyHandler processes all requests for non-existent urls
func emptyHandler(w http.ResponseWriter, _ *http.Request) {

	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprintf(w, "Nothing to see here. Move along please\n")
}

func statusRGHandler(w http.ResponseWriter, r *http.Request) {
	statusHandler(w, r, statusRG)
}
func statusCGHandler(w http.ResponseWriter, r *http.Request) {
	statusHandler(w, r, statusCG)
}
func statusWGHandler(w http.ResponseWriter, r *http.Request) {
	statusHandler(w, r, statusWG)
}
func statusNGHandler(w http.ResponseWriter, r *http.Request) {
	statusHandler(w, r, statusNG)
}

type webstatus struct {
	Key    string
	Fields []webfield
	Seeder string
}

// webfield is one labelled value of a node's summary.  The template adds the
// markup, so the value, which can hold what a node reported, is escaped.
type webfield struct {
	Label string
	Value string
}

func statusHandler(w http.ResponseWriter, r *http.Request, status uint32) {

	startT := time.Now()

	// read the seeder name
	n := r.FormValue("s")
	s := getSeederByName(n)
	if s == nil {
		writeHeader(w, r)
		_, _ = fmt.Fprintf(w, "No seeder found called %s", html.EscapeString(n))
		writeFooter(w, r, startT)
		return
	}

	// gather all the info before writing anything to the remote browser
	ws := generateWebStatus(s, status)

	st := `
	<center>
	<table border=1>
	  <tr>
	  <th>Node</th>
	  <th>Summary</th>
	  </tr>
	     {{range .}}
	  <tr>
	  <td>
	       <a href="/node?s={{.Seeder}}&nd={{.Key}}">{{.Key}}</a>
	  </td>
	  <td>
	       {{range .Fields}}<b>{{.Label}}:</b> {{.Value}} {{end}}
	  </td>
	  </tr>
	     {{end}}
	</table>
	</center>
	`

	writeHeader(w, r)

	if len(ws) == 0 {
		_, _ = fmt.Fprintf(w, "No Nodes found with this status")
	} else {

		switch status {
		case statusRG:
			_, _ = fmt.Fprintf(w, "<center><b>Node Status: statusRG - (Reported Good) Have not been able to get addresses yet</b></center>")
		case statusCG:
			_, _ = fmt.Fprintf(w, "<center><b>Node Status: statusCG - (Currently Good) Able to connect and get addresses</b></center>")
		case statusWG:
			_, _ = fmt.Fprintf(w, "<center><b>Node Status: statusWG - (Was Good) Was Ok but now can not get addresses</b></center>")
		case statusNG:
			_, _ = fmt.Fprintf(w, "<center><b>Node Status: statusNG - (No Good) Unable to get addresses</b></center>")
		}
		t := template.New("Status template")
		t, err := t.Parse(st)
		if err != nil {
			errLog.Printf("error parsing status template %v\n", err)
		}
		err = t.Execute(w, ws)
		if err != nil {
			errLog.Printf("error executing status template %v\n", err)
		}

	}
	writeFooter(w, r, startT)
}

// generateWebStatus is given a node status and returns a slice of webstatus structures
// ready to be ranged over by an html/template
func generateWebStatus(s *dnsseeder, status uint32) (ws []webstatus) {

	s.mtx.RLock()
	defer s.mtx.RUnlock()

	var fields []webfield

	for k, v := range s.theList {
		if v.Status != status {
			continue
		}

		switch status {
		case statusRG:
			fields = []webfield{
				{"Fail Count", fmt.Sprint(v.ConnectFails)},
				{"DNS Type", v.dns2str()},
			}
		case statusCG:
			fields = []webfield{
				{"Remote Version", fmt.Sprintf("%v%s", v.Version, v.StrVersion)},
				{"Last Block", fmt.Sprint(v.LastBlock)},
				{"DNS Type", v.dns2str()},
			}

		case statusWG:
			fields = []webfield{
				{"Last Try", time.Since(v.LastTry).String() + " ago"},
				{"Last Status", v.StatusStr},
			}

		case statusNG:
			fields = []webfield{
				{"Fail Count", fmt.Sprint(v.ConnectFails)},
				{"Last Try", time.Since(v.LastTry).String() + " ago"},
				{"Last Status", v.StatusStr},
			}

		default:
			fields = nil
		}

		ows := webstatus{
			Key:    k,
			Fields: fields,
			Seeder: s.name,
		}
		ws = append(ws, ows)
	}

	return ws
}

// copy Node details into a template friendly struct
type webtemplate struct {
	Key            string
	IP             string
	Port           uint16
	Statusstr      string
	Rating         string
	DNStype        string
	Lastconnect    string
	Lastconnectago string
	Lasttry        string
	Lasttryago     string
	Crawlstart     string
	Crawlstartago  string
	Crawlactive    bool
	Connectfails   uint32
	Version        int32
	Strversion     string
	Services       string
	Lastblock      int32
	Nonstdip       string
}

// nodeHandler displays details about one node
func nodeHandler(w http.ResponseWriter, r *http.Request) {

	st := time.Now()

	ndt := `
    <center>
    <table border=1>
      <tr>
      <th>Node {{.Key}}</th><th>Details</th>
      </tr>
      <tr><td>IP Address</td><td>{{.IP}}</td></tr>
      <tr><td>Port</td><td>{{.Port}}</td></tr>
      <tr><td>DNS Type</td><td>{{.DNStype}}</td></tr>
      <tr><td>Non Standard IP</td><td>{{.Nonstdip}}</td></tr>
      <tr><td>Last Connect</td><td>{{.Lastconnect}}<br>{{.Lastconnectago}} ago</td></tr>
      <tr><td>Last Connect Status</td><td>{{.Statusstr}}</td></tr>
      <tr><td>Last Try</td><td>{{.Lasttry}}<br>{{.Lasttryago}} ago</td></tr>
      <tr><td>Crawl Start</td><td>{{.Crawlstart}}<br>{{.Crawlstartago}} ago</td></tr>
      <tr><td>Crawl Active</td><td>{{.Crawlactive}}</td></tr>
      <tr><td>Connection Fails</td><td>{{.Connectfails}}</td></tr>
      <tr><td>Remote Version</td><td>{{.Version}}</td></tr>
      <tr><td>Remote SubVersion</td><td>{{.Strversion}}</td></tr>
      <tr><td>Remote Services</td><td>{{.Services}}</td></tr>
      <tr><td>Remote Last Block</td><td>{{.Lastblock}}</td></tr>
    </table>
    </center>
    `
	// read the seeder name
	n := r.FormValue("s")
	s := getSeederByName(n)
	if s == nil {
		writeHeader(w, r)
		_, _ = fmt.Fprintf(w, "No seeder found called %s", html.EscapeString(n))
		writeFooter(w, r, st)
		return
	}

	s.mtx.RLock()
	defer s.mtx.RUnlock()

	k := r.FormValue("nd")
	writeHeader(w, r)
	if _, ok := s.theList[k]; !ok {
		_, _ = fmt.Fprintf(w, "Sorry there is no Node with those details\n")
	} else {

		nd := s.theList[k]
		wt := webtemplate{
			Key:            k,
			IP:             nd.NA.IP.String(),
			Port:           nd.NA.Port,
			DNStype:        nd.dns2str(),
			Nonstdip:       nd.NonstdIP.String(),
			Statusstr:      nd.StatusStr,
			Lastconnect:    nd.LastConnect.String(),
			Lastconnectago: time.Since(nd.LastConnect).String(),
			Lasttry:        nd.LastTry.String(),
			Lasttryago:     time.Since(nd.LastTry).String(),
			Crawlstart:     nd.CrawlStart.String(),
			Crawlstartago:  time.Since(nd.CrawlStart).String(),
			Connectfails:   nd.ConnectFails,
			Crawlactive:    nd.CrawlActive,
			Version:        nd.Version,
			Strversion:     nd.StrVersion,
			Services:       nd.Services.String(),
			Lastblock:      nd.LastBlock,
		}

		// display details for the Node
		t := template.New("Node template")
		t, err := t.Parse(ndt)
		if err != nil {
			errLog.Printf("error parsing Node template %v\n", err)
		}
		err = t.Execute(w, wt)
		if err != nil {
			errLog.Printf("error executing Node template %v\n", err)
		}

	}
	writeFooter(w, r, st)
}

// summaryHandler displays details about one node
func summaryHandler(w http.ResponseWriter, r *http.Request) {

	st := time.Now()

	var hc struct {
		Name     string
		RG       uint32
		RGS      uint32
		CG       uint32
		CGS      uint32
		WG       uint32
		WGS      uint32
		NG       uint32
		NGS      uint32
		Total    uint32
		V4Std    uint32
		V4Non    uint32
		V6Std    uint32
		V6Non    uint32
		DNSTotal uint32
	}

	writeHeader(w, r)
	// loop through each of the seeder name from a slice so they are always returned in
	// the same order then get a pointer to the seeder struct
	for _, n := range config.order {
		s := config.seeders[n]

		hc.Name = s.name
		// fill the structs so they can be displayed via the template
		s.counts.mtx.RLock()
		hc.RG = s.counts.NdStatus[statusRG]
		hc.RGS = s.counts.NdStarts[statusRG]
		hc.CG = s.counts.NdStatus[statusCG]
		hc.CGS = s.counts.NdStarts[statusCG]
		hc.WG = s.counts.NdStatus[statusWG]
		hc.WGS = s.counts.NdStarts[statusWG]
		hc.NG = s.counts.NdStatus[statusNG]
		hc.NGS = s.counts.NdStarts[statusNG]
		hc.Total = hc.RG + hc.CG + hc.WG + hc.NG

		hc.V4Std = s.counts.DNSCounts[dnsV4Std]
		hc.V4Non = s.counts.DNSCounts[dnsV4Non]
		hc.V6Std = s.counts.DNSCounts[dnsV6Std]
		hc.V6Non = s.counts.DNSCounts[dnsV6Non]
		hc.DNSTotal = hc.V4Std + hc.V4Non + hc.V6Std + hc.V6Non
		s.counts.mtx.RUnlock()

		// we are using basic and simple html here. No fancy graphics or css
		sp := `
    <b>Stats for seeder: {{.Name}}</b>
    <center>
    <table><tr><td>
    Node Stats (count/started)<br>
    <table border=1><tr>
	<td><a href="/statusRG?s={{.Name}}">RG: {{.RG}}/{{.RGS}}</a></td>
    <td><a href="/statusCG?s={{.Name}}">CG: {{.CG}}/{{.CGS}}</a></td>
    <td><a href="/statusWG?s={{.Name}}">WG: {{.WG}}/{{.WGS}}</a></td>
    <td><a href="/statusNG?s={{.Name}}">NG: {{.NG}}/{{.NGS}}</a></td>
    <td>Total: {{.Total}}</td>
    </tr></table>
    </td><td>
    DNS Requests<br>
    <table border=1><tr>
	<td>V4 Std: {{.V4Std}}</td>
    <td>V4 Non: {{.V4Non}}</td>
    <td>V6 Std: {{.V6Std}}</td>
    <td>V6 Non: {{.V6Non}}</td>
    <td><a href="/dns?s={{.Name}}">Total: {{.DNSTotal}}</a></td>
    </tr></table>
    </td></tr></table>
	</center>
	`
		t := template.New("Header template")
		t, err := t.Parse(sp)
		if err != nil {
			errLog.Printf("error parsing summary template %v\n", err)
		}

		err = t.Execute(w, hc)
		if err != nil {
			errLog.Printf("error executing summary template %v\n", err)
		}
	}
	writeFooter(w, r, st)
}

// writeHeader will output the standard header
func writeHeader(w http.ResponseWriter, r *http.Request) {
	// we are using basic and simple html here. No fancy graphics or css
	h1 := `
    <!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">
    <html><head><title>dnsseeder</title></head><body>
	<center>
	<a href="/summary">Summary</a>
`
	_, _ = fmt.Fprint(w, h1)

	// read the seeder name
	n := r.FormValue("s")
	if n != "" {
		s := getSeederByName(n)
		if s != nil {
			_, _ = fmt.Fprintf(w, "<br><b>Seeder: %s</b>", html.EscapeString(s.name))
		}
	}
	_, _ = fmt.Fprintf(w, "</center><hr><br>")
}

// writeFooter will output the standard footer
func writeFooter(w http.ResponseWriter, r *http.Request, st time.Time) {

	// Footer needs to be exported for template processing to work
	var Footer struct {
		Uptime  string
		Version string
		Rt      string
	}

	f := `
	<hr>
	<center>
	<b>Version:</b> {{.Version}}
	<b>Uptime:</b> {{.Uptime}}
	<b>Request Time:</b> {{.Rt}}
	</center>
	</body></html>
	`
	Footer.Uptime = time.Since(config.uptime).String()
	Footer.Version = config.version
	Footer.Rt = time.Since(st).String()

	t := template.New("Footer template")
	t, err := t.Parse(f)
	if err != nil {
		errLog.Printf("error parsing template %v\n", err)
	}
	err = t.Execute(w, Footer)
	if err != nil {
		errLog.Printf("error executing template %v\n", err)
	}

	if config.verbose {
		log.Printf("status - processed web request: %s %s\n",
			r.RemoteAddr,
			r.RequestURI)
	}
}

/*


 */
