import { FormEvent, ReactNode, useCallback, useEffect, useState } from "react";
import {
  api,
  ApiError,
  BackupManifest,
  Branding,
  BundleManifest,
  CredentialMetadata,
  EmbeddingProfile,
  EvalReport,
  Features,
  IndexVersion,
  Manifest,
  ModelEntry,
  RuntimeErrors,
  Status,
  Workspace,
} from "./api";
import lazarusLogo from "./assets/lazarus_logo.png";

const TABS = ["Overview", "Models", "Knowledge", "Evaluations", "Access", "Resilience", "Settings"] as const;
type Tab = (typeof TABS)[number];
type RunAction = (label: string, action: () => Promise<unknown>) => Promise<boolean>;

const RUNTIME_STATES = ["initializing", "downloading", "compiling", "loading", "smoke_testing", "healthy"];

function StatePill({ state }: { state?: string }) {
  const tone = state === "healthy" || state === "active" || state === "succeeded" || state === "running"
    ? "ok"
    : state === "degraded" || state === "building" || state === "queued" || state === "validating"
      ? "warn"
      : state ? "bad" : "unknown";
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

function Overview({ run }: { run: RunAction }) {
  const [status, setStatus] = useState<Status | null>(null);
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [errors, setErrors] = useState<RuntimeErrors | null>(null);

  const refresh = useCallback(async () => {
    const [nextStatus, nextManifest, nextErrors] = await Promise.all([
      api.status(), api.manifest().catch(() => null), api.runtimeErrors().catch(() => null),
    ]);
    setStatus(nextStatus); setManifest(nextManifest); setErrors(nextErrors);
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
      <div className="metric"><span>Required roles</span><strong>{Object.values(status?.runtime.required_roles ?? {}).filter(Boolean).length || "—"}</strong><small>generation + embedding</small></div>
    </div>

    <section className="card">
      <PanelTitle title="Runtime lifecycle" subtitle="A ready appliance completes every stage and keeps both required roles healthy."
        action={<button className="secondary" onClick={() => run("Restart runtime", async () => { await api.restartRuntime(); })}>Restart runtime</button>} />
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
      <PanelTitle title="Appliance services" subtitle="Only the loopback ingress is exposed on the host." />
      <div className="service-grid">
        <div><StatePill state={status?.gateway.healthy ? "healthy" : "unreachable"} /><span>Gateway</span></div>
        <div><StatePill state={status?.docker_proxy.reachable ? "healthy" : "unreachable"} /><span>Restricted Docker proxy</span></div>
        {Object.entries(status?.services ?? {}).map(([service, state]) => <div key={service}><StatePill state={state} /><span>{service.replace(/^sovereign-/, "")}</span></div>)}
      </div>
    </section>
  </>;
}

function Models({ run }: { run: RunAction }) {
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [credentials, setCredentials] = useState<CredentialMetadata[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ id: "", role: "generation", source: "cloud", provider: "openai", model: "", revision: "", base_url: "", credential_id: "" });
  const refresh = useCallback(async () => {
    const [modelResult, credentialResult] = await Promise.all([api.models(), api.credentials().catch(() => ({ credentials: [] }))]);
    setModels(modelResult.models); setCredentials(credentialResult.credentials);
  }, []);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    const entry: ModelEntry = {
      id: form.id, role: form.role, source: form.source, provider: form.provider,
      model: form.model, capabilities: form.role === "embedding" ? ["text"] : ["chat", "completions"],
    };
    if (form.revision) entry.revision = form.revision;
    if (form.base_url) entry.base_url = form.base_url;
    if (form.credential_id) entry.credential_id = form.credential_id;
    if (await run("Add model", () => api.createModel(entry))) {
      setShowForm(false); setForm({ ...form, id: "", model: "", revision: "", base_url: "" }); await refresh();
    }
  }

  return <>
    <section className="card">
      <PanelTitle title="Model registry" subtitle="Local, pinned catalog, and cloud routes share stable product aliases."
        action={<button onClick={() => setShowForm(!showForm)}>{showForm ? "Cancel" : "Add model"}</button>} />
      {showForm && <form className="form-grid inset" onSubmit={submit}>
        <label>Product ID<input required value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} placeholder="team-coding-model" /></label>
        <label>Role<select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}><option value="generation">Generation</option><option value="embedding">Embedding</option></select></label>
        <label>Source<select value={form.source} onChange={(e) => setForm({ ...form, source: e.target.value })}><option value="cloud">Cloud</option><option value="remote">OpenAI-compatible remote</option><option value="huggingface">Hugging Face</option><option value="modelscope">ModelScope</option><option value="local">Local path</option></select></label>
        <label>Provider<select value={form.provider} onChange={(e) => setForm({ ...form, provider: e.target.value })}><option value="openai">OpenAI</option><option value="anthropic">Anthropic</option><option value="gemini">Gemini</option><option value="openai-compatible">OpenAI compatible</option><option value="custom">Custom</option></select></label>
        <label className="wide">Model or repository<input required value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} placeholder="gpt-5-mini or org/model" /></label>
        <label>Immutable revision<input value={form.revision} onChange={(e) => setForm({ ...form, revision: e.target.value })} placeholder="40-character commit" /></label>
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
            <button className="small secondary" onClick={() => run(`Load ${model.id}`, async () => { await completeJob(await api.loadModel(model.id)); await refresh(); })}>Load</button>
            <button className="small secondary" onClick={() => run(`Smoke-test ${model.id}`, async () => { await completeJob(await api.smokeModel(model.id)); })}>Test</button>
            <button className="small danger" onClick={() => window.confirm(`Delete ${model.id}?`) && run(`Delete ${model.id}`, async () => { await api.deleteModel(model.id); await refresh(); })}>Delete</button>
          </div></td>
        </tr>)}</tbody>
      </table></div>}
    </section>
  </>;
}

