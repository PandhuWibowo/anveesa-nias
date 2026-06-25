package handlers

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appdb "github.com/anveesa/nias/db"
	"gopkg.in/yaml.v3"
)

// Kubernetes clusters are reached directly over the Kubernetes REST API using a
// stored kubeconfig. Both Alibaba ACK and Huawei CCE export a standard
// kubeconfig (client-certificate or token auth — no exec plugins), so a single
// lightweight HTTPS client covers both. This is read-only management: cluster
// info, nodes, namespaces, pods, deployments, services, events, and pod logs.

// ── Stored cluster types ──────────────────────────────────────────────────

type KubeCluster struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"` // alibaba | huawei | other
	Context    string `json:"context"`
	Kubeconfig string `json:"kubeconfig,omitempty"` // only returned on demand
	OwnerID    int64  `json:"owner_id"`
	CreatedAt  string `json:"created_at"`
}

type KubeClusterInput struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Context    string `json:"context"`
	Kubeconfig string `json:"kubeconfig"`
}

var kubeProviders = map[string]bool{"alibaba": true, "huawei": true, "other": true}

// ── kubeconfig parsing ─────────────────────────────────────────────────────

type kubeconfigFile struct {
	Clusters []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server   string `yaml:"server"`
			CAData   string `yaml:"certificate-authority-data"`
			Insecure bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			ClientCertData string `yaml:"client-certificate-data"`
			ClientKeyData  string `yaml:"client-key-data"`
			Token          string `yaml:"token"`
		} `yaml:"user"`
	} `yaml:"users"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster   string `yaml:"cluster"`
			User      string `yaml:"user"`
			Namespace string `yaml:"namespace"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	CurrentContext string `yaml:"current-context"`
}

// kubeClient is a minimal authenticated client for one cluster context.
type kubeClient struct {
	base  string // API server URL, no trailing slash
	ns    string // default namespace from the context
	token string // bearer token, if token auth
	http  *http.Client
}

