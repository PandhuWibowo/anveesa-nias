package handlers

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

//go:embed marketplace_catalog.json
var bundledCatalogJSON []byte

type marketEnv struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

type marketApp struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Category    string      `json:"category"`
	Description string      `json:"description"`
	Icon        string      `json:"icon"`
	Website     string      `json:"website"`
	Source      string      `json:"source"` // bundled | custom | <catalog name>
	Env         []marketEnv `json:"env"`
	Compose     string      `json:"compose"`
}

// ── Catalog assembly ──────────────────────────────────────────────────────

func bundledApps() []marketApp {
	var doc struct {
		Apps []marketApp `json:"apps"`
	}
	json.Unmarshal(bundledCatalogJSON, &doc)
	for i := range doc.Apps {
		doc.Apps[i].Source = "bundled"
	}
	return doc.Apps
}

func customApps() []marketApp {
	apps := []marketApp{}
	rows, err := appdb.DB.Query(appdb.ConvertQuery(
		`SELECT slug, name, category, COALESCE(description,''), COALESCE(icon,''), COALESCE(website,''), compose, COALESCE(env_json,'[]')
		 FROM marketplace_custom_apps ORDER BY name ASC`))
	if err != nil {
		return apps
	}
	defer rows.Close()
	for rows.Next() {
		var a marketApp
		var envJSON string
		if rows.Scan(&a.ID, &a.Name, &a.Category, &a.Description, &a.Icon, &a.Website, &a.Compose, &envJSON) != nil {
			continue
		}
		json.Unmarshal([]byte(envJSON), &a.Env)
		a.Source = "custom"
		apps = append(apps, a)
	}
	return apps
}

func remoteApps() []marketApp {
	apps := []marketApp{}
	rows, err := appdb.DB.Query(appdb.ConvertQuery(
		`SELECT name, url FROM marketplace_catalogs WHERE enabled = 1`))
	if err != nil {
		return apps
	}
	type cat struct{ name, url string }
	var cats []cat
	for rows.Next() {
		var c cat
		if rows.Scan(&c.name, &c.url) == nil {
			cats = append(cats, c)
		}
	}
	rows.Close()

	client := &http.Client{Timeout: 6 * time.Second}
	for _, c := range cats {
		resp, err := client.Get(c.url)
		if err != nil {
			continue
		}
		var doc struct {
			Apps []marketApp `json:"apps"`
		}
		json.NewDecoder(resp.Body).Decode(&doc)
		resp.Body.Close()
		for i := range doc.Apps {
			doc.Apps[i].Source = c.name
			apps = append(apps, doc.Apps[i])
		}
	}
	return apps
}

func mergedCatalog() []marketApp {
	all := bundledApps()
	all = append(all, customApps()...)
	all = append(all, remoteApps()...)
	return all
}

func findMarketApp(id string) *marketApp {
	for _, a := range mergedCatalog() {
		if a.ID == id {
			app := a
			return &app
		}
	}
	return nil
}

// MarketplaceCatalog returns the merged app catalog.
func MarketplaceCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"apps": mergedCatalog()})
	}
}

// ── Catalog sources (remote registry URLs) ────────────────────────────────

func MarketplaceCatalogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, url, enabled, created_at FROM marketplace_catalogs ORDER BY id ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		type src struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Created string `json:"created_at"`
		}
		list := []src{}
		for rows.Next() {
			var s src
			var en int
			rows.Scan(&s.ID, &s.Name, &s.URL, &en, &s.Created)
			s.Enabled = en == 1
			list = append(list, s)
		}
		json.NewEncoder(w).Encode(list)
	}
}

func CreateMarketplaceCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var b struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil || strings.TrimSpace(b.URL) == "" {
			http.Error(w, `{"error":"name and url are required"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(b.Name) == "" {
			b.Name = b.URL
		}
		_, err := appdb.DB.Exec(appdb.ConvertQuery(
			`INSERT INTO marketplace_catalogs (name, url, enabled) VALUES (?,?,1)`), b.Name, b.URL)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

func DeleteMarketplaceCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := lastPathID(r)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM marketplace_catalogs WHERE id=?`), id)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// ── Custom apps ───────────────────────────────────────────────────────────

func MarketplaceCustomApps() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, slug, name, category, COALESCE(description,''), COALESCE(icon,''), COALESCE(website,''), compose, COALESCE(env_json,'[]')
			 FROM marketplace_custom_apps ORDER BY name ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		type capp struct {
			ID       int64       `json:"id"`
			Slug     string      `json:"slug"`
			Name     string      `json:"name"`
			Category string      `json:"category"`
			Desc     string      `json:"description"`
			Icon     string      `json:"icon"`
			Website  string      `json:"website"`
			Compose  string      `json:"compose"`
			Env      []marketEnv `json:"env"`
		}
		list := []capp{}
		for rows.Next() {
			var a capp
			var envJSON string
			rows.Scan(&a.ID, &a.Slug, &a.Name, &a.Category, &a.Desc, &a.Icon, &a.Website, &a.Compose, &envJSON)
			json.Unmarshal([]byte(envJSON), &a.Env)
			list = append(list, a)
		}
		json.NewEncoder(w).Encode(list)
	}
}

