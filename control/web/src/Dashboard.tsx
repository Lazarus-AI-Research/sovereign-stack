import { FormEvent, ReactNode, useCallback, useEffect, useState } from "react";
import {
  api,
  ApiError,
  Application,
  BackupManifest,
  Branding,
  BundleManifest,
  CatalogModel,
  CredentialMetadata,
  EmbeddingProfile,
  EvalReport,
  Features,
  IndexVersion,
  Identity,
  Job,
  Manifest,
  ModelEntry,
  NetworkStatus,
  Readiness,
  RuntimeErrors,
  Status,
  SupportBundle,
  UpdateInfo,
  Workspace,
} from "./api";
import lazarusLogo from "./assets/lazarus_logo.png";
import { applyTheme } from "./theme";
import { t } from "./i18n";

type RunAction = (label: string, action: () => Promise<unknown>) => Promise<boolean>;
type ConfirmAction = (options: { title: string; message: string; confirmLabel?: string; danger?: boolean }) => Promise<boolean>;

type PortalPage = "Chat" | "Activity" | "Tools" | "System" | "Models" | "Embeddings" | "Evaluations" | "Grafana" | "Phoenix" | "People" | "API & Providers" | "Network Access" | "Backups & Recovery" | "Updates" | "Settings";
type NavItem = { page: PortalPage; path: string; minimum: Identity["role"]; section: "primary" | "admin" };
const NAV: NavItem[] = [
  { page: "Chat", path: "/", minimum: "member", section: "primary" },
  { page: "Activity", path: "/activity", minimum: "member", section: "primary" },
  { page: "Tools", path: "/tools", minimum: "manager", section: "primary" },
  { page: "System", path: "/admin/system", minimum: "manager", section: "primary" },
  { page: "Models", path: "/admin/models", minimum: "manager", section: "admin" },
  { page: "Embeddings", path: "/admin/embeddings", minimum: "manager", section: "admin" },
  { page: "Evaluations", path: "/admin/evaluations", minimum: "manager", section: "admin" },
  { page: "People", path: "/admin/people", minimum: "admin", section: "admin" },
  { page: "API & Providers", path: "/admin/providers", minimum: "admin", section: "admin" },
  { page: "Network Access", path: "/admin/network", minimum: "admin", section: "admin" },
  { page: "Backups & Recovery", path: "/admin/recovery", minimum: "admin", section: "admin" },
  { page: "Updates", path: "/admin/updates", minimum: "admin", section: "admin" },
  { page: "Settings", path: "/admin/settings", minimum: "admin", section: "admin" },
];

const LEGACY_PATHS: Record<string, PortalPage> = {
  "/chat": "Chat", "/overview": "System", "/models": "Models", "/embeddings": "Embeddings",
  "/evaluations": "Evaluations", "/people": "People", "/access": "API & Providers",
  "/resilience": "Backups & Recovery", "/settings": "Settings",
  "/observe/grafana": "Grafana", "/observe/phoenix": "Phoenix",
};

const ROLE_LEVEL = { member: 0, manager: 1, admin: 2 };

const RUNTIME_STATES = ["initializing", "downloading", "compiling", "loading", "smoke_testing", "healthy"];

function StatePill({ state }: { state?: string }) {
  const tone = state === "healthy" || state === "ready" || state === "active" || state === "succeeded" || state === "complete" || state === "running" || state === "enabled" || state === "network" || state === "this computer"
    ? "ok"
    : state === "degraded" || state === "rolled_back" || state === "building" || state === "queued" || state === "validating" || state === "starting" || state === "scheduled" || state === "installing" || state === "loading" || state === "downloading" || state === "canceling"
      ? "warn"
      : state === "failed" || state === "error" || state === "unreachable" ? "bad" : "unknown";
  return <span className={`pill ${tone}`}>{state?.replaceAll("_", " ") ?? "unreachable"}</span>;
}

function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

function PanelTitle({ title, subtitle, action }: { title: string; subtitle?: string; action?: ReactNode }) {
  return (
    <div className="panel-title">
      <div><h2>{title}</h2>{subtitle && <p>{subtitle}</p>}</div>
      <div className="spacer" />{action}
    </div>
  );
}

function formatBytes(value: number) {
  if (!Number.isFinite(value)) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit += 1; }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

async function completeJob(start: { job_id?: string }, timeoutMs?: number) {
  if (start.job_id) await api.waitJob(start.job_id, timeoutMs);
}

function Activity({ canManage, run }: { canManage: boolean; run: RunAction }) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [readiness, setReadiness] = useState<Readiness | null>(null);
  const refresh = useCallback(async () => {
    const [activity, state] = await Promise.all([api.jobs(), api.readiness()]);
    setJobs(activity.jobs); setReadiness(state);
  }, []);
  useEffect(() => {
    refresh().catch(() => {});
    const readinessTimer = window.setInterval(() => api.readiness().then(setReadiness).catch(() => {}), 5000);
    let pollingTimer: number | null = null;
    const stream = new EventSource(api.jobsEventsURL(), { withCredentials: true });
    stream.addEventListener("jobs", (event) => {
      try { setJobs((JSON.parse((event as MessageEvent).data) as { jobs: Job[] }).jobs); } catch { /* Polling remains the compatibility fallback. */ }
    });
    stream.onerror = () => {
      stream.close();
      if (pollingTimer === null) pollingTimer = window.setInterval(() => api.jobs().then((result) => setJobs(result.jobs)).catch(() => {}), 2000);
    };
    return () => { stream.close(); window.clearInterval(readinessTimer); if (pollingTimer !== null) window.clearInterval(pollingTimer); };
  }, [refresh]);
  return <>
    <section className="card">
      <PanelTitle title="System readiness" subtitle="Core capabilities start independently, so a slow optional service never blocks the portal." action={<button className="small secondary" onClick={() => refresh()}>Refresh</button>} />
      <div className="readiness-grid">{Object.entries(readiness?.components ?? {}).map(([name, component]) => <div className="readiness-item" key={name}>
        <div><strong>{name.charAt(0).toUpperCase() + name.slice(1)}</strong><small>{component.message}</small>{component.progress_total ? <><progress max={component.progress_total} value={component.progress_current || 0} /><small>{formatBytes(component.progress_current || 0)} of {formatBytes(component.progress_total)}{component.eta_seconds ? ` · about ${Math.max(1, Math.ceil(component.eta_seconds / 60))} min remaining` : ""}</small></> : null}</div><StatePill state={component.state} />
      </div>)}</div>
    </section>
    <section className="card">
      <PanelTitle title="Activity" subtitle="Downloads, model changes, indexes, evaluations, backups, and maintenance continue safely if you leave this page." />
      {jobs.length === 0 ? <Empty>No background activity yet.</Empty> : <div className="activity-list">{jobs.map((job) => {
        const percent = job.progress_total && job.progress_total > 0 ? Math.min(100, Math.round(job.progress_current / job.progress_total * 100)) : null;
        return <article className="activity-item" key={job.id}>
          <div className="activity-head"><div><strong>{job.kind.replaceAll("-", " ")}</strong><small>{job.message || job.stage.replaceAll("_", " ")}</small></div><StatePill state={job.status === "running" ? job.stage : job.status} /></div>
          {(job.status === "running" || job.status === "queued") && <div className="progress-row"><progress max={job.progress_total || 1} value={job.progress_total ? job.progress_current : undefined} /><span>{job.progress_unit === "bytes" && job.progress_total ? `${formatBytes(job.progress_current)} / ${formatBytes(job.progress_total)}${job.progress_rate ? ` · ${formatBytes(job.progress_rate)}/s` : ""}${job.eta_seconds ? ` · ${Math.max(1, Math.ceil(job.eta_seconds / 60))} min` : ""}` : percent === null ? "In progress" : `${percent}%`}</span></div>}
          {job.error && <div className="operation-error"><strong>{job.error_code || "Operation failed"}</strong><span>{job.error}</span>{job.action && <span>{job.action}</span>}</div>}
          {canManage && <div className="actions">
            {(job.status === "queued" || job.status === "running") && <button className="small secondary" disabled={job.cancel_requested} onClick={() => run("Cancel operation", async () => { await api.cancelJob(job.id); await refresh(); })}>{job.cancel_requested ? "Canceling…" : "Cancel"}</button>}
            {(job.status === "failed" || job.status === "canceled") && <button className="small" onClick={() => run("Retry operation", async () => { await api.retryJob(job.id); await refresh(); })}>Retry</button>}
          </div>}
        </article>;
      })}</div>}
    </section>
  </>;
}