func buildKubeClient(kubeconfig, ctxOverride string) (*kubeClient, error) {
	var kc kubeconfigFile
	if err := yaml.Unmarshal([]byte(kubeconfig), &kc); err != nil {
		return nil, fmt.Errorf("invalid kubeconfig: %w", err)
	}
	if len(kc.Clusters) == 0 || len(kc.Users) == 0 {
		return nil, fmt.Errorf("kubeconfig has no clusters or users")
	}

	ctxName := strings.TrimSpace(ctxOverride)
	if ctxName == "" {
		ctxName = kc.CurrentContext
	}
	var cluName, userName, ns string
	for _, c := range kc.Contexts {
		if c.Name == ctxName {
			cluName, userName, ns = c.Context.Cluster, c.Context.User, c.Context.Namespace
			break
		}
	}
	if cluName == "" && len(kc.Contexts) > 0 { // fall back to first context
		c := kc.Contexts[0]
		cluName, userName, ns = c.Context.Cluster, c.Context.User, c.Context.Namespace
	}

	// Resolve cluster + user (fall back to the first of each).
	cluIdx, userIdx := 0, 0
	for i, c := range kc.Clusters {
		if c.Name == cluName {
			cluIdx = i
			break
		}
	}
	for i, u := range kc.Users {
		if u.Name == userName {
			userIdx = i
			break
		}
	}
	clu := kc.Clusters[cluIdx].Cluster
	usr := kc.Users[userIdx].User

	if strings.TrimSpace(clu.Server) == "" {
		return nil, fmt.Errorf("kubeconfig cluster has no server URL")
	}

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if clu.Insecure {
		tlsCfg.InsecureSkipVerify = true
	}
	if clu.CAData != "" {
		pem, err := base64.StdEncoding.DecodeString(strings.TrimSpace(clu.CAData))
		if err != nil {
			return nil, fmt.Errorf("decode CA data: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA data is not valid PEM")
		}
		tlsCfg.RootCAs = pool
	}

	var token string
	if usr.ClientCertData != "" && usr.ClientKeyData != "" {
		certPEM, err := base64.StdEncoding.DecodeString(strings.TrimSpace(usr.ClientCertData))
		if err != nil {
			return nil, fmt.Errorf("decode client cert: %w", err)
		}
		keyPEM, err := base64.StdEncoding.DecodeString(strings.TrimSpace(usr.ClientKeyData))
		if err != nil {
			return nil, fmt.Errorf("decode client key: %w", err)
		}
		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("client cert/key pair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{pair}
	} else if usr.Token != "" {
		token = strings.TrimSpace(usr.Token)
	} else {
		return nil, fmt.Errorf("kubeconfig user has no client certificate or token")
	}

	if ns == "" {
		ns = "default"
	}
	return &kubeClient{
		base:  strings.TrimRight(clu.Server, "/"),
		ns:    ns,
		token: token,
		http: &http.Client{
			Timeout:   25 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

func (kc *kubeClient) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, kc.base+path, nil)
	if err != nil {
		return err
	}
	if kc.token != "" {
		req.Header.Set("Authorization", "Bearer "+kc.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := kc.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("kubernetes api %d: %s", resp.StatusCode, msg)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (kc *kubeClient) getText(path string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, kc.base+path, nil)
	if err != nil {
		return "", err
	}
	if kc.token != "" {
		req.Header.Set("Authorization", "Bearer "+kc.token)
	}
	resp, err := kc.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // cap at 5MB
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("kubernetes api %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

// ── cluster client cache ───────────────────────────────────────────────────

var (
	kubeCacheMu sync.Mutex
	kubeCache   = map[int64]*kubeClient{}
)

func clientForCluster(id int64) (*kubeClient, error) {
	kubeCacheMu.Lock()
	if c, ok := kubeCache[id]; ok {
		kubeCacheMu.Unlock()
		return c, nil
	}
	kubeCacheMu.Unlock()

	cl, err := loadKubeCluster(id)
	if err != nil {
		return nil, err
	}
	client, err := buildKubeClient(cl.Kubeconfig, cl.Context)
	if err != nil {
		return nil, err
	}
	kubeCacheMu.Lock()
	kubeCache[id] = client
	kubeCacheMu.Unlock()
	return client, nil
}

func evictKubeClient(id int64) {
	kubeCacheMu.Lock()
	delete(kubeCache, id)
	kubeCacheMu.Unlock()
}

func loadKubeCluster(id int64) (*KubeCluster, error) {
	var c KubeCluster
	var encKubeconfig string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(
		`SELECT id, name, provider, kubeconfig, context, COALESCE(owner_id,0), created_at
		 FROM kube_clusters WHERE id=?`), id).
		Scan(&c.ID, &c.Name, &c.Provider, &encKubeconfig, &c.Context, &c.OwnerID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Kubeconfig, _ = decryptCredential(encKubeconfig)
	return &c, nil
}

// ── Cluster CRUD ───────────────────────────────────────────────────────────

func ListKubeClusters() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, provider, context, COALESCE(owner_id,0), created_at
			 FROM kube_clusters ORDER BY name ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		list := []KubeCluster{}
		for rows.Next() {
			var c KubeCluster
			if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.Context, &c.OwnerID, &c.CreatedAt); err != nil {
				continue
			}
			list = append(list, c) // never expose the stored kubeconfig in list
		}
		json.NewEncoder(w).Encode(list)
	}
}

func validateKubeInput(in *KubeClusterInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(in.Kubeconfig) == "" {
		return fmt.Errorf("kubeconfig is required")
	}
	if in.Provider == "" {
		in.Provider = "other"
	}
	if !kubeProviders[in.Provider] {
		return fmt.Errorf("invalid provider")
	}
	// Validate the kubeconfig parses and yields a usable client.
	if _, err := buildKubeClient(in.Kubeconfig, in.Context); err != nil {
		return err
	}
	return nil
}

func CreateKubeCluster() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in KubeClusterInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if err := validateKubeInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		enc, err := encryptCredential(in.Kubeconfig)
		if err != nil {
			http.Error(w, jsonError("encrypt kubeconfig: "+err.Error()), http.StatusInternalServerError)
			return
		}
		ownerID, _ := currentUserID(r)

		var id int64
		insert := `INSERT INTO kube_clusters (name, provider, kubeconfig, context, owner_id) VALUES (?,?,?,?,?)`
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			err = appdb.DB.QueryRow(appdb.ConvertQuery(insert+" RETURNING id"),
				in.Name, in.Provider, enc, in.Context, ownerID).Scan(&id)
		} else {
			var res interface{ LastInsertId() (int64, error) }
			res, err = appdb.DB.Exec(appdb.ConvertQuery(insert),
				in.Name, in.Provider, enc, in.Context, ownerID)
			if err == nil {
				id, _ = res.LastInsertId()
			}
		}
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": id})
	}
}