func CreateMarketplaceCustomApp() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var b struct {
			Slug     string      `json:"slug"`
			Name     string      `json:"name"`
			Category string      `json:"category"`
			Desc     string      `json:"description"`
			Icon     string      `json:"icon"`
			Website  string      `json:"website"`
			Compose  string      `json:"compose"`
			Env      []marketEnv `json:"env"`
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil || strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.Compose) == "" {
			http.Error(w, `{"error":"name and compose are required"}`, http.StatusBadRequest)
			return
		}
		slug := strings.TrimSpace(b.Slug)
		if slug == "" {
			slug = "custom-" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(b.Name), " ", "-"))
		}
		if strings.TrimSpace(b.Category) == "" {
			b.Category = "Custom"
		}
		envJSON, _ := json.Marshal(b.Env)
		_, err := appdb.DB.Exec(appdb.ConvertQuery(
			`INSERT INTO marketplace_custom_apps (slug, name, category, description, icon, website, compose, env_json)
			 VALUES (?,?,?,?,?,?,?,?)`),
			slug, b.Name, b.Category, b.Desc, b.Icon, b.Website, b.Compose, string(envJSON))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

func DeleteMarketplaceCustomApp() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := lastPathID(r)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM marketplace_custom_apps WHERE id=?`), id)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// ── Install / uninstall ───────────────────────────────────────────────────

func renderCompose(app *marketApp, overrides map[string]string) string {
	out := app.Compose
	for _, e := range app.Env {
		val := e.Value
		if v, ok := overrides[e.Key]; ok && strings.TrimSpace(v) != "" {
			val = v
		}
		out = strings.ReplaceAll(out, "${"+e.Key+"}", val)
	}
	for k, v := range overrides {
		out = strings.ReplaceAll(out, "${"+k+"}", v)
	}
	return out
}

// MarketplaceInstall renders an app's compose and deploys it to a host.
func MarketplaceInstall() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var b struct {
			AppID     string            `json:"app_id"`
			HostID    int64             `json:"host_id"`
			StackName string            `json:"stack_name"`
			Env       map[string]string `json:"env"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		app := findMarketApp(b.AppID)
		if app == nil {
			http.Error(w, `{"error":"app not found in catalog"}`, http.StatusBadRequest)
			return
		}
		stack := strings.TrimSpace(b.StackName)
		if stack == "" {
			stack = app.ID
		}
		if !composeNamePattern.MatchString(stack) {
			http.Error(w, `{"error":"invalid stack name — use lowercase letters, digits, - and _"}`, http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(b.HostID)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusNotFound)
			return
		}
		yaml := renderCompose(app, b.Env)
		out, runErr := runHostCommand(h, []string{"docker", "compose", "-p", stack, "-f", "-", "up", "-d"}, yaml)
		result := map[string]interface{}{"output": out, "ok": runErr == nil}
		if runErr != nil {
			result["error"] = runErr.Error() + " — is the docker compose plugin installed on the host?"
			json.NewEncoder(w).Encode(result)
			return
		}
		appdb.DB.Exec(appdb.ConvertQuery(
			`INSERT INTO marketplace_installs (app_id, app_name, host_id, stack_name) VALUES (?,?,?,?)`),
			app.ID, app.Name, b.HostID, stack)
		json.NewEncoder(w).Encode(result)
	}
}

func MarketplaceInstalls() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT i.id, i.app_id, i.app_name, i.host_id, COALESCE(h.name,'?'), i.stack_name, i.installed_at
			 FROM marketplace_installs i LEFT JOIN docker_hosts h ON h.id = i.host_id
			 ORDER BY i.installed_at DESC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		type inst struct {
			ID        int64  `json:"id"`
			AppID     string `json:"app_id"`
			AppName   string `json:"app_name"`
			HostID    int64  `json:"host_id"`
			HostName  string `json:"host_name"`
			StackName string `json:"stack_name"`
			Installed string `json:"installed_at"`
		}
		list := []inst{}
		for rows.Next() {
			var i inst
			rows.Scan(&i.ID, &i.AppID, &i.AppName, &i.HostID, &i.HostName, &i.StackName, &i.Installed)
			list = append(list, i)
		}
		json.NewEncoder(w).Encode(list)
	}
}

func MarketplaceUninstall() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var b struct {
			ID int64 `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		var hostID int64
		var stack string
		err := appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT host_id, stack_name FROM marketplace_installs WHERE id=?`), b.ID).Scan(&hostID, &stack)
		if err != nil {
			http.Error(w, jsonError("install not found"), http.StatusNotFound)
			return
		}
		h, err := loadDockerHost(hostID)
		if err == nil {
			runHostCommand(h, []string{"docker", "compose", "-p", stack, "down", "--remove-orphans"}, "")
		}
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM marketplace_installs WHERE id=?`), b.ID)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// ── Tool tiles ────────────────────────────────────────────────────────────

func MarketplaceTiles() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, COALESCE(icon,''), url, sort_order FROM marketplace_tiles ORDER BY sort_order ASC, id ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		type tile struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Icon string `json:"icon"`
			URL  string `json:"url"`
			Sort int    `json:"sort_order"`
		}
		list := []tile{}
		for rows.Next() {
			var t tile
			rows.Scan(&t.ID, &t.Name, &t.Icon, &t.URL, &t.Sort)
			list = append(list, t)
		}
		json.NewEncoder(w).Encode(list)
	}
}

func CreateMarketplaceTile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var b struct {
			Name string `json:"name"`
			Icon string `json:"icon"`
			URL  string `json:"url"`
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil || strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.URL) == "" {
			http.Error(w, `{"error":"name and url are required"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(b.Icon) == "" {
			b.Icon = "🔗"
		}
		_, err := appdb.DB.Exec(appdb.ConvertQuery(
			`INSERT INTO marketplace_tiles (name, icon, url) VALUES (?,?,?)`), b.Name, b.Icon, b.URL)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

func DeleteMarketplaceTile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := lastPathID(r)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM marketplace_tiles WHERE id=?`), id)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// lastPathID parses a trailing numeric id from the request path.
func lastPathID(r *http.Request) int64 {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	id, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return id
}
