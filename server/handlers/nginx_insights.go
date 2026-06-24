package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file adds read-only "insight" views on top of the live nginx config:
//   - the effective merged config (`nginx -T`)
//   - an SSL certificate dashboard (expiry per domain)
//   - a server/upstream overview parsed from the merged config
//   - live stub_status metrics
// They all build on a small brace-aware parser of the `nginx -T` dump.

// nginxDumpConfig returns the fully merged configuration via `<bin> -T`.
func nginxDumpConfig(h *DockerHost, bin string, sudo bool) (string, error) {
	out, err := runHostPrivileged(h, sudo, []string{bin, "-T"}, "")
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return out, nil
}

// ── Effective config (`nginx -T`) ─────────────────────────────────────────

// NginxEffectiveConfig returns the merged config across all includes.
func NginxEffectiveConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		out, err := nginxDumpConfig(h, nginxBin(r), nginxUseSudo(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"content": out})
	}
}

// ── Mini config AST ───────────────────────────────────────────────────────

type nginxNode struct {
	Name     string
	Args     []string
	Children []*nginxNode
}

// tokenizeNginxConf splits a config into words, '{', '}' and ';' tokens,
// skipping '#' comments and respecting quotes.
func tokenizeNginxConf(text string) []string {
	var toks []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			toks = append(toks, buf.String())
			buf.Reset()
		}
	}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '#':
			flush()
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
		case c == '"' || c == '\'':
			flush()
			quote := c
			i++
			for i < len(rs) && rs[i] != quote {
				buf.WriteRune(rs[i])
				i++
			}
			toks = append(toks, buf.String())
			buf.Reset()
		case c == '{' || c == '}' || c == ';':
			flush()
			toks = append(toks, string(c))
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		default:
			buf.WriteRune(c)
		}
	}
	flush()
	return toks
}

func parseNginxConf(text string) []*nginxNode {
	toks := tokenizeNginxConf(text)
	pos := 0
	return parseNginxBlock(toks, &pos)
}

func parseNginxBlock(toks []string, pos *int) []*nginxNode {
	var nodes []*nginxNode
	var words []string
	for *pos < len(toks) {
		t := toks[*pos]
		switch t {
		case "}":
			*pos++
			return nodes
		case ";":
			*pos++
			if len(words) > 0 {
				nodes = append(nodes, &nginxNode{Name: words[0], Args: words[1:]})
				words = nil
			}
		case "{":
			*pos++
			child := parseNginxBlock(toks, pos)
			node := &nginxNode{Children: child}
			if len(words) > 0 {
				node.Name, node.Args = words[0], words[1:]
			}
			nodes = append(nodes, node)
			words = nil
		default:
			words = append(words, t)
			*pos++
		}
	}
	return nodes
}

// walkNginx visits every node depth-first.
func walkNginx(nodes []*nginxNode, fn func(n *nginxNode)) {
	for _, n := range nodes {
		fn(n)
		walkNginx(n.Children, fn)
	}
}

// ── SSL certificate dashboard ─────────────────────────────────────────────

type nginxCert struct {
	Path     string   `json:"path"`
	Subject  string   `json:"subject"`
	NotAfter string   `json:"not_after"`
	DaysLeft int      `json:"days_left"`
	Expired  bool     `json:"expired"`
	Domains  []string `json:"domains"`
	Error    string   `json:"error,omitempty"`
}