func UpdateKubeCluster() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := kubeIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid cluster id"}`, http.StatusBadRequest)
			return
		}
		var in KubeClusterInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if in.Provider == "" {
			in.Provider = "other"
		}
		if !kubeProviders[in.Provider] {
			http.Error(w, jsonError("invalid provider"), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			http.Error(w, jsonError("name is required"), http.StatusBadRequest)
			return
		}

		// An empty kubeconfig means "keep the existing one".
		if strings.TrimSpace(in.Kubeconfig) == "" {
			if _, err := appdb.DB.Exec(appdb.ConvertQuery(
				`UPDATE kube_clusters SET name=?, provider=?, context=? WHERE id=?`),
				in.Name, in.Provider, in.Context, id); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
		} else {
			if _, err := buildKubeClient(in.Kubeconfig, in.Context); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			enc, err := encryptCredential(in.Kubeconfig)
			if err != nil {
				http.Error(w, jsonError("encrypt kubeconfig: "+err.Error()), http.StatusInternalServerError)
				return
			}
			if _, err := appdb.DB.Exec(appdb.ConvertQuery(
				`UPDATE kube_clusters SET name=?, provider=?, kubeconfig=?, context=? WHERE id=?`),
				in.Name, in.Provider, enc, in.Context, id); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
		}
		evictKubeClient(id)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func DeleteKubeCluster() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := kubeIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid cluster id"}`, http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM kube_clusters WHERE id=?`), id); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		evictKubeClient(id)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// TestKubeCluster validates an unsaved kubeconfig by hitting /version.
func TestKubeCluster() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in KubeClusterInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		client, err := buildKubeClient(in.Kubeconfig, in.Context)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var v kubeVersion
		if err := client.get("/version", &v); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"version": v.GitVersion, "platform": v.Platform})
	}
}

// ── Read-only resource handlers ────────────────────────────────────────────

func kubeIDFromPath(r *http.Request) (int64, error) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/kube/clusters/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("missing cluster id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

func clientFromPath(w http.ResponseWriter, r *http.Request) (*kubeClient, bool) {
	id, err := kubeIDFromPath(r)
	if err != nil {
		http.Error(w, `{"error":"invalid cluster id"}`, http.StatusBadRequest)
		return nil, false
	}
	client, err := clientForCluster(id)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return nil, false
	}
	return client, true
}

type kubeVersion struct {
	GitVersion string `json:"gitVersion"`
	Platform   string `json:"platform"`
}

// KubePing returns cluster version/platform — used as the connectivity check.
func KubePing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, ok := clientFromPath(w, r)
		if !ok {
			return
		}
		var v kubeVersion
		if err := client.get("/version", &v); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"version": v.GitVersion, "platform": v.Platform})
	}
}

// minimal k8s JSON shapes (only the fields we surface)

type k8sMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Labels            map[string]string `json:"labels"`
}

type kubeNode struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Roles      string `json:"roles"`
	Version    string `json:"version"`
	OS         string `json:"os"`
	CPU        string `json:"cpu"`
	Memory     string `json:"memory"`
	InternalIP string `json:"internal_ip"`
	Created    string `json:"created"`
}

