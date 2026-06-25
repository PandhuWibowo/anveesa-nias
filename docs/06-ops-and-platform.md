# Ops And Platform

Ops and platform features cover scheduled work, backups, health visibility, and runtime signals inside the application.

## Scheduler

Route:
- `/scheduler`

Purpose:
- Schedules recurring tasks or query jobs.
- Turns repeated manual checks into planned jobs.

Use cases:
- Run a recurring report query.
- Schedule a repeated maintenance check.
- Automate a data validation query.
- Keep operational tasks visible to administrators.

Typical workflow:
1. Open scheduler.
2. Create a scheduled job.
3. Select the target query, task, or connection context.
4. Set interval or timing.
5. Save and monitor recent runs.

Expected result:
- The task runs according to its schedule and exposes status/history for review.

Notes:
- Keep scheduled queries focused.
- Avoid schedules that generate unnecessary database load.
- Review failed jobs regularly.

Screenshot:
- `docs/screenshots/scheduler-page.png`

## Backup

Route:
- `/backup`

Purpose:
- Provides backup and restore operations where supported by the app configuration.

Use cases:
- Create a manual backup before a risky administrative change.
- Review backup status.
- Restore app-managed data in a controlled situation.

Typical workflow:
1. Open backup.
2. Review available backup actions.
3. Trigger backup or restore when appropriate.
4. Confirm the result and audit any follow-up actions.

Expected result:
- Backup operations are visible and deliberate.

Notes:
- Backups can contain sensitive connection metadata and audit data.
- Store backup artifacts securely.

Screenshot:
- `docs/screenshots/backup-page.png`

## Runtime Health

Route:
- `/health`

Purpose:
- Shows whether core services and connection checks are healthy.

Use cases:
- Confirm the backend is responding.
- Check a connection after credential rotation.
- Verify service readiness after configuration changes.

Typical workflow:
1. Open health.
2. Review status cards and failed checks.
3. Investigate any failing dependency or connection.

Expected result:
- Users can distinguish between app-level and database-level health problems.

Screenshot:
- `docs/screenshots/health-endpoint-check.png`

## Data Script Runtime Notes

Purpose:
- Documents the native runtimes used by data-script execution.

Supported runtime commands:
- `node`
- `python3`
- `php`

Use cases:
- A script author chooses the best language for a planned data transformation.
- A maintainer checks which runtime must be available for a script type.

Notes:
- Script behavior should be deterministic and reviewable.
- Avoid scripts that depend on hidden local state.

---

## Nginx Management

Route:
- `/nginx`

Purpose:
- Manage nginx configuration, sites, logs, and TLS certificates on remote servers over SSH.

Use cases:
- Edit nginx config files directly from the browser.
- Enable or disable virtual host sites.
- Tail and search access/error logs in real time.
- Inspect TLS certificate expiry across all servers.
- Run `nginx -t` config tests and reload the service.
- View the full fleet health of all nginx hosts at once.

Connection workflow:
1. Select a host from the searchable dropdown (hosts come from Docker → SSH hosts).
2. The app pings the host via SSH and shows a status badge: **Connected** (green), **Unreachable** (red), or **Testing…** (grey blinking).
3. Use **Connect** to re-test SSH reachability after a failure.
4. Use **Disconnect** to stop log following and clear the active session without removing the host.

Tabs:
- **Config** — file tree + CodeMirror editor with backup/revert.
- **Sites** — list sites with enable/disable toggle.
- **Logs** — file selector, live follow, keyword search with level filter.
- **Map** — visualise server blocks and upstream groups.
- **Certs** — TLS certificate expiry summary per domain.
- **Status** — nginx stub_status metrics with req/s rate.

Notes:
- Hosts must be added under Docker → Add host (Remote host) before they appear here.
- `sudo` can be toggled per host for elevated nginx access.
- Compressed log files (`.gz`, `.bz2`) can be read but not followed.

Screenshot:
- `docs/screenshots/nginx-page.png`

---

## Docker Hosts Management

Route:
- `/docker-hosts`

Purpose:
- Dedicated page for listing and managing all Docker SSH host connections.