// NginxCerts lists every ssl_certificate in the merged config with its expiry
// (via openssl) and the domains that use it.
func NginxCerts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		sudo := nginxUseSudo(r)
		dump, err := nginxDumpConfig(h, nginxBin(r), sudo)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		tree := parseNginxConf(dump)

		// Collect cert paths and the server_names that reference each.
		domainsByCert := map[string]map[string]bool{}
		certOrder := []string{}
		var visit func(nodes []*nginxNode)
		visit = func(nodes []*nginxNode) {
			for _, n := range nodes {
				if n.Name == "server" && len(n.Children) > 0 {
					var cert, names string
					for _, c := range n.Children {
						if c.Name == "ssl_certificate" && len(c.Args) > 0 {
							cert = c.Args[0]
						}
						if c.Name == "server_name" {
							names = strings.Join(c.Args, " ")
						}
					}
					if cert != "" {
						if _, ok := domainsByCert[cert]; !ok {
							domainsByCert[cert] = map[string]bool{}
							certOrder = append(certOrder, cert)
						}
						for _, d := range strings.Fields(names) {
							if d != "" && d != "_" {
								domainsByCert[cert][d] = true
							}
						}
					}
				}
				visit(n.Children)
			}
		}
		visit(tree)

		if len(certOrder) == 0 {
			json.NewEncoder(w).Encode(map[string]interface{}{"certs": []nginxCert{}})
			return
		}

		// One round trip: loop over the paths and print openssl output per cert.
		var sb strings.Builder
		sb.WriteString("for f in")
		for _, p := range certOrder {
			sb.WriteString(" '" + strings.ReplaceAll(p, "'", `'\''`) + "'")
		}
		sb.WriteString("; do echo \"@@@$f\"; openssl x509 -in \"$f\" -noout -enddate -subject -nameopt RFC2253 2>&1; done")
		out, _ := runHostPrivileged(h, sudo, []string{"sh", "-c", sb.String()}, "")

		parsed := map[string]*nginxCert{}
		var cur *nginxCert
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimRight(line, "\r")
			switch {
			case strings.HasPrefix(line, "@@@"):
				p := strings.TrimPrefix(line, "@@@")
				cur = &nginxCert{Path: p}
				parsed[p] = cur
			case cur == nil:
				continue
			case strings.HasPrefix(line, "notAfter="):
				cur.NotAfter = strings.TrimPrefix(line, "notAfter=")
				if t, e := time.Parse("Jan _2 15:04:05 2006 MST", cur.NotAfter); e == nil {
					cur.DaysLeft = int(time.Until(t).Hours() / 24)
					cur.Expired = time.Now().After(t)
				}
			case strings.HasPrefix(line, "subject="):
				cur.Subject = strings.TrimPrefix(line, "subject=")
			case strings.Contains(strings.ToLower(line), "unable to load") ||
				strings.Contains(line, "No such file") ||
				strings.Contains(strings.ToLower(line), "permission denied"):
				if cur.Error == "" {
					cur.Error = strings.TrimSpace(line)
				}
			}
		}

		certs := make([]nginxCert, 0, len(certOrder))
		for _, p := range certOrder {
			c := parsed[p]
			if c == nil {
				c = &nginxCert{Path: p, Error: "no openssl output"}
			}
			c.Domains = []string{}
			for d := range domainsByCert[p] {
				c.Domains = append(c.Domains, d)
			}
			sort.Strings(c.Domains)
			certs = append(certs, *c)
		}
		// Soonest-to-expire first; errored certs last.
		sort.SliceStable(certs, func(i, j int) bool {
			if (certs[i].Error == "") != (certs[j].Error == "") {
				return certs[i].Error == ""
			}
			return certs[i].DaysLeft < certs[j].DaysLeft
		})
		json.NewEncoder(w).Encode(map[string]interface{}{"certs": certs})
	}
}

// ── Server / upstream overview ────────────────────────────────────────────

type nginxServerInfo struct {
	Listen    []string `json:"listen"`
	Names     string   `json:"names"`
	Root      string   `json:"root"`
	Cert      string   `json:"cert"`
	ProxyPass []string `json:"proxy_pass"`
}

type nginxUpstreamInfo struct {
	Name    string   `json:"name"`
	Servers []string `json:"servers"`
}

// collectDirective gathers the first arg of every directive named `name`
// anywhere in the subtree.
func collectDirective(n *nginxNode, name string) []string {
	var out []string
	walkNginx(n.Children, func(c *nginxNode) {
		if c.Name == name && len(c.Args) > 0 {
			out = append(out, c.Args[0])
		}
	})
	return out
}