func KubeNodes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, ok := clientFromPath(w, r)
		if !ok {
			return
		}
		var resp struct {
			Items []struct {
				Metadata k8sMeta `json:"metadata"`
				Status   struct {
					Conditions []struct {
						Type   string `json:"type"`
						Status string `json:"status"`
					} `json:"conditions"`
					NodeInfo struct {
						KubeletVersion string `json:"kubeletVersion"`
						OSImage        string `json:"osImage"`
						Architecture   string `json:"architecture"`
					} `json:"nodeInfo"`
					Capacity  map[string]string `json:"capacity"`
					Addresses []struct {
						Type    string `json:"type"`
						Address string `json:"address"`
					} `json:"addresses"`
				} `json:"status"`
			} `json:"items"`
		}
		if err := client.get("/api/v1/nodes", &resp); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		out := []kubeNode{}
		for _, it := range resp.Items {
			n := kubeNode{
				Name:    it.Metadata.Name,
				Version: it.Status.NodeInfo.KubeletVersion,
				OS:      it.Status.NodeInfo.OSImage,
				Created: it.Metadata.CreationTimestamp,
				Status:  "NotReady",
			}
			for _, c := range it.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					n.Status = "Ready"
				}
			}
			if it.Status.Capacity != nil {
				n.CPU = it.Status.Capacity["cpu"]
				n.Memory = it.Status.Capacity["memory"]
			}
			for _, a := range it.Status.Addresses {
				if a.Type == "InternalIP" {
					n.InternalIP = a.Address
				}
			}
			roles := []string{}
			for k := range it.Metadata.Labels {
				if strings.HasPrefix(k, "node-role.kubernetes.io/") {
					role := strings.TrimPrefix(k, "node-role.kubernetes.io/")
					if role != "" {
						roles = append(roles, role)
					}
				}
			}
			sort.Strings(roles)
			n.Roles = strings.Join(roles, ",")
			if n.Roles == "" {
				n.Roles = "<none>"
			}
			out = append(out, n)
		}
		json.NewEncoder(w).Encode(out)
	}
}

type kubeNamespace struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Created string `json:"created"`
}

func KubeNamespaces() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, ok := clientFromPath(w, r)
		if !ok {
			return
		}
		var resp struct {
			Items []struct {
				Metadata k8sMeta `json:"metadata"`
				Status   struct {
					Phase string `json:"phase"`
				} `json:"status"`
			} `json:"items"`
		}
		if err := client.get("/api/v1/namespaces", &resp); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		out := []kubeNamespace{}
		for _, it := range resp.Items {
			out = append(out, kubeNamespace{
				Name:    it.Metadata.Name,
				Status:  it.Status.Phase,
				Created: it.Metadata.CreationTimestamp,
			})
		}
		json.NewEncoder(w).Encode(out)
	}
}

type kubePod struct {
	Name       string   `json:"name"`
	Namespace  string   `json:"namespace"`
	Status     string   `json:"status"`
	Ready      string   `json:"ready"`
	Restarts   int      `json:"restarts"`
	Node       string   `json:"node"`
	PodIP      string   `json:"pod_ip"`
	Created    string   `json:"created"`
	Containers []string `json:"containers"`
}

func KubePods() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, ok := clientFromPath(w, r)
		if !ok {
			return
		}
		path := "/api/v1/pods?limit=1000"
		if ns := strings.TrimSpace(r.URL.Query().Get("namespace")); ns != "" {
			path = "/api/v1/namespaces/" + ns + "/pods?limit=1000"
		}
		var resp struct {
			Items []struct {
				Metadata k8sMeta `json:"metadata"`
				Spec     struct {
					NodeName   string `json:"nodeName"`
					Containers []struct {
						Name string `json:"name"`
					} `json:"containers"`
				} `json:"spec"`
				Status struct {
					Phase             string `json:"phase"`
					PodIP             string `json:"podIP"`
					ContainerStatuses []struct {
						Ready        bool `json:"ready"`
						RestartCount int  `json:"restartCount"`
					} `json:"containerStatuses"`
				} `json:"status"`
			} `json:"items"`
		}
		if err := client.get(path, &resp); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		out := []kubePod{}
		for _, it := range resp.Items {
			p := kubePod{
				Name:      it.Metadata.Name,
				Namespace: it.Metadata.Namespace,
				Status:    it.Status.Phase,
				Node:      it.Spec.NodeName,
				PodIP:     it.Status.PodIP,
				Created:   it.Metadata.CreationTimestamp,
			}
			ready, total := 0, len(it.Status.ContainerStatuses)
			for _, cs := range it.Status.ContainerStatuses {
				if cs.Ready {
					ready++
				}
				p.Restarts += cs.RestartCount
			}
			p.Ready = fmt.Sprintf("%d/%d", ready, total)
			for _, c := range it.Spec.Containers {
				p.Containers = append(p.Containers, c.Name)
			}
			out = append(out, p)
		}
		json.NewEncoder(w).Encode(out)
	}
}

type kubeDeployment struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Ready     string   `json:"ready"`
	UpToDate  int      `json:"up_to_date"`
	Available int      `json:"available"`
	Created   string   `json:"created"`
	Images    []string `json:"images"`
}