function Tools({ open }: { open: (path: string) => void }) {
  const [applications, setApplications] = useState<Application[]>([]);
  useEffect(() => { api.applications().then((result) => setApplications(result.applications)).catch(() => {}); }, []);
  const tools = applications.filter((application) => application.id !== "chat");
  return <section className="card">
    <PanelTitle title="Tools" subtitle="Every bundled tool stays behind the same SovereignStack sign-in and portal URL." />
    {tools.length === 0 ? <Empty>No additional tools are available for your role.</Empty> : <div className="tool-grid">{tools.map((tool) => <button className="tool-card" key={tool.id} onClick={() => open(tool.path)}>
      <span className="tool-mark">{tool.label.charAt(0)}</span><span><strong>{tool.label}</strong><small>{tool.description}</small></span><span aria-hidden="true">→</span>
    </button>)}</div>}
  </section>;
}

function NetworkAccess({ run }: { run: RunAction }) {
  const [status, setStatus] = useState<NetworkStatus | null>(null);
  const [mode, setMode] = useState<"desktop" | "lan" | "domain">("desktop");
  const [target, setTarget] = useState("");
  const [pendingURL, setPendingURL] = useState("");
  const refresh = useCallback(async () => {
    const next = await api.network();
    setStatus(next);
    if (next.access_mode) setMode(next.access_mode);
    if (next.access_mode === "lan") setTarget(next.bind_address);
    if (next.access_mode === "domain") setTarget(next.site_address);
  }, []);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);
  const url = status?.public_url || window.location.origin;
  const local = status?.access_mode === "desktop";
  async function save(event: FormEvent) {
    event.preventDefault();
    const ok = await run("Update network access", async () => { await api.setNetwork(mode, mode === "desktop" ? "" : target); });
    if (!ok) return;
    const current = new URL(status?.public_url || window.location.origin);
    const port = current.port || "8880";
    const next = mode === "domain" ? `https://${target}/` : mode === "lan" ? `http://${target}:${port}/` : `http://127.0.0.1:${port}/`;
    setPendingURL(next);
    const localBrowser = ["127.0.0.1", "localhost", "::1"].includes(window.location.hostname);
    if (mode !== "desktop" || localBrowser) window.setTimeout(() => window.location.assign(next), 2200);
  }
  return <div className="two-column">
    <section className="card"><PanelTitle title="Portal address" subtitle="Share one portal address—not individual service ports." />
      <div className="copy-row"><input readOnly value={url} aria-label="Portal address" /><button onClick={() => navigator.clipboard.writeText(url)}>Copy</button></div>
      <div className="privacy-list"><div><span>Current reachability</span><StatePill state={local ? "this computer" : "network"} /></div><div><span>TLS</span><StatePill state={window.location.protocol === "https:" ? "healthy" : "disabled"} /></div></div>
    </section>
    <section className="card"><PanelTitle title="Managed network access" subtitle="Desktop, private LAN, and domain publication use the signed host service rather than container privileges." />
      {status?.managed ? <form className="stack-form" onSubmit={save}>
        <label>Who can reach the portal<select value={mode} onChange={(event) => { setMode(event.target.value as typeof mode); setTarget(""); }}><option value="desktop">Only this computer</option><option value="lan">Devices on my private network</option><option value="domain">A domain with automatic HTTPS</option></select></label>
        {mode === "lan" && <label>Private IPv4 address<input required value={target} onChange={(event) => setTarget(event.target.value)} placeholder="192.168.1.20" /></label>}
        {mode === "domain" && <label>Domain name<input required value={target} onChange={(event) => setTarget(event.target.value.toLowerCase())} placeholder="ai.example.com" /></label>}
        <p className="notice">Changing this setting can move the portal to a new address. Copy the new address when it appears.</p>
        {pendingURL && <p className="notice" role="status">{mode === "desktop" && !["127.0.0.1", "localhost", "::1"].includes(window.location.hostname) ? <>Access is now host-only. Open <strong>{pendingURL}</strong> on that computer.</> : <>Opening <a href={pendingURL}>{pendingURL}</a>…</>}</p>}<button disabled={Boolean(pendingURL)}>Apply access mode</button>
      </form> : <p className="notice">This installation predates managed host controls. Upgrade the host package to enable browser-based access changes; the existing <code>sovereign access</code> commands remain supported.</p>}
    </section>
  </div>;
}