function Knowledge({ run }: { run: RunAction }) {
  const [profiles, setProfiles] = useState<Record<string, EmbeddingProfile>>({});
  const [indexes, setIndexes] = useState<IndexVersion[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [selectedProfile, setSelectedProfile] = useState("");
  const [validation, setValidation] = useState("");
  const refresh = useCallback(async () => {
    const [profileResult, indexResult, workspaceResult] = await Promise.all([
      api.embeddingProfiles(), api.indexes(), api.workspaces().catch(() => ({ workspaces: [] })),
    ]);
    setProfiles(profileResult.embedding_profiles); setIndexes(indexResult.indexes); setWorkspaces(workspaceResult.workspaces);
    if (!selectedProfile) setSelectedProfile(Object.keys(profileResult.embedding_profiles)[0] ?? "");
  }, [selectedProfile]);
  useEffect(() => { refresh().catch(() => {}); }, [refresh]);

  return <>
    <section className="card">
      <PanelTitle title="Embedding profiles" subtitle="Dimensions are discovered from the loaded runtime; prefixes and preprocessing are versioned with each index." />
      <div className="profile-grid">{Object.entries(profiles).map(([id, profile]) => <article className="profile" key={id}>
        <div className="row"><h3>{id}</h3><StatePill state={profile.modalities.length > 1 ? "omni" : "text"} /></div>
        <p className="mono clip">{profile.model}</p><p>{profile.modalities.join(" · ")} · {profile.distance_metric} · {profile.normalization || "none"}</p>
        <div className="actions"><button className="small secondary" onClick={() => run(`Validate ${id}`, async () => { const result = await api.validateProfile(id); setValidation(JSON.stringify(result)); })}>Validate</button>
          <button className="small" onClick={() => run(`Activate ${id}`, async () => { await completeJob(await api.activateProfile(id)); await refresh(); })}>Activate</button></div>
      </article>)}</div>
      {validation && <p className="notice">Validation: {validation}</p>}
    </section>

    <section className="card">
      <PanelTitle title="Workspace indexes" subtitle="A rebuild preserves the active index until validation succeeds, then switches atomically." />
      {workspaces.length > 0 && <div className="inset rebuild-row">
        <label>Embedding profile<select value={selectedProfile} onChange={(e) => setSelectedProfile(e.target.value)}>{Object.keys(profiles).map((id) => <option key={id}>{id}</option>)}</select></label>
        <div><span className="label-text">Start or replace an index</span><div className="actions">{workspaces.map((workspace) => <button key={workspace.id} className="secondary" onClick={() => run(`Rebuild ${workspace.name}`, async () => { await completeJob(await api.startIndex(workspace, selectedProfile)); await refresh(); })}>{workspace.name}</button>)}</div></div>
      </div>}
      {indexes.length === 0 ? <Empty>Create a workspace in the main app, then start its first versioned index here.</Empty> : <div className="table-wrap"><table>
        <thead><tr><th>Workspace</th><th>Profile</th><th>Status</th><th>Progress</th><th>Vectors</th><th></th></tr></thead>
        <tbody>{indexes.map((index) => <tr key={index.id}>
          <td><span className="strong">{index.provider_slug}</span><small className="block mono">{index.id.slice(0, 8)}</small></td>
          <td>{index.profile_id}<small className="block">{index.dimensions ? `${index.dimensions} dimensions` : "discovering dimensions"}</small></td>
          <td><StatePill state={index.status} />{index.error && <small className="block error">{index.error}</small>}</td>
          <td>{index.processed_documents}/{index.document_count}</td><td>{index.vector_count.toLocaleString()}</td>
          <td><div className="actions"><button className="small secondary" onClick={() => run(`Rebuild ${index.provider_slug}`, async () => { await completeJob(await api.rebuildIndex(index.id, selectedProfile || index.profile_id)); await refresh(); })}>Rebuild</button>
            {index.status !== "active" && index.vector_count > 0 && <button className="small" onClick={() => run("Activate index", async () => { await api.activateIndex(index.id); await refresh(); })}>Activate</button>}</div></td>
        </tr>)}</tbody>
      </table></div>}
    </section>
  </>;
}

function Evaluations({ run }: { run: RunAction }) {
  const suites = [
    ["smoke", "Release health"], ["quick", "Quick benchmark"], ["embedding", "Portable embedding"],
    ["retrieval", "Retrieval quality"], ["mixed-role", "Mixed-role pressure"], ["omni-embedding", "Cross-modal retrieval"], ["full", "Full benchmark"],
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

function Access({ run }: { run: RunAction }) {
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
      <div className="compact-list">{credentials.map((credential) => <div key={credential.id}><div><strong>{credential.label}</strong><small>{credential.provider} · {credential.id}</small></div><button className="small danger" onClick={() => window.confirm(`Delete ${credential.label}?`) && run("Delete credential", async () => { await api.deleteCredential(credential.id); await refresh(); })}>Delete</button></div>)}</div>
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

function Resilience({ run }: { run: RunAction }) {
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
      {backups.length === 0 ? <Empty>No completed backups.</Empty> : <div className="compact-list">{backups.map((backup) => <div key={backup.id}><div><strong>{backup.id}</strong><small>{backup.files.length} files · {backup.created_at}</small></div><div className="actions"><button className="small secondary" onClick={() => run("Verify backup", async () => { const result = await api.verifyBackup(backup.id); if (!result.valid) throw new ApiError(422, result.problems.join(", ")); })}>Verify</button><button className="small danger" onClick={() => window.confirm("Restore replaces the live appliance databases and configuration. Continue?") && run("Restore backup", async () => { await completeJob(await api.restoreBackup(backup.id)); })}>Restore</button></div></div>)}</div>}
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
      <form className="stack-form" onSubmit={(event) => { event.preventDefault(); run("Apply branding", async () => { await api.saveBranding(branding); await api.applyBranding(); }); }}>
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

export function Dashboard({ onLogout }: { onLogout: () => void }) {
  const [tab, setTab] = useState<Tab>("Overview");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<{ tone: "ok" | "bad"; text: string } | null>(null);
  const run: RunAction = async (label, action) => {
    setBusy(label); setMessage(null);
    try { await action(); setMessage({ tone: "ok", text: `${label} completed.` }); return true; }
    catch (error) { setMessage({ tone: "bad", text: error instanceof Error ? error.message : String(error) }); return false; }
    finally { setBusy(""); }
  };

  return <div className="shell">
    <aside>
      <div className="brand"><img src={lazarusLogo} alt="Lazarus" /><div><strong>Sovereign</strong><span>Control plane</span></div></div>
      <nav>{TABS.map((item) => <button key={item} className={tab === item ? "active" : ""} onClick={() => setTab(item)}>{item}</button>)}</nav>
      <div className="aside-foot"><a href="/" className="workspace-link">Open workspace ↗</a><button className="signout" onClick={onLogout}>Sign out</button></div>
    </aside>
    <main>
      <header><div><span className="eyebrow">Sovereign Control</span><h1>{tab}</h1></div><div className="spacer" />{busy && <span className="working"><i />{busy}</span>}</header>
      {message && <button className={`toast ${message.tone}`} onClick={() => setMessage(null)}>{message.text}<span>×</span></button>}
      <div className="content">
        {tab === "Overview" && <Overview run={run} />}
        {tab === "Models" && <Models run={run} />}
        {tab === "Knowledge" && <Knowledge run={run} />}
        {tab === "Evaluations" && <Evaluations run={run} />}
        {tab === "Access" && <Access run={run} />}
        {tab === "Resilience" && <Resilience run={run} />}
        {tab === "Settings" && <Settings run={run} />}
      </div>
    </main>
  </div>;
}