func KubeDeployments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, ok := clientFromPath(w, r)
		if !ok {
			return
		}
		path := "/apis/apps/v1/deployments?limit=1000"
		if ns := strings.TrimSpace(r.URL.Query().Get("namespace")); ns != "" {
			path = "/apis/apps/v1/namespaces/" + ns + "/deployments?limit=1000"
		}
		var resp struct {
			Items []struct {
				Metadata k8sMeta `json:"metadata"`
				Spec     struct {
					Replicas int `json:"replicas"`
					Template struct {
						Spec struct {
							Containers []struct {
								Image string `json:"image"`
							} `json:"containers"`
						} `json:"spec"`
					} `json:"template"`
				} `json:"spec"`
				Status struct {
					ReadyReplicas     int `json:"readyReplicas"`
					UpdatedReplicas   int `json:"updatedReplicas"`
					AvailableReplicas int `json:"availableReplicas"`
				} `json:"status"`
			} `json:"items"`
		}
		if err := client.get(path, &resp); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		out := []kubeDeployment{}
		for _, it := range resp.Items {
			d := kubeDeployment{
				Name:      it.Metadata.Name,
				Namespace: it.Metadata.Namespace,
				Ready:     fmt.Sprintf("%d/%d", it.Status.ReadyReplicas, it.Spec.Replicas),
				UpToDate:  it.Status.UpdatedReplicas,
				Available: it.Status.AvailableReplicas,
				Created:   it.Metadata.CreationTimestamp,
			}
			for _, c := range it.Spec.Template.Spec.Containers {
				d.Images = append(d.Images, c.Image)
			}
			out = append(out, d)
		}
		json.NewEncoder(w).Encode(out)
	}
}

type kubeService struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Type       string `json:"type"`
	ClusterIP  string `json:"cluster_ip"`
	ExternalIP string `json:"external_ip"`
	Ports      string `json:"ports"`
	Created    string `json:"created"`
}

func KubeServices() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, ok := clientFromPath(w, r)
		if !ok {
			return
		}
		path := "/api/v1/services?limit=1000"
		if ns := strings.TrimSpace(r.URL.Query().Get("namespace")); ns != "" {
			path = "/api/v1/namespaces/" + ns + "/services?limit=1000"
		}
		var resp struct {
			Items []struct {
				Metadata k8sMeta `json:"metadata"`
				Spec     struct {
					Type        string   `json:"type"`
					ClusterIP   string   `json:"clusterIP"`
					ExternalIPs []string `json:"externalIPs"`
					Ports       []struct {
						Port     int    `json:"port"`
						NodePort int    `json:"nodePort"`
						Protocol string `json:"protocol"`
					} `json:"ports"`
				} `json:"spec"`
				Status struct {
					LoadBalancer struct {
						Ingress []struct {
							IP       string `json:"ip"`
							Hostname string `json:"hostname"`
						} `json:"ingress"`
					} `json:"loadBalancer"`
				} `json:"status"`
			} `json:"items"`
		}
		if err := client.get(path, &resp); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		out := []kubeService{}
		for _, it := range resp.Items {
			s := kubeService{
				Name:      it.Metadata.Name,
				Namespace: it.Metadata.Namespace,
				Type:      it.Spec.Type,
				ClusterIP: it.Spec.ClusterIP,
				Created:   it.Metadata.CreationTimestamp,
			}
			ext := []string{}
			ext = append(ext, it.Spec.ExternalIPs...)
			for _, ing := range it.Status.LoadBalancer.Ingress {
				if ing.IP != "" {
					ext = append(ext, ing.IP)
				} else if ing.Hostname != "" {
					ext = append(ext, ing.Hostname)
				}
			}
			if len(ext) == 0 {
				s.ExternalIP = "<none>"
			} else {
				s.ExternalIP = strings.Join(ext, ",")
			}
			ports := []string{}
			for _, p := range it.Spec.Ports {
				if p.NodePort > 0 {
					ports = append(ports, fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, p.Protocol))
				} else {
					ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
				}
			}
			s.Ports = strings.Join(ports, ", ")
			out = append(out, s)
		}
		json.NewEncoder(w).Encode(out)
	}
}

type kubeEvent struct {
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Object    string `json:"object"`
	Message   string `json:"message"`
	Count     int    `json:"count"`
	LastSeen  string `json:"last_seen"`
}

func KubeEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, ok := clientFromPath(w, r)
		if !ok {
			return
		}
		path := "/api/v1/events?limit=500"
		if ns := strings.TrimSpace(r.URL.Query().Get("namespace")); ns != "" {
			path = "/api/v1/namespaces/" + ns + "/events?limit=500"
		}
		var resp struct {
			Items []struct {
				Metadata       k8sMeta `json:"metadata"`
				Type           string  `json:"type"`
				Reason         string  `json:"reason"`
				Message        string  `json:"message"`
				Count          int     `json:"count"`
				LastTimestamp  string  `json:"lastTimestamp"`
				InvolvedObject struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
				} `json:"involvedObject"`
			} `json:"items"`
		}
		if err := client.get(path, &resp); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		out := []kubeEvent{}
		for _, it := range resp.Items {
			out = append(out, kubeEvent{
				Namespace: it.Metadata.Namespace,
				Type:      it.Type,
				Reason:    it.Reason,
				Object:    it.InvolvedObject.Kind + "/" + it.InvolvedObject.Name,
				Message:   it.Message,
				Count:     it.Count,
				LastSeen:  it.LastTimestamp,
			})
		}
		// Most recent first.
		sort.SliceStable(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
		json.NewEncoder(w).Encode(out)
	}
}

// KubePodLogs returns recent logs for a pod.
// Path: /api/kube/clusters/{id}/pods/{ns}/{pod}/logs?container=&tail=
func KubePodLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, ok := clientFromPath(w, r)
		if !ok {
			return
		}
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/kube/clusters/"), "/")
		parts := strings.Split(rest, "/") // {id}/pods/{ns}/{pod}/logs
		if len(parts) < 5 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		ns, pod := parts[2], parts[3]
		tail := strings.TrimSpace(r.URL.Query().Get("tail"))
		if tail == "" {
			tail = "300"
		}
		path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log?tailLines=%s&timestamps=true", ns, pod, tail)
		if c := strings.TrimSpace(r.URL.Query().Get("container")); c != "" {
			path += "&container=" + c
		}
		text, err := client.getText(path)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"logs": text})
	}
}

// KubeOverview returns a per-cluster reachability/summary used by the clusters
// list page (parity with Docker's overview).
type kubeSummary struct {
	ClusterID  int64  `json:"cluster_id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Reachable  bool   `json:"reachable"`
	Version    string `json:"version"`
	Nodes      int    `json:"nodes"`
	Namespaces int    `json:"namespaces"`
	Pods       int    `json:"pods"`
	Error      string `json:"error,omitempty"`
}

func KubeOverview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, provider FROM kube_clusters ORDER BY name ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		type clu struct {
			id       int64
			name     string
			provider string
		}
		var clusters []clu
		for rows.Next() {
			var c clu
			if err := rows.Scan(&c.id, &c.name, &c.provider); err == nil {
				clusters = append(clusters, c)
			}
		}
		rows.Close()

		out := make([]kubeSummary, len(clusters))
		var wg sync.WaitGroup
		for i, c := range clusters {
			wg.Add(1)
			go func(i int, c clu) {
				defer wg.Done()
				s := kubeSummary{ClusterID: c.id, Name: c.name, Provider: c.provider}
				client, err := clientForCluster(c.id)
				if err != nil {
					s.Error = err.Error()
					out[i] = s
					return
				}
				var v kubeVersion
				if err := client.get("/version", &v); err != nil {
					s.Error = err.Error()
					out[i] = s
					return
				}
				s.Reachable = true
				s.Version = v.GitVersion
				// Best-effort counts; ignore individual failures.
				var nodes struct {
					Items []json.RawMessage `json:"items"`
				}
				if client.get("/api/v1/nodes", &nodes) == nil {
					s.Nodes = len(nodes.Items)
				}
				var nss struct {
					Items []json.RawMessage `json:"items"`
				}
				if client.get("/api/v1/namespaces", &nss) == nil {
					s.Namespaces = len(nss.Items)
				}
				var pods struct {
					Items []json.RawMessage `json:"items"`
				}
				if client.get("/api/v1/pods?limit=2000", &pods) == nil {
					s.Pods = len(pods.Items)
				}
				out[i] = s
			}(i, c)
		}
		wg.Wait()
		json.NewEncoder(w).Encode(out)
	}
}