function Overview({ run, confirm }: { run: RunAction; confirm: ConfirmAction }) {
  const [status, setStatus] = useState<Status | null>(null);
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [errors, setErrors] = useState<RuntimeErrors | null>(null);
  const [supportBundles, setSupportBundles] = useState<SupportBundle[]>([]);

  const refresh = useCallback(async () => {
    const [nextStatus, nextManifest, nextErrors, support] = await Promise.all([
      api.status(), api.manifest().catch(() => null), api.runtimeErrors().catch(() => null), api.supportBundles().catch(() => ({ support_bundles: [] })),
    ]);
    setStatus(nextStatus); setManifest(nextManifest); setErrors(nextErrors); setSupportBundles(support.support_bundles);
  }, []);

  useEffect(() => {
    refresh().catch(() => {});
    const timer = window.setInterval(() => refresh().catch(() => {}), 5000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  const runtimeState = status?.runtime.state;
  return <>
    <div className="metric-grid">
      <div className="metric"><span>Runtime</span><strong>{runtimeState?.replaceAll("_", " ") ?? "Connecting"}</strong><StatePill state={runtimeState} /></div>
      <div className="metric"><span>Hardware profile</span><strong>{manifest?.profile ?? "—"}</strong><small>{manifest?.backend ?? "runtime unavailable"}</small></div>
      <div className="metric"><span>Gateway</span><strong>{status?.gateway.healthy ? "Ready" : "Unavailable"}</strong><StatePill state={status?.gateway.healthy ? "healthy" : "unreachable"} /></div>
      <div className="metric"><span>Required roles</span><strong>{Object.values(status?.runtime.required_roles ?? {}).filter(Boolean).length || "—"}</strong><small>enabled runtime roles</small></div>
    </div>

    <section className="card">
      <PanelTitle title="Runtime lifecycle" subtitle="A ready appliance completes every stage and keeps both required roles healthy."
        action={<div className="actions"><button className="secondary" onClick={async () => { if (await confirm({ title: "Repair portal services?", message: "Repair reconciles the signed configuration and restarts only services that are not healthy. Chat may pause briefly; appliance data is not removed.", confirmLabel: "Run repair" })) await run("Repair portal services", async () => { await api.repair(); await refresh(); }); }}>Repair</button><button className="secondary" onClick={async () => { if (await confirm({ title: "Restart the generation runtime?", message: "Active generation requests will stop and Chat will be unavailable while the current model reloads. Configuration and data are unchanged.", confirmLabel: "Restart runtime" })) await run("Restart runtime", async () => { await api.restartRuntime(); }); }}>Restart runtime</button></div>} />
      <ol className="statemachine">
        {RUNTIME_STATES.map((state) => <li key={state} className={state === runtimeState ? "current" : ""}>{state.replaceAll("_", " ")}</li>)}
      </ol>
      {manifest && <div className="table-wrap"><table>
        <thead><tr><th>Role</th><th>Status</th><th>Stable alias</th><th>Engine model</th><th>Capabilities</th></tr></thead>
        <tbody>{Object.entries(manifest.roles).map(([name, role]) => <tr key={name}>
          <td className="strong">{name}</td><td><StatePill state={role.enabled ? role.status : "disabled"} /></td>
          <td>{role.served_model_name ?? "—"}</td><td className="mono clip">{role.engine_model ?? "—"}</td>
          <td>{role.dimensions ? `${role.dimensions} dimensions` : role.modalities?.join(", ") ?? "—"}</td>
        </tr>)}</tbody>
      </table></div>}
      {errors && errors.errors.length > 0 && <div className="error-box">{errors.errors.map((error) =>
        <p key={error.code + (error.role ?? "")}><strong>{error.code}</strong>{error.role ? ` · ${error.role}` : ""}<br />{error.message}</p>)}</div>}
    </section>

    <section className="card">
      <PanelTitle title="Appliance services" subtitle="Only the unified portal ingress is published on the host." />
      <div className="service-grid">
        <div><StatePill state={status?.gateway.healthy ? "healthy" : "unreachable"} /><span>Gateway</span></div>
        <div><StatePill state={status?.docker_proxy.reachable ? "healthy" : "unreachable"} /><span>Restricted Docker proxy</span></div>
        {Object.entries(status?.services ?? {}).map(([service, state]) => <div key={service}><StatePill state={state} /><span>{service.replace(/^sovereign-/, "")}</span></div>)}
      </div>
    </section>
    <section className="card">
      <PanelTitle title="Diagnostics" subtitle="Support bundles exclude prompts and secrets and redact environment values by allowlist."
        action={<button className="secondary" onClick={() => run("Create support bundle", async () => { await completeJob(await api.createSupportBundle()); await refresh(); })}>Create support bundle</button>} />
      {supportBundles.length === 0 ? <Empty>No support bundles created.</Empty> : <div className="compact-list">{supportBundles.map((bundle) => <div key={bundle.id}><div><strong>{bundle.created_at}</strong><small>{formatBytes(bundle.bytes)} · SHA-256 {bundle.sha256.slice(0, 12)}…</small></div><a className="button small secondary" href={api.supportBundleDownloadURL(bundle.id)}>Download</a></div>)}</div>}
    </section>
  </>;
}

function Models({ run, confirm }: { run: RunAction; confirm: ConfirmAction }) {
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [catalog, setCatalog] = useState<CatalogModel[]>([]);
  const [profile, setProfile] = useState("");
  const [credentials, setCredentials] = useState<CredentialMetadata[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ id: "", role: "generation", source: "cloud", provider: "openai", model: "", revision: "", artifact: "", sha256: "", base_url: "", credential_id: "" });
  const refresh = useCallback(async () => {
    const [modelResult, credentialResult, catalogResult] = await Promise.all([api.models(), api.credentials().catch(() => ({ credentials: [] })), api.modelCatalog()]);
    setModels(modelResult.models); setCredentials(credentialResult.credentials);
    setCatalog(catalogResult.models); setProfile(catalogResult.profile);
  }, []);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    const entry: ModelEntry = {
      id: form.id, role: form.role, source: form.source, provider: form.provider,
      model: form.model, capabilities: form.role === "embedding" ? ["text"] : ["chat", "completions"],
    };
    if (form.revision) entry.revision = form.revision;
    if (form.artifact) entry.artifact = form.artifact;
    if (form.sha256) entry.sha256 = form.sha256;
    if (form.base_url) entry.base_url = form.base_url;
    if (form.credential_id) entry.credential_id = form.credential_id;
    if (await run("Add model", () => api.createModel(entry))) {
      setShowForm(false); setForm({ ...form, id: "", model: "", revision: "", artifact: "", sha256: "", base_url: "" }); await refresh();
    }
  }

  return <>
    <section className="card">
      <PanelTitle title="Recommended models" subtitle={`Reviewed for ${profile || "this appliance"}. Technical registry pins are managed automatically.`} />
      <div className="catalog-grid">{catalog.map((item) => <article className={`catalog-card ${item.recommended ? "recommended" : ""}`} key={item.id}>
        <div className="row"><span className="catalog-role">{item.role}</span>{item.recommended && <span className="recommendation">Recommended</span>}</div>
        <h3>{item.display_name}</h3><p>{item.description}</p>
        <div className="catalog-meta"><span>{formatBytes(item.download_bytes)}</span><span>{item.capabilities.join(" · ")}</span></div>
        {!item.compatible ? <div className="compatibility-error">{item.compatibility_reason || `Not compatible with ${profile}`}</div> : <button disabled={item.role === "embedding" && item.registered} onClick={() => run(`${item.registered ? "Start" : "Install"} ${item.display_name}`, async () => { await api.installCatalogModel(item.id); await refresh(); })}>{item.role === "embedding" && item.registered ? "Built in" : item.registered ? "Use this model" : "Install"}</button>}
      </article>)}</div>
    </section>
    <section className="card">
      <PanelTitle title="Installed and custom models" subtitle="Add a custom provider or pinned model only when the recommended catalog is not enough."
        action={<button className="secondary" onClick={() => setShowForm(!showForm)}>{showForm ? "Close advanced" : "Add custom model"}</button>} />
      {showForm && <form className="form-grid inset" onSubmit={submit}>
        <label>Product ID<input required value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} placeholder="team-coding-model" /></label>
        <label>Role<select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}><option value="generation">Generation</option><option value="embedding">Embedding</option></select></label>
        <label>Source<select value={form.source} onChange={(e) => setForm({ ...form, source: e.target.value })}><option value="cloud">Cloud</option><option value="remote">OpenAI-compatible remote</option><option value="huggingface">Hugging Face</option><option value="modelscope">ModelScope</option><option value="local">Managed local path</option><option value="offline">Offline bundle artifact</option></select></label>
        <label>Provider<select value={form.provider} onChange={(e) => setForm({ ...form, provider: e.target.value })}><option value="openai">OpenAI</option><option value="anthropic">Anthropic</option><option value="gemini">Gemini</option><option value="openai-compatible">OpenAI compatible</option><option value="custom">Custom</option></select></label>
        <label className="wide">Model or repository<input required value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} placeholder="gpt-5-mini or org/model" /></label>
        <label>Immutable revision<input value={form.revision} onChange={(e) => setForm({ ...form, revision: e.target.value })} placeholder="40-character commit" /></label>
        {(form.source === "local" || form.source === "offline") && <><label>Managed artifact<input required={form.role === "embedding"} value={form.artifact} onChange={(e) => setForm({ ...form, artifact: e.target.value })} placeholder="metal/embedding-model.gguf" /></label><label>SHA-256<input required={form.role === "embedding"} pattern="[a-fA-F0-9]{64}" value={form.sha256} onChange={(e) => setForm({ ...form, sha256: e.target.value.toLowerCase() })} /></label></>}
        <label>Credential<select value={form.credential_id} onChange={(e) => setForm({ ...form, credential_id: e.target.value })}><option value="">None</option>{credentials.map((credential) => <option key={credential.id} value={credential.id}>{credential.label}</option>)}</select></label>
        <label className="wide">Remote base URL<input value={form.base_url} onChange={(e) => setForm({ ...form, base_url: e.target.value })} placeholder="https://host.example/v1" /></label>
        <div className="wide form-actions"><button type="submit">Save model</button></div>
      </form>}
      {models.length === 0 ? <Empty>No models are registered.</Empty> : <div className="table-wrap"><table>
        <thead><tr><th>Model</th><th>Role</th><th>Source</th><th>Validation</th><th>Runtime</th><th></th></tr></thead>
        <tbody>{models.map((model) => <tr key={model.id}>
          <td><span className="strong">{model.id}</span><small className="block mono clip">{model.model}</small></td>
          <td>{model.role}</td><td>{model.provider || model.source}</td><td><StatePill state={model.validation_state || "unvalidated"} /></td>
          <td><StatePill state={model.loaded ? "healthy" : "inactive"} /></td>
          <td><div className="actions">
            {model.role !== "embedding" && <button className="small secondary" onClick={() => run(`Load ${model.id}`, async () => { await completeJob(await api.loadModel(model.id)); await refresh(); })}>Load</button>}
            <button className="small secondary" onClick={() => run(`Smoke-test ${model.id}`, async () => { await completeJob(await api.smokeModel(model.id)); })}>Test</button>
            <button className="small danger" onClick={async () => { if (await confirm({ title: `Delete ${model.id}?`, message: "This removes the registry entry. Existing downloaded files are retained.", confirmLabel: "Delete model", danger: true })) await run(`Delete ${model.id}`, async () => { await api.deleteModel(model.id); await refresh(); }); }}>Delete</button>
          </div></td>
        </tr>)}</tbody>
      </table></div>}
    </section>
  </>;
}

