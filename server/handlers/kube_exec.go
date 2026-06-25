package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/coder/websocket"
)

// WebSocket-based live pod logs (follow) and an interactive exec terminal.
// Auth is enforced inside the handlers (not via middleware) because a browser
// can't set the Authorization header on a WebSocket upgrade — the JWT is passed
// as a ?token= query param, validated the same way the Docker terminal does.

// kubeWSAuthorized validates the WS token and the given app permission.
func kubeWSAuthorized(r *http.Request, perm string) bool {
	if len(jwtSecret) == 0 {
		return true // auth disabled
	}
	userID, err := dockerWSUserID(r)
	if err != nil {
		return false
	}
	return appdb.HasUserAppPermission(userID, perm)
}

// parseClusterPods extracts {id}, {ns}, {pod} from
// /api/kube/clusters/{id}/pods/{ns}/{pod}/(logstream|exec).
func parseClusterPod(r *http.Request) (id int64, ns, pod string, ok bool) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/kube/clusters/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) < 5 || parts[1] != "pods" {
		return 0, "", "", false
	}
	cid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", "", false
	}
	return cid, parts[2], parts[3], true
}

// KubePodLogStream streams a pod's logs (follow) to the browser over a WS.
func KubePodLogStream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !kubeWSAuthorized(r, PermKubeView) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id, ns, pod, ok := parseClusterPod(r)
		if !ok {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		client, err := clientForCluster(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		tail := strings.TrimSpace(r.URL.Query().Get("tail"))
		if tail == "" {
			tail = "200"
		}
		logURL := fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s/log?follow=true&timestamps=true&tailLines=%s",
			client.base, url.PathEscape(ns), url.PathEscape(pod), url.QueryEscape(tail))
		if c := strings.TrimSpace(r.URL.Query().Get("container")); c != "" {
			logURL += "&container=" + url.QueryEscape(c)
		}

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer ws.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Close the upstream GET when the browser disconnects.
		go func() {
			ws.Read(ctx) // browser sends nothing; this returns on close
			cancel()
		}()

		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, logURL, nil)
		if client.token != "" {
			req.Header.Set("Authorization", "Bearer "+client.token)
		}
		// A follow stream is long-lived, so reuse the cluster's TLS transport but
		// without the 25s request timeout (cancellation comes from ctx on close).
		streamClient := &http.Client{Transport: client.http.Transport}
		resp, err := streamClient.Do(req)
		if err != nil {
			ws.Write(ctx, websocket.MessageText, []byte("stream error: "+err.Error()))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			ws.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf("stream error: kubernetes api %d", resp.StatusCode)))
			return
		}

		buf := make([]byte, 16<<10)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if werr := ws.Write(ctx, websocket.MessageText, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
}

// k8s exec channel-framed stream prefixes each binary message with a channel
// byte: 0=stdin, 1=stdout, 2=stderr, 3=error, 4=resize (v4 subprotocol).
const (
	kChStdin  = 0
	kChStdout = 1
	kChStderr = 2
	kChResize = 4
)

// KubePodExec bridges a browser WS terminal to a `kubectl exec` (TTY) session
// inside a pod. Gated by kube.exec (high-risk).
func KubePodExec() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !kubeWSAuthorized(r, PermKubeExec) {
			http.Error(w, "insufficient permissions", http.StatusForbidden)
			return
		}
		id, ns, pod, ok := parseClusterPod(r)
		if !ok {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		client, err := clientForCluster(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		shell := strings.TrimSpace(r.URL.Query().Get("shell"))
		if shell == "" {
			shell = "/bin/sh"
		}
		q := url.Values{}
		q.Set("stdin", "true")
		q.Set("stdout", "true")
		q.Set("stderr", "true")
		q.Set("tty", "true")
		q.Add("command", shell)
		if c := strings.TrimSpace(r.URL.Query().Get("container")); c != "" {
			q.Set("container", c)
		}
		// https://{host} → wss://{host}
		wsBase := "wss://" + strings.TrimPrefix(strings.TrimPrefix(client.base, "https://"), "http://")
		execURL := fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s/exec?%s",
			wsBase, url.PathEscape(ns), url.PathEscape(pod), q.Encode())

		// Accept the browser WS first.
		bws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer bws.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Dial the cluster's exec WS using the cluster's TLS client.
		dialOpts := &websocket.DialOptions{
			HTTPClient:   client.http,
			Subprotocols: []string{"v4.channel.k8s.io", "v3.channel.k8s.io", "v2.channel.k8s.io", "channel.k8s.io"},
		}
		if client.token != "" {
			dialOpts.HTTPHeader = http.Header{"Authorization": {"Bearer " + client.token}}
		}
		dctx, dcancel := context.WithTimeout(ctx, 20*time.Second)
		kws, _, err := websocket.Dial(dctx, execURL, dialOpts)
		dcancel()
		if err != nil {
			bws.Write(ctx, websocket.MessageText, []byte("\r\nexec failed: "+err.Error()+"\r\n"))
			return
		}
		kws.SetReadLimit(8 << 20)
		defer kws.Close(websocket.StatusNormalClosure, "")

		// Browser → cluster: binary frames are stdin; text frames are resize JSON.
		go func() {
			for {
				mt, data, err := bws.Read(ctx)
				if err != nil {
					cancel()
					return
				}
				if mt == websocket.MessageText {
					var rs struct {
						Cols int `json:"cols"`
						Rows int `json:"rows"`
					}
					if json.Unmarshal(data, &rs) == nil && rs.Cols > 0 {
						payload, _ := json.Marshal(map[string]int{"Width": rs.Cols, "Height": rs.Rows})
						kws.Write(ctx, websocket.MessageBinary, append([]byte{kChResize}, payload...))
					}
					continue
				}
				kws.Write(ctx, websocket.MessageBinary, append([]byte{kChStdin}, data...))
			}
		}()

		// Cluster → browser: forward stdout/stderr payloads.
		for {
			_, data, err := kws.Read(ctx)
			if err != nil {
				return
			}
			if len(data) == 0 {
				continue
			}
			switch data[0] {
			case kChStdout, kChStderr:
				if werr := bws.Write(ctx, websocket.MessageBinary, data[1:]); werr != nil {
					return
				}
			default:
				// channel 3 (error/status) and others are ignored for the TTY.
			}
		}
	}
}
