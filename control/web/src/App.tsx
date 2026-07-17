import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "./api";
import { Dashboard } from "./Dashboard";
import { Login } from "./Login";
import { applyTheme } from "./theme";

type Session = "checking" | "anonymous" | "authenticated";

export function App() {
  const [session, setSession] = useState<Session>("checking");

  // Apply the customer's branding as early as possible — /theme is public, so
  // the login screen is themed too. A failure leaves the default theme in place.
  useEffect(() => {
    api.theme().then(applyTheme).catch(() => {});
  }, []);

  const check = useCallback(async () => {
    try {
      await api.me();
      setSession("authenticated");
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setSession("anonymous");
      } else {
        // auth not ready yet (control still connecting to its database)
        setSession("anonymous");
      }
    }
  }, []);

  useEffect(() => {
    check();
  }, [check]);

  if (session === "checking") {
    return <div className="centered">Loading…</div>;
  }
  if (session === "anonymous") {
    return <Login onLogin={() => setSession("authenticated")} />;
  }
  return (
    <Dashboard
      onLogout={async () => {
        await api.logout().catch(() => {});
        setSession("anonymous");
      }}
    />
  );
}