function Knowledge({ run }: { run: RunAction }) {
  const [profiles, setProfiles] = useState<Record<string, EmbeddingProfile>>({});
  const [embeddingModels, setEmbeddingModels] = useState<ModelEntry[]>([]);
  const [indexes, setIndexes] = useState<IndexVersion[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [validation, setValidation] = useState("");
  const [activeProfile, setActiveProfile] = useState("");
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ id: "", provider: "openai-compatible", model_entry_id: "", model: "", revision: "", served_model_name: "", pooling: "mean", normalization: "l2", query_prefix: "", document_prefix: "" });
  const refresh = useCallback(async () => {
    const [profileResult, indexResult, workspaceResult, state, modelResult] = await Promise.all([
      api.embeddingProfiles(), api.indexes(), api.workspaces().catch(() => ({ workspaces: [] })), api.embeddingState().catch(() => ({ active: null })), api.models(),
    ]);
    setProfiles(profileResult.embedding_profiles); setIndexes(indexResult.indexes); setWorkspaces(workspaceResult.workspaces);
    setEmbeddingModels(modelResult.models.filter((model) => model.role === "embedding"));
    setActiveProfile(state.active?.profile_id ?? "");
  }, []);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);

  async function createProfile(event: FormEvent) {
    event.preventDefault();
    const isGemma = form.provider === "embeddinggemma";
    const profile: EmbeddingProfile = {
      provider: form.provider, source: isGemma ? "huggingface" : form.provider === "sovereign-runtime" ? "local" : "remote",
      model: isGemma ? "ggml-org/embeddinggemma-300M-qat-q4_0-GGUF" : form.model,
      revision: isGemma ? "8dd0ca2a66a8f14470acb0e2a71f801afbc5fb73" : form.revision,
      served_model_name: isGemma ? "embedding-gemma-default" : form.served_model_name,
      model_entry_id: isGemma ? undefined : form.model_entry_id,
      pooling: form.pooling, normalization: form.normalization, distance_metric: "cosine",
      query_prefix: form.query_prefix, document_prefix: form.document_prefix,
      chunking_strategy: "recursive-v1", preprocessing_version: "sovereign-embed-v1", modalities: ["text"],
    };
    if (await run("Save embedding profile", () => api.createEmbeddingProfile(form.id, profile))) {
      setShowForm(false); setForm({ ...form, id: "", model_entry_id: "", model: "", revision: "", served_model_name: "" }); await refresh();
    }
  }

  const compatibleModels = embeddingModels.filter((model) => {
    const remote = model.source === "remote" || model.source === "cloud";
    return form.provider === "openai-compatible" ? remote : !remote;
  });

  return <>
    <section className="card">
      <PanelTitle title="Embedding provider" subtitle="One provider is active across the appliance. Activation rebuilds every workspace and switches all indexes together."
        action={<button onClick={() => setShowForm(!showForm)}>{showForm ? "Cancel" : "Add provider"}</button>} />
      {showForm && <form className="form-grid inset" onSubmit={createProfile}>
        <label>Profile ID<input required value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} placeholder="research-embeddings" /></label>
        <label>Provider<select value={form.provider} onChange={(e) => setForm({ ...form, provider: e.target.value, model_entry_id: "", model: "", revision: "" })}><option value="embeddinggemma">Built-in EmbeddingGemma</option><option value="sovereign-runtime">Custom local model</option><option value="openai-compatible">OpenAI-compatible service</option></select></label>
        {form.provider !== "embeddinggemma" && <><label>Model registry entry<select required value={form.model_entry_id} onChange={(e) => { const entry = embeddingModels.find((model) => model.id === e.target.value); setForm({ ...form, model_entry_id: e.target.value, model: entry?.model ?? "", revision: entry?.revision ?? "" }); }}><option value="">Choose an embedding model</option>{compatibleModels.map((model) => <option key={model.id} value={model.id}>{model.id} · {model.source}</option>)}</select></label>
          <label>Stable alias<input required value={form.served_model_name} onChange={(e) => setForm({ ...form, served_model_name: e.target.value })} placeholder="embedding-special-default" /></label>
          <label className="wide">Model or repository<input required readOnly value={form.model} /></label>
          <label>Immutable revision<input required readOnly value={form.revision} /></label>
          <label>Pooling<select value={form.pooling} onChange={(e) => setForm({ ...form, pooling: e.target.value })}><option value="mean">Mean</option><option value="last">Last token</option><option value="cls">CLS token</option></select></label>
          <label>Normalization<select value={form.normalization} onChange={(e) => setForm({ ...form, normalization: e.target.value })}><option value="l2">L2</option><option value="none">None</option></select></label></>}
        <label>Query prefix<input value={form.query_prefix} onChange={(e) => setForm({ ...form, query_prefix: e.target.value })} /></label>
        <label>Document prefix<input value={form.document_prefix} onChange={(e) => setForm({ ...form, document_prefix: e.target.value })} /></label>
        <div className="wide form-actions"><button>Save provider</button></div>
      </form>}
      <div className="profile-grid">{Object.entries(profiles).map(([id, profile]) => <article className="profile" key={id}>
        <div className="row"><h3>{id}</h3><StatePill state={id === activeProfile ? "active" : profile.provider} /></div>
        <p className="mono clip">{profile.model}</p><p>{profile.provider} · {profile.modalities.join(" · ")} · {profile.distance_metric} · {profile.normalization || "none"}</p>
        <div className="actions"><button className="small secondary" onClick={() => run(`Validate ${id}`, async () => { const result = await api.validateProfile(id); setValidation(JSON.stringify(result)); })}>Validate</button>
          <button className="small" disabled={id === activeProfile} onClick={() => run(`Activate ${id}`, async () => { await completeJob(await api.activateProfile(id)); await refresh(); })}>{id === activeProfile ? "Active" : "Activate everywhere"}</button></div>
      </article>)}</div>
      {validation && <p className="notice">Validation: {validation}</p>}
    </section>

    <section className="card">
      <PanelTitle title="Workspace indexes" subtitle={`Every workspace uses ${activeProfile || "the appliance provider"}. Change providers above to rebuild and switch the entire appliance atomically.`} />
      {workspaces.length > 0 && activeProfile && <div className="inset rebuild-row">
        <div><span className="label-text">Initialize or refresh a workspace</span><div className="actions">{workspaces.map((workspace) => <button key={workspace.id} className="secondary" onClick={() => run(`Rebuild ${workspace.name}`, async () => { await completeJob(await api.startIndex(workspace, activeProfile)); await refresh(); })}>{workspace.name}</button>)}</div></div>
      </div>}
      {indexes.length === 0 ? <Empty>Create a workspace in the main app, then start its first versioned index here.</Empty> : <div className="table-wrap"><table>
        <thead><tr><th>Workspace</th><th>Profile</th><th>Status</th><th>Progress</th><th>Vectors</th><th></th></tr></thead>
        <tbody>{indexes.map((index) => <tr key={index.id}>
          <td><span className="strong">{index.provider_slug}</span><small className="block mono">{index.id.slice(0, 8)}</small></td>
          <td>{index.profile_id}<small className="block">{index.dimensions ? `${index.dimensions} dimensions` : "discovering dimensions"}</small></td>
          <td><StatePill state={index.status} />{index.error && <small className="block error">{index.error}</small>}</td>
          <td>{index.processed_documents}/{index.document_count}</td><td>{index.vector_count.toLocaleString()}</td>
          <td><div className="actions">{index.profile_id === activeProfile && <button className="small secondary" onClick={() => run(`Rebuild ${index.provider_slug}`, async () => { await completeJob(await api.rebuildIndex(index.id, activeProfile)); await refresh(); })}>Rebuild</button>}</div></td>
        </tr>)}</tbody>
      </table></div>}
    </section>
  </>;
}

