const BASE = "/api/control/v1";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(BASE + path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new ApiError(resp.status, body.error ?? resp.statusText);
  }
  return body as T;
}

export interface Status {
  control: { status: string; version: string };
  runtime: {
    reachable: boolean;
    state?: string;
    ready?: boolean;
    required_roles?: Record<string, boolean>;
  };
  gateway: { healthy: boolean };
  docker_proxy: { reachable: boolean; docker?: string };
  services?: Record<string, string>;
}

export interface Manifest {
  state: string;
  profile: string;
  backend: string;
  runtime_version: string;
  roles: Record<
    string,
    {
      enabled: boolean;
      status: string;
      served_model_name?: string;
      engine_model?: string;
      dimensions?: number;
      modalities?: string[];
    }
  >;
}

export interface RuntimeErrors {
  errors: {
    code: string;
    role?: string;
    message: string;
    recoverable: boolean;
    first_seen: string;
  }[];
}

export const api = {
  login: (username: string, password: string) =>
    request<{ token: string }>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<unknown>("/auth/logout", { method: "POST" }),
  me: () => request<{ username: string }>("/auth/me"),
  status: () => request<Status>("/status"),
  manifest: () => request<Manifest>("/runtime/manifest"),
  runtimeErrors: () => request<RuntimeErrors>("/runtime/errors"),
  restartRuntime: () => request<unknown>("/runtime/restart", { method: "POST" }),
};
