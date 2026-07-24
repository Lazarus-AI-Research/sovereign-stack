import { FormEvent, useEffect, useRef, useState } from "react";
import { api, ApiError, CatalogModel, HardwareInventory, Invitation, Job, NetworkStatus } from "./api";

export function Onboarding({ kind, token, onComplete }: { kind: "claim" | "invite"; token: string; onComplete: () => void }) {
  const [invite, setInvite] = useState<Invitation | null>(null);
  const [form, setForm] = useState({ username: "", display_name: "", password: "", confirm: "" });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [step, setStep] = useState<"account" | "network" | "recommendation" | "provisioning">("account");
  const [hardware, setHardware] = useState<HardwareInventory | null>(null);
  const [recommended, setRecommended] = useState<CatalogModel | null>(null);
  const [job, setJob] = useState<Job | null>(null);
  const [network, setNetwork] = useState<NetworkStatus | null>(null);
  const [accessMode, setAccessMode] = useState<"desktop" | "lan">("desktop");
  const [redirectURL, setRedirectURL] = useState("");
  const pollRef = useRef<number | null>(null);

  useEffect(() => {
    if (kind === "invite") api.invitation(token).then(setInvite).catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, [kind, token]);
  useEffect(() => () => { if (pollRef.current !== null) window.clearInterval(pollRef.current); }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (form.password !== form.confirm) { setError("Passwords do not match"); return; }
    setBusy(true); setError("");
    try {
      const value = { username: form.username, display_name: form.display_name, password: form.password };
      if (kind === "claim") {
        await api.claim(token, value);
        const [inventory, catalog, access] = await Promise.all([api.hardware(), api.modelCatalog(), api.network().catch(() => null)]);
        setHardware(inventory);
        setRecommended(catalog.models.find((model) => model.role === "generation" && model.recommended) ?? null);
        setNetwork(access);
        if (access?.access_mode === "lan") setAccessMode("lan");
        setStep("recommendation");
      } else {
        await api.acceptInvitation(token, value);
        finish();
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : `Setup failed: ${err instanceof Error ? err.message : err}`);
    } finally { setBusy(false); }
  }

  function finish() {
    if (pollRef.current !== null) window.clearInterval(pollRef.current);
    window.history.replaceState({}, "", "/");
    onComplete();
  }

  async function configureNetwork() {
    setBusy(true); setError("");
    try {
      if (!network || network.access_mode === accessMode) { finish(); return; }
      const target = accessMode === "lan" ? network.lan_addresses?.[0] || "" : "";
      if (accessMode === "lan" && !target) throw new ApiError(422, "No private network address was detected.", "network_address_unavailable", "Keep the current setting and configure Network Access later.");
      const current = new URL(network.public_url || window.location.origin);
      const port = current.port || window.location.port || "8880";
      const nextURL = accessMode === "lan" ? `http://${target}:${port}/` : `http://127.0.0.1:${port}/`;
      await api.setNetwork(accessMode, target);
      setRedirectURL(nextURL);
      const localBrowser = ["127.0.0.1", "localhost", "::1"].includes(window.location.hostname);
      if (accessMode === "lan" || localBrowser) window.setTimeout(() => window.location.assign(nextURL), 2200);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.message}${err.action ? ` ${err.action}` : ""}` : String(err));
    } finally { setBusy(false); }
  }

  async function provision() {
    if (!recommended) { if (network?.managed) setStep("network"); else finish(); return; }
    setBusy(true); setError("");
    try {
      const result = await api.installCatalogModel(recommended.id, false);
      setStep("provisioning");
      if (!result.job_id) {
        pollRef.current = window.setInterval(async () => {
          try {
            const state = await api.readiness();
            if (state.components.generation?.state === "ready") {
              const now = new Date().toISOString();
              setJob({ id: "startup", kind: "model-load", status: "succeeded", stage: "complete", message: "Generation model is ready", progress_current: 1, progress_total: 1, progress_unit: "steps", cancel_requested: false, created_at: now, updated_at: now, finished_at: now });
              if (pollRef.current !== null) window.clearInterval(pollRef.current);
            }
          } catch { /* Control may restart while the runtime comes online. */ }
        }, 1500);
        return;
      }
      const initial = await api.job(result.job_id);
      setJob(initial);
      pollRef.current = window.setInterval(async () => {
        try {
          const next = await api.job(result.job_id!);
          setJob(next);
          if (["succeeded", "failed", "canceled"].includes(next.status) && pollRef.current !== null) window.clearInterval(pollRef.current);
        } catch { if (pollRef.current !== null) window.clearInterval(pollRef.current); }
      }, 1500);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally { setBusy(false); }
  }

  if (step === "network") return <div className="centered"><section className="card onboarding-card">
    <SetupProgress current={4} />
    <span className="eyebrow">Choose portal access</span><h1>Where will you use SovereignStack?</h1>
    <p className="onboarding-lead">You will always use one portal address. Individual tools and service ports stay out of sight.</p>
    <div className="access-options" role="radiogroup" aria-label="Portal access">
      <label className={accessMode === "desktop" ? "selected" : ""}><input type="radio" name="access" value="desktop" checked={accessMode === "desktop"} onChange={() => setAccessMode("desktop")} /><span><strong>Only this computer</strong><small>Safest for a personal workstation.</small></span></label>
      <label className={accessMode === "lan" ? "selected" : ""}><input type="radio" name="access" value="lan" checked={accessMode === "lan"} onChange={() => setAccessMode("lan")} /><span><strong>My private network</strong><small>Recommended for a server or shared appliance.</small></span></label>
    </div>
    <p className="notice">A custom domain with automatic HTTPS can be added later under Network Access.{accessMode === "lan" && network?.lan_addresses?.[0] ? ` The portal will move to ${network.lan_addresses[0]}.` : ""}</p>
    {redirectURL && <p className="notice" role="status">{accessMode === "desktop" && !["127.0.0.1", "localhost", "::1"].includes(window.location.hostname) ? <>Access is now limited to the host. Open <strong>{redirectURL}</strong> in a browser on that computer.</> : <>Opening <a href={redirectURL}>{redirectURL}</a>. Sign in once at the new address.</>}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    <div className="setup-actions"><button className="secondary" onClick={finish}>Keep current setting</button><button disabled={busy || Boolean(redirectURL) || (accessMode === "lan" && !network?.lan_addresses?.length)} onClick={configureNetwork}>{busy ? "Applying…" : network?.access_mode === accessMode ? "Open chat" : "Apply and open portal"}</button></div>
  </section></div>;

  if (step === "recommendation") return <div className="centered"><section className="card onboarding-card">
    <SetupProgress current={2} />
    <span className="eyebrow">Hardware detected</span><h1>Your private AI is ready to set up</h1>
    <p className="onboarding-lead">SovereignStack selected a reviewed configuration for this machine. You can change models later under Administration.</p>
    <div className="setup-summary">
      <div><span>Hardware</span><strong>{hardware?.gpu?.name || hardware?.profile || "Detected automatically"}</strong><small>{hardware?.memory_bytes ? `${Math.round(hardware.memory_bytes / 1024 / 1024 / 1024)} GB memory` : hardware?.architecture}</small></div>
      <div><span>Assistant</span><strong>{recommended?.display_name || "Existing model"}</strong><small>{recommended ? `${Math.round(recommended.download_bytes / 1024 / 1024 / 1024 * 10) / 10} GB download` : "Already configured"}</small></div>
      <div><span>Knowledge search</span><strong>EmbeddingGemma</strong><small>Built in · private · recommended</small></div>
    </div>
    {recommended && !recommended.compatible && <p className="error" role="alert">{recommended.compatibility_reason || "The recommended model is not compatible with this appliance."} Open advanced setup from the portal.</p>}
    {error && <p className="error" role="alert">{error}</p>}
    <div className="setup-actions"><button className="secondary" onClick={() => network?.managed ? setStep("network") : finish()}>Configure later</button><button disabled={busy || (recommended !== null && !recommended.compatible)} onClick={provision}>{busy ? "Starting…" : "Use recommended setup"}</button></div>
  </section></div>;

  if (step === "provisioning") {
    const percent = job?.progress_total ? Math.round(job.progress_current / job.progress_total * 100) : null;
    return <div className="centered"><section className="card onboarding-card">
      <SetupProgress current={3} />
      <span className="eyebrow">Preparing your assistant</span><h1>{job?.status === "succeeded" ? "Ready to chat" : job?.status === "failed" ? "Setup needs attention" : "You can keep this page open"}</h1>
      <p className="onboarding-lead">{job?.message || "SovereignStack is configuring the recommended model in the background."}</p>
      <progress max={job?.progress_total || 1} value={job?.progress_total ? job.progress_current : undefined} />
      <div className="provisioning-status"><StateLabel value={job?.stage || "starting"} /><span>{percent === null ? "Working…" : `${percent}%`}</span></div>
      {job?.error && <p className="error" role="alert">{job.error}</p>}
      <div className="setup-actions"><button className="secondary" onClick={() => network?.managed ? setStep("network") : finish()}>{network?.managed ? "Continue" : job?.status === "succeeded" ? "Open chat" : "Continue in background"}</button>{job?.status === "failed" && <button onClick={provision}>Retry</button>}</div>
    </section></div>;
  }

  return <div className="centered"><form className="card login onboarding-account" onSubmit={submit}>
    {kind === "claim" && <SetupProgress current={1} />}
    <span className="eyebrow">{kind === "claim" ? "First-run setup" : `${invite?.role ?? "User"} invitation`}</span>
    <h1>{kind === "claim" ? "Create the first administrator" : "Join SovereignStack"}</h1>
    {kind === "claim" && <p className="onboarding-privacy">Your account and prompts stay on this appliance. SovereignStack does not require a cloud account.</p>}
    <label>Display name<input required autoFocus value={form.display_name} onChange={(e) => setForm({ ...form, display_name: e.target.value })} /></label>
    <label>Username<input required autoComplete="username" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value.toLowerCase() })} /></label>
    <label>Password<input required minLength={12} type="password" autoComplete="new-password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} /></label>
    <label>Confirm password<input required minLength={12} type="password" autoComplete="new-password" value={form.confirm} onChange={(e) => setForm({ ...form, confirm: e.target.value })} /></label>
    {error && <p className="error" role="alert">{error}</p>}
    <button disabled={busy || form.password.length < 12}>{busy ? "Creating account…" : "Continue"}</button>
  </form></div>;
}

function SetupProgress({ current }: { current: number }) {
  const tone = (step: number) => step < current ? "done" : step === current ? "current" : "";
  return <div className="setup-progress four" aria-label={`Setup step ${current} of 4`}>
    <span className={tone(1)}>1</span><i /><span className={tone(2)}>2</span><i /><span className={tone(3)}>3</span><i /><span className={tone(4)}>4</span>
  </div>;
}

function StateLabel({ value }: { value: string }) {
  return <span className="setup-state">{value.replaceAll("_", " ")}</span>;
}