function Evaluations({ run }: { run: RunAction }) {
  const suites = [
    ["smoke", "Release health"], ["quick", "Quick benchmark"], ["embedding", "Portable embedding"],
    ["retrieval", "Retrieval quality"], ["mixed-role", "Mixed-role pressure"], ["full", "Full benchmark"],
  ];
  const [results, setResults] = useState<string[]>([]);
  const [selected, setSelected] = useState<EvalReport | null>(null);
  const refresh = useCallback(async () => setResults((await api.evalResults()).results), []);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);
  return <>
    <section className="card"><PanelTitle title="Evaluation suites" subtitle="Functional release gates and benchmarks run through the same gateway used by the workspace." />
      <div className="suite-grid">{suites.map(([id, label]) => <button className="suite" key={id} onClick={() => run(`Run ${label}`, async () => { await completeJob(await api.runEval(id)); await refresh(); })}><strong>{label}</strong><span>{id}</span></button>)}</div>
    </section>
    <section className="card"><PanelTitle title="Reports" subtitle="JSON and HTML reports persist under the appliance data directory." action={<button className="secondary small" onClick={() => refresh()}>Refresh</button>} />
      {results.length === 0 ? <Empty>No evaluation reports yet.</Empty> : <div className="split-list"><div>{results.map((result) => <button className="list-button" key={result} onClick={async () => setSelected(await api.evalResult(result))}>{result}</button>)}</div>
        <pre>{selected ? JSON.stringify(selected, null, 2) : "Select a report to inspect its results."}</pre></div>}
    </section>
  </>;
}

function Access({ run, confirm }: { run: RunAction; confirm: ConfirmAction }) {
  const [credentials, setCredentials] = useState<CredentialMetadata[]>([]);
  const [credentialForm, setCredentialForm] = useState({ id: "", provider: "openai", label: "", secret: "" });
  const [keys, setKeys] = useState<Record<string, unknown>>({});
  const [keyForm, setKeyForm] = useState({ alias: "", models: "assistant-large", budget: "", rpm: "", tpm: "" });
  const [issuedKey, setIssuedKey] = useState<Record<string, unknown> | null>(null);
  const refresh = useCallback(async () => {
    const [credentialResult, keyResult] = await Promise.all([api.credentials(), api.gatewayKeys().catch(() => ({}))]);
    setCredentials(credentialResult.credentials); setKeys(keyResult);
  }, []);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);

  async function saveCredential(event: FormEvent) {
    event.preventDefault();
    if (await run("Save encrypted credential", () => api.createCredential(credentialForm))) {
      setCredentialForm({ ...credentialForm, id: "", label: "", secret: "" }); await refresh();
    }
  }
  async function createKey(event: FormEvent) {
    event.preventDefault();
    const request: { key_alias: string; models?: string[]; max_budget?: number; rpm_limit?: number; tpm_limit?: number } = {
      key_alias: keyForm.alias, models: keyForm.models.split(",").map((item) => item.trim()).filter(Boolean),
    };
    if (keyForm.budget) request.max_budget = Number(keyForm.budget);
    if (keyForm.rpm) request.rpm_limit = Number(keyForm.rpm);
    if (keyForm.tpm) request.tpm_limit = Number(keyForm.tpm);
    let result: Record<string, unknown> | null = null;
    if (await run("Issue gateway key", async () => { result = await api.createGatewayKey(request); })) {
      setIssuedKey(result); setKeyForm({ ...keyForm, alias: "" }); await refresh();
    }
  }

  return <div className="two-column">
    <section className="card"><PanelTitle title="Provider credentials" subtitle="Secrets are AES-256-GCM encrypted and never returned after submission." />
      <form className="stack-form inset" onSubmit={saveCredential}>
        <label>Credential ID<input required value={credentialForm.id} onChange={(e) => setCredentialForm({ ...credentialForm, id: e.target.value })} placeholder="openai-office" /></label>
        <label>Provider<select value={credentialForm.provider} onChange={(e) => setCredentialForm({ ...credentialForm, provider: e.target.value })}><option>openai</option><option>anthropic</option><option>gemini</option><option>custom</option></select></label>
        <label>Display label<input required value={credentialForm.label} onChange={(e) => setCredentialForm({ ...credentialForm, label: e.target.value })} /></label>
        <label>Secret<input required type="password" autoComplete="off" value={credentialForm.secret} onChange={(e) => setCredentialForm({ ...credentialForm, secret: e.target.value })} /></label>
        <button type="submit">Encrypt and save</button>
      </form>
      <div className="compact-list">{credentials.map((credential) => <div key={credential.id}><div><strong>{credential.label}</strong><small>{credential.provider} · {credential.id}</small></div><button className="small danger" onClick={async () => { if (await confirm({ title: `Delete ${credential.label}?`, message: "Models using this credential will stop working until another credential is selected.", confirmLabel: "Delete credential", danger: true })) await run("Delete credential", async () => { await api.deleteCredential(credential.id); await refresh(); }); }}>Delete</button></div>)}</div>
    </section>
    <section className="card"><PanelTitle title="Gateway keys" subtitle="Issue scoped keys with model, spend, and request limits." />
      <form className="stack-form inset" onSubmit={createKey}>
        <label>Key alias<input required value={keyForm.alias} onChange={(e) => setKeyForm({ ...keyForm, alias: e.target.value })} /></label>
        <label>Allowed models<input value={keyForm.models} onChange={(e) => setKeyForm({ ...keyForm, models: e.target.value })} /></label>
        <div className="form-grid"><label>Max budget<input type="number" min="0" step="0.01" value={keyForm.budget} onChange={(e) => setKeyForm({ ...keyForm, budget: e.target.value })} /></label><label>RPM<input type="number" min="1" value={keyForm.rpm} onChange={(e) => setKeyForm({ ...keyForm, rpm: e.target.value })} /></label><label>TPM<input type="number" min="1" value={keyForm.tpm} onChange={(e) => setKeyForm({ ...keyForm, tpm: e.target.value })} /></label></div>
        <button type="submit">Issue key</button>
      </form>
      {issuedKey && <div className="secret-once"><strong>Copy this response now</strong><pre>{JSON.stringify(issuedKey, null, 2)}</pre></div>}
      <details><summary>Current key metadata</summary><pre>{JSON.stringify(keys, null, 2)}</pre></details>
    </section>
  </div>;
}

