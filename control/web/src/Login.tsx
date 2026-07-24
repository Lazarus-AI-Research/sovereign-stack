import { FormEvent, useState } from "react";
import { api, ApiError } from "./api";

export function Login({ onLogin }: { onLogin: () => void }) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.login(username, password);
      onLogin();
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 401
          ? "That username or password is not correct."
          : `Login failed: ${err instanceof Error ? err.message : err}`,
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="centered">
      <form className="card login" onSubmit={submit}>
        <span className="eyebrow">SovereignStack</span>
        <h1>Welcome back</h1>
        <p className="login-intro">Sign in to your private AI workspace and administration portal.</p>
        <label>
          Username
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
          />
        </label>
        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            autoFocus
          />
        </label>
        {error && <p className="error">{error}</p>}
        <button disabled={busy || !password}>{busy ? "Signing in…" : "Sign in"}</button>
      </form>
    </div>
  );
}
