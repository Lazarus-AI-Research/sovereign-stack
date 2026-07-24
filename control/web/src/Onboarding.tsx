import { FormEvent, useEffect, useState } from "react";
import { api, ApiError, Invitation } from "./api";

export function Onboarding({ kind, token, onComplete }: { kind: "claim" | "invite"; token: string; onComplete: () => void }) {
  const [invite, setInvite] = useState<Invitation | null>(null);
  const [form, setForm] = useState({ username: "", display_name: "", password: "", confirm: "" });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (kind === "invite") api.invitation(token).then(setInvite).catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, [kind, token]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (form.password !== form.confirm) { setError("Passwords do not match"); return; }
    setBusy(true); setError("");
    try {
      const value = { username: form.username, display_name: form.display_name, password: form.password };
      if (kind === "claim") await api.claim(token, value);
      else await api.acceptInvitation(token, value);
      window.history.replaceState({}, "", "/");
      onComplete();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : `Setup failed: ${err instanceof Error ? err.message : err}`);
    } finally { setBusy(false); }
  }

  return <div className="centered"><form className="card login" onSubmit={submit}>
    <span className="eyebrow">{kind === "claim" ? "First-run setup" : `${invite?.role ?? "User"} invitation`}</span>
    <h1>{kind === "claim" ? "Create the first administrator" : "Join SovereignStack"}</h1>
    <label>Display name<input required autoFocus value={form.display_name} onChange={(e) => setForm({ ...form, display_name: e.target.value })} /></label>
    <label>Username<input required autoComplete="username" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value.toLowerCase() })} /></label>
    <label>Password<input required minLength={12} type="password" autoComplete="new-password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} /></label>
    <label>Confirm password<input required minLength={12} type="password" autoComplete="new-password" value={form.confirm} onChange={(e) => setForm({ ...form, confirm: e.target.value })} /></label>
    {error && <p className="error">{error}</p>}
    <button disabled={busy || form.password.length < 12}>{busy ? "Creating account…" : "Continue"}</button>
  </form></div>;
}