function Resilience({ run, confirm }: { run: RunAction; confirm: ConfirmAction }) {
  const [backups, setBackups] = useState<BackupManifest[]>([]);
  const [bundles, setBundles] = useState<BundleManifest[]>([]);
  const [profile, setProfile] = useState("");
  const [includeWeights, setIncludeWeights] = useState(true);
  const refresh = useCallback(async () => {
    const [backupResult, bundleResult, manifest] = await Promise.all([api.backups(), api.bundles(), api.manifest().catch(() => null)]);
    setBackups(backupResult.backups); setBundles(bundleResult.bundles); setProfile(manifest?.profile ?? "");
  }, []);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);

  return <div className="two-column">
    <section className="card"><PanelTitle title="Backups" subtitle="Databases and product config; model weights and secrets are excluded."
      action={<button onClick={() => run("Create backup", async () => { await completeJob(await api.createBackup()); await refresh(); })}>Create backup</button>} />
      {backups.length === 0 ? <Empty>No completed backups.</Empty> : <div className="compact-list">{backups.map((backup) => <div key={backup.id}><div><strong>{backup.id}</strong><small>{backup.files.length} files · {backup.created_at}</small></div><div className="actions"><button className="small secondary" onClick={() => run("Verify backup", async () => { const result = await api.verifyBackup(backup.id); if (!result.valid) throw new ApiError(422, result.problems.join(", ")); })}>Verify</button><button className="small danger" onClick={async () => { if (await confirm({ title: "Restore this backup?", message: "Restore verifies this backup, creates a fresh rollback point, then replaces live databases and configuration. Chat pauses temporarily; a failed restore automatically reapplies the rollback point.", confirmLabel: "Restore backup", danger: true })) await run("Restore backup", async () => { await completeJob(await api.restoreBackup(backup.id)); }); }}>Restore</button></div></div>)}</div>}
    </section>
    <section className="card"><PanelTitle title="Offline bundles" subtitle={`Same-platform distribution for ${profile || "the installed profile"}.`} />
      <div className="inset bundle-create"><label className="check"><input type="checkbox" checked={includeWeights} onChange={(e) => setIncludeWeights(e.target.checked)} /> Include the complete local model cache</label><p>Bundles always contain pinned application and service images{profile === "metal-arm64" ? " plus the signed Metal agent" : ""}.</p><button onClick={() => run("Create offline bundle", async () => { await completeJob(await api.createBundle(profile, includeWeights ? ["all"] : []), 7_200_000); await refresh(); })}>Create bundle</button></div>
      {bundles.length === 0 ? <Empty>No completed bundles.</Empty> : <div className="compact-list">{bundles.map((bundle) => <div key={bundle.bundle_id}><div><strong>{bundle.bundle_id}</strong><small>{bundle.profile} · {bundle.includes_weights ? "weights included" : "images only"} · {bundle.files.reduce((sum, file) => sum + file.bytes, 0) ? formatBytes(bundle.files.reduce((sum, file) => sum + file.bytes, 0)) : "size pending"}</small></div><a className="button small secondary" href={api.bundleDownloadURL(bundle.bundle_id)}>Download</a></div>)}</div>}
    </section>
  </div>;
}

function Settings({ run }: { run: RunAction }) {
  const [branding, setBranding] = useState<Branding | null>(null);
  const [features, setFeatures] = useState<Features | null>(null);
  useEffect(() => { Promise.all([api.branding(), api.features()]).then(([b, f]) => { setBranding(b); setFeatures(f); }).catch(() => {}); }, []);
  if (!branding) return <section className="card"><Empty>Loading product settings…</Empty></section>;
  return <div className="two-column">
    <section className="card"><PanelTitle title="Branding" subtitle="Applies to the workspace title and product assets." />
      <form className="stack-form" onSubmit={(event) => { event.preventDefault(); run("Apply branding", async () => { await api.saveBranding(branding); applyTheme(branding); await api.applyBranding(); }); }}>
        <label>Product name<input value={branding.product_name} onChange={(e) => setBranding({ ...branding, product_name: e.target.value })} /></label>
        <label>Company name<input value={branding.company_name} onChange={(e) => setBranding({ ...branding, company_name: e.target.value })} /></label>
        <div className="form-grid"><label>Primary color<input type="color" value={branding.colors.primary} onChange={(e) => setBranding({ ...branding, colors: { ...branding.colors, primary: e.target.value } })} /></label><label>Accent color<input type="color" value={branding.colors.accent} onChange={(e) => setBranding({ ...branding, colors: { ...branding.colors, accent: e.target.value } })} /></label></div>
        <button type="submit">Save and apply</button>
      </form>
    </section>
    <section className="card"><PanelTitle title="Privacy posture" subtitle="Content capture remains off by default." />
      <div className="privacy-list"><div><span>Metadata tracing</span><StatePill state={features?.tracing.metadata_only ? "healthy" : "disabled"} /></div><div><span>Prompt logging</span><StatePill state={features?.prompt_logging.enabled ? "enabled" : "disabled"} /></div><div><span>Response logging</span><StatePill state={features?.tracing.response_logging ? "enabled" : "disabled"} /></div><div><span>Phoenix</span><StatePill state={features?.phoenix.enabled ? "healthy" : "disabled"} /></div></div>
      <p className="notice">v0.1 exposes metadata-only tracing. Enabling content capture requires explicit redaction, scope, retention, and consent configuration.</p>
    </section>
  </div>;
}