// NginxMap parses the merged config into a server + upstream overview.
func NginxMap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		dump, err := nginxDumpConfig(h, nginxBin(r), nginxUseSudo(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		tree := parseNginxConf(dump)

		servers := []nginxServerInfo{}
		upstreams := []nginxUpstreamInfo{}
		walkNginx(tree, func(n *nginxNode) {
			switch {
			case n.Name == "upstream" && len(n.Args) > 0:
				up := nginxUpstreamInfo{Name: n.Args[0], Servers: []string{}}
				for _, c := range n.Children {
					if c.Name == "server" && len(c.Args) > 0 {
						up.Servers = append(up.Servers, c.Args[0])
					}
				}
				upstreams = append(upstreams, up)
			case n.Name == "server" && len(n.Children) > 0:
				s := nginxServerInfo{Listen: []string{}, ProxyPass: []string{}}
				for _, c := range n.Children {
					switch c.Name {
					case "listen":
						s.Listen = append(s.Listen, strings.Join(c.Args, " "))
					case "server_name":
						s.Names = strings.Join(c.Args, " ")
					case "root":
						if len(c.Args) > 0 {
							s.Root = c.Args[0]
						}
					case "ssl_certificate":
						if len(c.Args) > 0 {
							s.Cert = c.Args[0]
						}
					}
				}
				if pp := collectDirective(n, "proxy_pass"); pp != nil {
					s.ProxyPass = pp
				}
				servers = append(servers, s)
			}
		})
		json.NewEncoder(w).Encode(map[string]interface{}{"servers": servers, "upstreams": upstreams})
	}
}

// ── stub_status metrics ───────────────────────────────────────────────────

type nginxStub struct {
	Active   int `json:"active"`
	Accepts  int `json:"accepts"`
	Handled  int `json:"handled"`
	Requests int `json:"requests"`
	Reading  int `json:"reading"`
	Writing  int `json:"writing"`
	Waiting  int `json:"waiting"`
}

// NginxStubStatus curls the host's stub_status page (default
// http://127.0.0.1/nginx_status) and parses the counters.
func NginxStubStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		url := strings.TrimSpace(r.URL.Query().Get("url"))
		if url == "" {
			url = "http://127.0.0.1/nginx_status"
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			http.Error(w, jsonError("url must be http(s)"), http.StatusBadRequest)
			return
		}
		out, err := runHostCommand(h, []string{"curl", "-s", "--max-time", "5", "-k", url}, "")
		if err != nil {
			http.Error(w, jsonError(strings.TrimSpace(out)+": "+err.Error()), http.StatusBadGateway)
			return
		}
		stub, perr := parseStubStatus(out)
		if perr != nil {
			http.Error(w, jsonError("unexpected response — is stub_status enabled at this URL?"), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(stub)
	}
}

// parseStubStatus reads the fixed 4-line stub_status format.
func parseStubStatus(s string) (*nginxStub, error) {
	fields := strings.Fields(s)
	// Expected tokens include: Active connections: N ... <accepts> <handled> <requests> Reading: N Writing: N Waiting: N
	idx := func(label string) int {
		for i, f := range fields {
			if f == label {
				return i
			}
		}
		return -1
	}
	atoi := func(i int) int {
		if i < 0 || i >= len(fields) {
			return 0
		}
		n, _ := strconv.Atoi(fields[i])
		return n
	}
	ai := idx("connections:")
	ri := idx("Reading:")
	wi := idx("Writing:")
	gi := idx("Waiting:")
	if ai < 0 || ri < 0 {
		return nil, fmt.Errorf("not stub_status")
	}
	st := &nginxStub{
		Active:  atoi(ai + 1),
		Reading: atoi(ri + 1),
		Writing: atoi(wi + 1),
		Waiting: atoi(gi + 1),
	}
	// The three bare counters sit on the line after "server accepts handled requests".
	if si := idx("requests"); si >= 0 && si+3 < len(fields) {
		st.Accepts = atoi(si + 1)
		st.Handled = atoi(si + 2)
		st.Requests = atoi(si + 3)
	}
	return st, nil
}
