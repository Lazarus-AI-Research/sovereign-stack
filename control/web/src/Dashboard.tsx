import { useEffect, useState } from "react";
import { api, Manifest, RuntimeErrors, Status } from "./api";

const RUNTIME_STATES = [
  "initializing",
  "downloading",
  "compiling",
  "loading",
  "smoke_testing",
  "healthy",
] as const;

function StatePill({ state }: { state?: string }) {
  const tone =
    state === "healthy" ? "ok" : state === "degraded" ? "warn" : state ? "bad" : "unknown";
  return <span className={`pill ${tone}`}>{state ?? "unreachable"}</span>;
}

export function Dashboard({ onLogout }: { onLogout: () => void }) {
  const [status, setStatus] = useState<Status | null>(null);
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [errors, setErrors] = useState<RuntimeErrors | null>(null);
  const [restarting, setRestarting] = useState(false);

  useEffect(() => {
    let live = true;
    async function poll() {
      try {
        const [s, m, e] = await Promise.all([
          api.status(),
          api.manifest().catch(() => null),
          api.runtimeErrors().catch(() => null),
        ]);
        if (live) {
          setStatus(s);
          setManifest(m);
          setErrors(e);
        }
      } catch {
        // transient; keep last view
      }
    }
    poll();
    const timer = setInterval(poll, 5000);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, []);

  const runtimeState = status?.runtime.state;

  return (
    <div className="page">
      <header>
        <h1>Sovereign Control</h1>
        <div className="spacer" />
        <span className="muted">{status?.control.version}</span>
        <button className="ghost" onClick={onLogout}>
          Sign out
        </button>
      </header>

      <section className="card">
        <div className="row">
          <h2>Runtime</h2>
          <StatePill state={runtimeState} />
          <div className="spacer" />
          <button
            className="ghost"
            disabled={restarting}
            onClick={async () => {
              setRestarting(true);
              try {
                await api.restartRuntime();
              } finally {
                setTimeout(() => setRestarting(false), 3000);
              }
            }}
          >
            {restarting ? "Restarting…" : "Restart runtime"}
          </button>
        </div>
        <ol className="statemachine">
          {RUNTIME_STATES.map((state) => (
            <li key={state} className={state === runtimeState ? "current" : ""}>
              {state.replace("_", " ")}
            </li>
          ))}
        </ol>
        {manifest && (
          <table>
            <thead>
              <tr>
                <th>Role</th>
                <th>Status</th>
                <th>Alias</th>
                <th>Engine model</th>
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(manifest.roles).map(([name, role]) => (
                <tr key={name}>
                  <td>{name}</td>
                  <td>
                    <StatePill state={role.enabled ? role.status : "disabled"} />
                  </td>
                  <td>{role.served_model_name ?? "—"}</td>
                  <td className="mono">{role.engine_model ?? "—"}</td>
                  <td>
                    {role.dimensions ? `dim ${role.dimensions}` : ""}
                    {role.modalities ? ` · ${role.modalities.join(", ")}` : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {errors && errors.errors.length > 0 && (
          <div className="errors">
            {errors.errors.map((error) => (
              <p key={error.code + (error.role ?? "")} className="error">
                <strong>{error.code}</strong>
                {error.role ? ` (${error.role})` : ""}: {error.message}
              </p>
            ))}
          </div>
        )}
      </section>

      <section className="card">
        <h2>Services</h2>
        <div className="badges">
          <span className={`pill ${status?.gateway.healthy ? "ok" : "bad"}`}>gateway</span>
          <span className={`pill ${status?.docker_proxy.reachable ? "ok" : "bad"}`}>
            docker proxy
          </span>
          {Object.entries(status?.services ?? {}).map(([service, state]) => (
            <span key={service} className={`pill ${state === "running" ? "ok" : "bad"}`}>
              {service.replace(/^sovereign-/, "")}
            </span>
          ))}
        </div>
      </section>
    </div>
  );
}