function Updates({ run }: { run: RunAction }) {
  const [updates, setUpdates] = useState<UpdateInfo | null>(null);
  const [showWhatsNew, setShowWhatsNew] = useState(false);
  const refresh = useCallback(() => api.updates().then((value) => {
    setUpdates(value);
    const version = value.release.latest_version || value.release.current_version;
    setShowWhatsNew(Boolean(value.release.release_url && version && localStorage.getItem(`sovereign-whats-new-${version}`) !== "seen"));
  }).catch(() => setUpdates(null)), []);
  useEffect(() => {
    refresh();
    const timer = window.setInterval(refresh, 5000);
    return () => window.clearInterval(timer);
  }, [refresh]);
  const dismissWhatsNew = () => {
    const version = updates?.release.latest_version || updates?.release.current_version;
    if (version) localStorage.setItem(`sovereign-whats-new-${version}`, "seen");
    setShowWhatsNew(false);
  };
  return <section className="card update-card"><PanelTitle title="Product updates" subtitle="Release archives are signature-verified. SovereignStack creates a backup first and restores the previous release if validation fails." action={<button className="small secondary" onClick={refresh}>Check again</button>} />
    {showWhatsNew && updates?.release.release_url && <div className="whats-new"><div><strong>What's new in {updates.release.latest_version || updates.release.current_version}</strong><p>Review the release notes before updating this appliance.</p></div><div className="actions"><a className="button small" href={updates.release.release_url} target="_blank" rel="noreferrer">View release notes</a><button className="small secondary" onClick={dismissWhatsNew}>Dismiss</button></div></div>}
    <div className="privacy-list"><div><span>Installed version</span><strong>{updates?.release.current_version || "—"}</strong></div><div><span>Latest release</span><strong>{updates?.release.latest_version || "Unavailable"}</strong></div><div><span>Update state</span><StatePill state={updates?.operation.state || "idle"} /></div></div>
    {updates?.release.check_error && <p className="notice">{updates.release.check_error}</p>}
    {updates?.operation.message && updates.operation.state !== "idle" && <p className="notice">{updates.operation.message}</p>}
    {!updates?.release.available && !updates?.release.check_error && <p className="notice">This appliance is up to date.</p>}
    <div className="actions update-actions">{updates?.release.release_url && <a className="button secondary" href={updates.release.release_url} target="_blank" rel="noreferrer">What's new</a>}{updates?.release.available && updates.release.latest_version && <button onClick={() => run(`Schedule update ${updates.release.latest_version}`, async () => { await api.applyUpdate(updates.release.latest_version!); window.setTimeout(refresh, 2500); })}>Update to {updates.release.latest_version}</button>}</div>
  </section>;
}

function EmbeddedApp({ title, src }: { title: string; src: string }) {
  return <section className="embedded-app"><iframe title={title} src={src} allow="clipboard-read; clipboard-write" /></section>;
}

function People({ run }: { run: RunAction }) {
  const [users, setUsers] = useState<Identity[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [form, setForm] = useState<{ role: Identity["role"]; workspace_ids: string[] }>({ role: "member", workspace_ids: [] });
  const [inviteURL, setInviteURL] = useState("");
  const refresh = useCallback(async () => {
    const [people, workspaceResult] = await Promise.all([api.users(), api.workspaces().catch(() => ({ workspaces: [] }))]);
    setUsers(people.users); setWorkspaces(workspaceResult.workspaces);
  }, []);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);

  async function invite(event: FormEvent) {
    event.preventDefault();
    let result: Awaited<ReturnType<typeof api.createInvitation>> | null = null;
    if (await run("Create invitation", async () => { result = await api.createInvitation(form); })) {
      setInviteURL(`${window.location.origin}${result!.path}`);
    }
  }
  async function save(user: Identity) {
    await run(`Update ${user.username}`, async () => {
      const updated = await api.updateUser(user.id, { role: user.role, disabled: user.disabled, workspace_ids: user.workspace_ids ?? [] });
      setUsers((items) => items.map((item) => item.id === updated.id ? updated : item));
    });
  }
  const updateLocal = (id: number, patch: Partial<Identity>) => setUsers((items) => items.map((item) => item.id === id ? { ...item, ...patch } : item));

  return <>
    <section className="card"><PanelTitle title="Invite a person" subtitle="Links expire after 24 hours and can be used once." />
      <form className="form-grid" onSubmit={invite}>
        <label>Role<select value={form.role} onChange={(event) => setForm({ ...form, role: event.target.value as Identity["role"] })}><option value="member">Member</option><option value="manager">Manager</option><option value="admin">Administrator</option></select></label>
        <div><span className="label-text">Workspace access</span><div className="workspace-checks">{workspaces.map((workspace) => <label className="check" key={workspace.id}><input type="checkbox" checked={form.workspace_ids.includes(workspace.id)} onChange={(event) => setForm({ ...form, workspace_ids: event.target.checked ? [...form.workspace_ids, workspace.id] : form.workspace_ids.filter((id) => id !== workspace.id) })} />{workspace.name}</label>)}</div></div>
        <div className="wide form-actions"><button>Create invite link</button></div>
      </form>
      {inviteURL && <div className="secret-once"><strong>Copy this invite link</strong><div className="copy-row"><input readOnly value={inviteURL} /><button onClick={() => navigator.clipboard.writeText(inviteURL)}>Copy</button></div></div>}
    </section>
    <section className="card"><PanelTitle title="People" subtitle="Control is the identity authority; changes synchronize at the next chat launch." />
      <div className="table-wrap"><table><thead><tr><th>User</th><th>Role</th><th>Workspace access</th><th>Status</th><th /></tr></thead><tbody>{users.map((user) => <tr key={user.id}>
        <td><span className="strong">{user.display_name || user.username}</span><small className="block">{user.username}</small></td>
        <td><select value={user.role} onChange={(event) => updateLocal(user.id, { role: event.target.value as Identity["role"] })}><option value="member">Member</option><option value="manager">Manager</option><option value="admin">Administrator</option></select></td>
        <td><div className="workspace-checks compact">{workspaces.map((workspace) => <label className="check" key={workspace.id}><input type="checkbox" checked={(user.workspace_ids ?? []).includes(workspace.id)} onChange={(event) => updateLocal(user.id, { workspace_ids: event.target.checked ? [...(user.workspace_ids ?? []), workspace.id] : (user.workspace_ids ?? []).filter((id) => id !== workspace.id) })} />{workspace.name}</label>)}</div></td>
        <td><label className="check"><input type="checkbox" checked={!user.disabled} onChange={(event) => updateLocal(user.id, { disabled: !event.target.checked })} />Active</label></td>
        <td><button className="small secondary" onClick={() => save(user)}>Save</button></td>
      </tr>)}</tbody></table></div>
    </section>
  </>;
}

export function Dashboard({ identity, onLogout }: { identity: Identity; onLogout: () => void }) {
  const available = NAV.filter((item) => ROLE_LEVEL[identity.role] >= ROLE_LEVEL[item.minimum]);
  const pageForPath = () => {
    const direct = available.find((item) => item.path === window.location.pathname)?.page;
    const legacy = LEGACY_PATHS[window.location.pathname];
    if (direct) return direct;
    if (legacy && (legacy === "Chat" || ROLE_LEVEL[identity.role] >= ROLE_LEVEL.manager)) return legacy;
    return "Chat";
  };
  const [page, setPage] = useState<PortalPage>(pageForPath);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<{ tone: "ok" | "bad"; text: string } | null>(null);
  const [readiness, setReadiness] = useState<Readiness | null>(null);
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem("sovereign-sidebar-collapsed") === "true");
  const [mobileOpen, setMobileOpen] = useState(false);
  const [appearance, setAppearance] = useState<"system" | "dark" | "light">(() => (localStorage.getItem("sovereign-appearance") as "system" | "dark" | "light") || "system");
  const [confirmation, setConfirmation] = useState<({ title: string; message: string; confirmLabel?: string; danger?: boolean; resolve: (value: boolean) => void }) | null>(null);
  const confirm: ConfirmAction = (options) => new Promise((resolve) => setConfirmation({ ...options, resolve }));
  const run: RunAction = async (label, action) => {
    setBusy(label); setMessage(null);
    try { await action(); setMessage({ tone: "ok", text: `${label} completed.` }); return true; }
    catch (error) {
      const detail = error instanceof ApiError && error.action ? `${error.message} ${error.action}` : error instanceof Error ? error.message : String(error);
      setMessage({ tone: "bad", text: detail }); return false;
    }
    finally { setBusy(""); }
  };

  useEffect(() => {
    const onPopState = () => setPage(pageForPath());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, [available]);
  useEffect(() => {
    const refresh = () => api.readiness().then(setReadiness).catch(() => setReadiness(null));
    refresh();
    const timer = window.setInterval(refresh, 5000);
    return () => window.clearInterval(timer);
  }, []);
  useEffect(() => {
    if (appearance === "system") document.documentElement.removeAttribute("data-appearance");
    else document.documentElement.dataset.appearance = appearance;
    localStorage.setItem("sovereign-appearance", appearance);
  }, [appearance]);
  useEffect(() => {
    if (!confirmation) return;
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") { confirmation.resolve(false); setConfirmation(null); }
      if (event.key === "Tab") {
        const controls = Array.from(document.querySelectorAll<HTMLElement>(".modal button:not(:disabled), .modal a[href], .modal input:not(:disabled), .modal select:not(:disabled)"));
        if (controls.length === 0) return;
        const first = controls[0];
        const last = controls[controls.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
      }
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [confirmation]);
  const navigate = (path: string, explicitPage?: PortalPage) => {
    window.history.pushState({}, "", path);
    setPage(explicitPage ?? available.find((item) => item.path === path)?.page ?? "Chat");
    setMobileOpen(false);
  };
  const setCollapsedState = () => {
    const next = !collapsed;
    setCollapsed(next); localStorage.setItem("sovereign-sidebar-collapsed", String(next));
  };
  const primary = available.filter((item) => item.section === "primary");
  const admin = available.filter((item) => item.section === "admin");
  const readinessText = readiness?.overall === "ready" ? "System ready" : readiness ? "Starting services" : "Checking system";
  const cycleAppearance = () => setAppearance(appearance === "system" ? "dark" : appearance === "dark" ? "light" : "system");

  return <div className={`shell ${collapsed ? "sidebar-collapsed" : ""} ${mobileOpen ? "mobile-nav-open" : ""}`}>
    {mobileOpen && <button className="nav-scrim" aria-label="Close navigation" onClick={() => setMobileOpen(false)} />}
    <aside aria-label="Main navigation">
      <div className="brand"><img src={lazarusLogo} alt="" /><div><strong>Sovereign</strong><span>Private AI</span></div><button className="collapse-button" aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"} onClick={setCollapsedState}>{collapsed ? "›" : "‹"}</button></div>
      <nav>
        <div className="nav-group">{primary.map((item) => <button key={item.path} title={t(`nav.${item.page}`, item.page)} className={page === item.page ? "active" : ""} onClick={() => navigate(item.path)}><span className="nav-icon" aria-hidden="true">{item.page.charAt(0)}</span><span>{t(`nav.${item.page}`, item.page)}</span></button>)}</div>
        {admin.length > 0 && <div className="nav-group admin-nav"><span className="nav-heading">{t("shell.administration", "Administration")}</span>{admin.map((item) => <button key={item.path} title={t(`nav.${item.page}`, item.page)} className={page === item.page ? "active" : ""} onClick={() => navigate(item.path)}><span className="nav-icon" aria-hidden="true">{item.page.charAt(0)}</span><span>{t(`nav.${item.page}`, item.page)}</span></button>)}</div>}
      </nav>
      <div className="aside-foot"><button className="system-chip" onClick={() => navigate("/activity", "Activity")}><i className={readiness?.overall === "ready" ? "ready" : "starting"} /><span>{readinessText}</span></button><span className="signed-in">{identity.display_name || identity.username}<small>{identity.role}</small></span><button className="signout" onClick={onLogout}>{t("shell.signOut", "Sign out")}</button></div>
    </aside>
    <main>
      <header><button className="mobile-menu" aria-label="Open navigation" onClick={() => setMobileOpen(true)}>☰</button><div><span className="eyebrow">Sovereign Portal</span><h1>{t(`nav.${page}`, page)}</h1></div><div className="spacer" />{busy && <span className="working"><i />{busy}</span>}<details className="account-menu"><summary aria-label="Open account menu">{(identity.display_name || identity.username).charAt(0).toUpperCase()}</summary><div className="account-popover"><strong>{identity.display_name || identity.username}</strong><small>{identity.username} · {identity.role}</small><button className="secondary" onClick={cycleAppearance}>Appearance: {appearance}</button><a className="button secondary" href="https://github.com/Lazarus-AI-Research/sovereign-stack/tree/main/docs" target="_blank" rel="noreferrer">Documentation</a><button className="secondary" onClick={onLogout}>{t("shell.signOut", "Sign out")}</button></div></details></header>
      {message && <div role={message.tone === "bad" ? "alert" : "status"} aria-live={message.tone === "bad" ? "assertive" : "polite"} className={`toast ${message.tone}`}><span>{message.text}</span><button aria-label="Dismiss notification" onClick={() => setMessage(null)}>×</button></div>}
      <div className={`content ${["Chat", "Grafana", "Phoenix"].includes(page) ? "app-content" : ""}`}>
        {page === "Chat" && <EmbeddedApp title="Sovereign Chat" src="/api/control/v1/workspace/sso" />}
        {page === "Activity" && <Activity canManage={ROLE_LEVEL[identity.role] >= ROLE_LEVEL.manager} run={run} />}
        {page === "Tools" && <Tools open={(path) => navigate(path)} />}
        {page === "System" && <Overview run={run} confirm={confirm} />}
        {page === "Models" && <Models run={run} confirm={confirm} />}
        {page === "Embeddings" && <Knowledge run={run} />}
        {page === "Evaluations" && <Evaluations run={run} />}
        {page === "Grafana" && <EmbeddedApp title="Grafana" src="/apps/grafana/" />}
        {page === "Phoenix" && <EmbeddedApp title="Phoenix" src="/apps/phoenix/" />}
        {page === "People" && <People run={run} />}
        {page === "API & Providers" && <Access run={run} confirm={confirm} />}
        {page === "Network Access" && <NetworkAccess run={run} />}
        {page === "Backups & Recovery" && <Resilience run={run} confirm={confirm} />}
        {page === "Updates" && <Updates run={run} />}
        {page === "Settings" && <Settings run={run} />}
      </div>
    </main>
    {confirmation && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) { confirmation.resolve(false); setConfirmation(null); } }}><section className="modal" role="dialog" aria-modal="true" aria-labelledby="confirmation-title">
      <h2 id="confirmation-title">{confirmation.title}</h2><p>{confirmation.message}</p><div className="modal-actions"><button className="secondary" onClick={() => { confirmation.resolve(false); setConfirmation(null); }}>Cancel</button><button autoFocus className={confirmation.danger ? "danger" : ""} onClick={() => { confirmation.resolve(true); setConfirmation(null); }}>{confirmation.confirmLabel || "Continue"}</button></div>
    </section></div>}
  </div>;
}