Use cases:
- Add a new remote server as a Docker host.
- Edit SSH credentials or socket path for an existing host.
- Delete a host connection that is no longer needed.
- Test SSH + Docker daemon connectivity for each host.
- View reachability, running container count, image count, and Docker version for each host at a glance.

Typical workflow:
1. Open Docker Hosts.
2. Click **Add host** to register a remote server.
3. Enter SSH credentials (password or private key).
4. Use **Test** to verify connectivity before saving.
5. Click **Manage →** on any card to jump to the Docker page for that host.

Notes:
- Hosts added here are shared with the Nginx management page.
- SSH credentials are AES-encrypted at rest using `NIAS_ENCRYPTION_KEY`.

Screenshot:
- `docs/screenshots/docker-hosts-page.png`

---

## Docker Management

Route:
- `/docker`

Purpose:
- Manage remote Docker hosts and their containers, images, volumes, networks, and compose stacks directly from the browser over SSH.

Use cases:
- View running and stopped containers across multiple VMs.
- Start, stop, restart, or remove containers.
- Pull or remove images.
- Manage volumes and networks.
- Run `docker compose up/down` for stacks.
- Open an interactive terminal (exec) inside a running container.
- Browse container logs.

Typical workflow:
1. Add a remote host under **Docker → Add host** with SSH credentials.
2. Select the host from the host picker — the app tunnels into the Docker daemon socket over SSH.
3. Navigate to Containers, Images, Volumes, Networks, or Compose tabs.
4. Act on resources via the action buttons.

Notes:
- No Docker TCP port needs to be exposed on the remote VM — everything goes over the SSH socket tunnel.
- SSH credentials are AES-encrypted at rest using `NIAS_ENCRYPTION_KEY`.
- The same SSH host list is shared with the Nginx management page.

Screenshot:
- `docs/screenshots/docker-page.png`

---

## Kubernetes Management

Routes:
- `/kube-clusters` — cluster connections
- `/kubernetes` — workload browser

Purpose:
- Read-only management of Kubernetes clusters (Alibaba ACK, Huawei CCE, or any standard kubeconfig) directly from the browser, plus pod logs and an interactive exec terminal.

Connection workflow:
1. Open **Kubernetes Clusters → Add cluster**.
2. Pick the provider (Alibaba ACK / Huawei CCE / Other) and paste the cluster's kubeconfig (or upload the file).
   - ACK: cluster detail → **Connection Information** → Public or Internal Access kubeconfig.
   - Use the **Public Access** kubeconfig if Nias reaches the cluster over the internet; **Internal Access** only if Nias runs inside the cluster's VPC.
3. **Test connection** to confirm the API server is reachable, then Save.
4. Open **Kubernetes**, select the cluster, and browse.

Resource browser (Lens-style left sidebar):
- **Cluster** — Overview, Nodes, Namespaces.
- **Workloads** — Pods, Deployments, StatefulSets, DaemonSets, Jobs, CronJobs.
- **Network** — Services, Ingresses.
- **Config & Storage** — ConfigMaps, Secrets (values masked), PVCs.
- **Events**.

Per-resource actions:
- **YAML** — describe any object as YAML in a side drawer.
- **Logs** (pods) — snapshot or live follow, with a container selector.
- **Exec** (pods) — interactive in-browser shell, gated behind the high-risk `kube.exec` permission.

Notes:
- The kubeconfig is AES-encrypted at rest using `NIAS_ENCRYPTION_KEY`.
- The Nias host must have network reachability to each cluster's API server (use the public-access kubeconfig for clusters outside its network).
- Auth supported: embedded client certificate or bearer token. Exec-plugin kubeconfigs (e.g. `ack-ram-authenticator` `exec:` blocks) are not yet supported.
- This integration is read-only — no resource mutations (scale/delete/apply). The exec terminal can run commands inside a container, which is why it requires a separate high-risk permission.
- Permissions: `kube.view` (browse), `kube.manage` (manage cluster connections), `kube.exec` (pod exec).

Screenshot:
- `docs/screenshots/kubernetes-page.png`
