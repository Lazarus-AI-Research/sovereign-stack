const BASE = "/api/control/v1";

export class ApiError extends Error {
  status: number;
  code?: string;
  action?: string;
  constructor(status: number, message: string, code?: string, action?: string) {
    super(message);
    this.status = status;
    this.code = code;
    this.action = action;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(BASE + path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new ApiError(resp.status, body.error ?? resp.statusText, body.code, body.action);
  return body as T;
}

const idPath = (id: string) => encodeURIComponent(id);

export interface Status {
  control: { status: string; version: string };
  runtime: { reachable: boolean; state?: string; ready?: boolean; required_roles?: Record<string, boolean> };
  embeddings?: { reachable: boolean; backend?: string; profile_id?: string; served_model_name?: string };
  gateway: { healthy: boolean };
  docker_proxy: { reachable: boolean; docker?: string };
  services?: Record<string, string>;
}

export interface Manifest {
  state: string;
  profile: string;
  backend: string;
  runtime_version: string;
  roles: Record<string, {
    enabled: boolean;
    status: string;
    served_model_name?: string;
    engine_model?: string;
    dimensions?: number;
    modalities?: string[];
  }>;
}

export interface RuntimeErrors {
  errors: { code: string; role?: string; message: string; recoverable: boolean; first_seen: string }[];
}

export interface ModelEntry {
  id: string;
  role: string;
  source: string;
  model: string;
  revision?: string;
  artifact?: string;
  sha256?: string;
  base_url?: string;
  credential_id?: string;
  provider?: string;
  capabilities?: string[];
  compatible_profiles?: string[];
  validation_state?: string;
  loaded?: boolean;
}

export interface EmbeddingProfile {
  provider: string;
  source: string;
  model: string;
  revision: string;
  served_model_name: string;
  pooling?: string;
  normalization?: string;
  distance_metric: string;
  query_prefix?: string;
  document_prefix?: string;
  chunking_strategy: string;
  preprocessing_version: string;
  modalities: string[];
  model_entry_id?: string;
}

export interface Identity {
  id: number;
  username: string;
  display_name: string;
  role: "admin" | "manager" | "member";
  disabled: boolean;
  workspace_ids: string[];
}

export interface Invitation {
  id: string;
  role: Identity["role"];
  workspace_ids: string[];
  created_at: string;
  expires_at: string;
}

export interface EmbeddingState {
  profile_id: string;
  provider: string;
  served_model_name: string;
  dimensions: number;
  activated_at: string;
}

export interface IndexVersion {
  id: string;
  workspace_id: string;
  provider_slug: string;
  profile_id: string;
  model_id: string;
  model_revision: string;
  dimensions: number;
  status: string;
  document_count: number;
  processed_documents: number;
  vector_count: number;
  error?: string;
  created_at: string;
}

export interface Workspace {
  id: string;
  upstream_id: number;
  name: string;
  slug: string;
}

export interface Job {
  id: string;
  kind: string;
  status: "queued" | "running" | "succeeded" | "failed" | "canceled";
  stage: string;
  message?: string;
  progress_current: number;
  progress_total?: number;
  progress_unit?: string;
  progress_rate?: number;
  eta_seconds?: number;
  result?: unknown;
  error?: string;
  error_code?: string;
  action?: string;
  cancel_requested: boolean;
  retry_of?: string;
  initiated_by?: number;
  created_at: string;
  started_at?: string;
  updated_at: string;
  finished_at?: string;
}

export interface ReadinessComponent {
  state: string;
  message: string;
  job_id?: string;
  action?: string;
  progress_current?: number;
  progress_total?: number;
  progress_unit?: string;
  rate_bytes?: number;
  eta_seconds?: number;
}

export interface Readiness {
  overall: "ready" | "starting" | "degraded";
  components: Record<string, ReadinessComponent>;
}

export interface Application {
  id: string;
  label: string;
  description?: string;
  icon?: string;
  path: string;
  health_component?: string;
  minimum_role: Identity["role"];
  embed: boolean;
}

export interface HardwareInventory {
  os: string;
  architecture: string;
  profile: string;
  memory_bytes?: number;
  storage_free_bytes?: number;
  gpu?: { name?: string; vram_bytes?: number };
  detections: { profile: string; reasons: string[] }[];
}

export interface CatalogModel {
  id: string;
  display_name: string;
  description: string;
  role: string;
  capabilities: string[];
  compatible_profiles: string[];
  download_bytes: number;
  minimum_memory_bytes?: number;
  minimum_vram_bytes?: number;
  recommended: boolean;
  registered: boolean;
  compatible: boolean;
  compatibility_reason?: string;
}

export interface NetworkStatus {
  managed: boolean;
  access_mode: "desktop" | "lan" | "domain" | "";
  public_url: string;
  bind_address: string;
  site_address: string;
  version: string;
  lan_addresses?: string[];
}

export interface SupportBundle {
  id: string;
  created_at: string;
  bytes: number;
  sha256: string;
}

export interface UpdateInfo {
  release: {
    current_version: string;
    latest_version?: string;
    available: boolean;
    release_url?: string;
    checked_at: string;
    check_error?: string;
  };
  operation: {
    state: string;
    version?: string;
    message?: string;
    started_at?: string;
    updated_at?: string;
  };
}

export interface EvalReport {
  suite?: string;
  generated_at?: string;
  passed?: boolean;
  results?: unknown[];
  [key: string]: unknown;
}

export interface BackupManifest {
  id: string;
  created_at: string;
  files: { name: string; bytes: number; sha256: string }[];
  excludes: string[];
  verification_state?: "valid" | "invalid";
  verified_at?: string;
}

export interface BundleManifest {
  bundle_id: string;
  version: string;
  profile: string;
  architecture: string;
  created_at: string;
  includes_weights: boolean;
  images: unknown[];
  models: { name: string }[];
  files: { name: string; bytes: number }[];
}

export interface CredentialMetadata {
  id: string;
  provider: string;
  label: string;
  created_at: string;
  updated_at: string;
}

export interface GatewayKeyMetadata {
  id: string;
  alias: string;
  models: string[];
  max_budget?: number;
  spend?: number;
  tpm_limit?: number;
  rpm_limit?: number;
  created_at?: string;
  expires_at?: string;
}

export interface GatewayKeyList {
  keys: GatewayKeyMetadata[];
  base_url: string;
}

export interface IssuedGatewayKey {
  secret: string;
  id?: string;
  alias: string;
  models: string[];
  max_budget?: number;
  tpm_limit?: number;
  rpm_limit?: number;
  expires_at?: string;
  base_url: string;
}

export interface Branding {
  product_name: string;
  company_name: string;
  logo: string;
  logo_animated?: string;
  favicon: string;
  colors: { primary: string; accent: string };
}

export interface Features {
  phoenix: { enabled: boolean };
  tracing: { enabled: boolean; metadata_only: boolean; full_trace: boolean; prompt_logging: boolean; response_logging: boolean };
  prompt_logging: { enabled: boolean };
  experience?: { portal_navigation: boolean; managed_downloads: boolean; host_lifecycle: boolean };
}

export const api = {
  login: (username: string, password: string) => request<{ token: string }>("/auth/login", { method: "POST", body: JSON.stringify({ username, password }) }),
  logout: () => request<unknown>("/auth/logout", { method: "POST" }),
  me: () => request<Identity>("/auth/me"),
  claim: (token: string, value: { username: string; display_name: string; password: string }) => request<Identity>(`/auth/claim/${idPath(token)}`, { method: "POST", body: JSON.stringify(value) }),
  invitation: (token: string) => request<Invitation>(`/auth/invitations/${idPath(token)}`),
  acceptInvitation: (token: string, value: { username: string; display_name: string; password: string }) => request<Identity>(`/auth/invitations/${idPath(token)}`, { method: "POST", body: JSON.stringify(value) }),
  users: () => request<{ users: Identity[] }>("/users"),
  updateUser: (id: number, value: { role: Identity["role"]; disabled: boolean; workspace_ids: string[] }) => request<Identity>(`/users/${id}`, { method: "PATCH", body: JSON.stringify(value) }),
  createInvitation: (value: { role: Identity["role"]; workspace_ids: string[] }) => request<{ invitation: Invitation; token: string; path: string }>("/invitations", { method: "POST", body: JSON.stringify(value) }),
  status: () => request<Status>("/status"),
  readiness: () => request<Readiness>("/readiness"),
  applications: () => request<{ applications: Application[] }>("/applications"),
  hardware: () => request<HardwareInventory>("/hardware"),
  network: () => request<NetworkStatus>("/network"),
  setNetwork: (mode: "desktop" | "lan" | "domain", target = "") => request<{ updating: boolean }>("/network", { method: "PUT", body: JSON.stringify({ mode, target }) }),
  repair: () => request<{ repaired: boolean }>("/repair", { method: "POST", body: "{}" }),
  supportBundles: () => request<{ support_bundles: SupportBundle[] }>("/support-bundles"),
  createSupportBundle: () => request<{ job_id: string }>("/support-bundles", { method: "POST", body: "{}" }),
  supportBundleDownloadURL: (id: string) => `${BASE}/support-bundles/${idPath(id)}/download`,
  updates: () => request<UpdateInfo>("/updates"),
  applyUpdate: (version: string) => request<{ scheduled: boolean; version: string }>("/updates/apply", { method: "POST", body: JSON.stringify({ version }) }),
  manifest: () => request<Manifest>("/runtime/manifest"),
  runtimeErrors: () => request<RuntimeErrors>("/runtime/errors"),
  restartRuntime: () => request<unknown>("/runtime/restart", { method: "POST" }),

  models: () => request<{ models: ModelEntry[] }>("/models"),
  modelCatalog: () => request<{ catalog_version: string; profile: string; models: CatalogModel[] }>("/model-catalog"),
  installCatalogModel: (id: string, activate = true) => request<{ job_id?: string; model_id: string; ready?: boolean }>(`/model-catalog/${idPath(id)}/install`, { method: "POST", body: JSON.stringify({ activate }) }),
  createModel: (model: ModelEntry) => request<ModelEntry>("/models", { method: "POST", body: JSON.stringify(model) }),
  deleteModel: (id: string) => request<unknown>(`/models/${idPath(id)}`, { method: "DELETE" }),
  loadModel: (id: string) => request<{ job_id?: string }>(`/models/${idPath(id)}/load`, { method: "POST" }),
  smokeModel: (id: string) => request<{ job_id: string }>(`/models/${idPath(id)}/smoke-test`, { method: "POST" }),

  embeddingProfiles: () => request<{ embedding_profiles: Record<string, EmbeddingProfile> }>("/embedding-profiles"),
  embeddingState: () => request<{ active: EmbeddingState | null }>("/embedding-state"),
  createEmbeddingProfile: (id: string, profile: EmbeddingProfile) => request<unknown>("/embedding-profiles", { method: "POST", body: JSON.stringify({ id, ...profile }) }),
  activateProfile: (id: string) => request<{ job_id: string }>(`/embedding-profiles/${idPath(id)}/activate`, { method: "POST" }),
  validateProfile: (id: string) => request<Record<string, unknown>>(`/embedding-profiles/${idPath(id)}/validate`, { method: "POST" }),
  indexes: () => request<{ indexes: IndexVersion[] }>("/indexes"),
  workspaces: () => request<{ workspaces: Workspace[] }>("/workspaces"),
  startIndex: (workspace: Workspace, profile: string) => request<{ job_id: string }>("/indexes/rebuild", {
    method: "POST",
    body: JSON.stringify({ workspace_id: workspace.id, provider_slug: workspace.slug, embedding_profile: profile }),
  }),
  rebuildIndex: (id: string, profile: string) => request<{ job_id: string }>(`/indexes/${idPath(id)}/rebuild`, {
    method: "POST", body: JSON.stringify({ embedding_profile: profile, activate_when_complete: true }),
  }),
  activateIndex: (id: string) => request<IndexVersion>(`/indexes/${idPath(id)}/activate`, { method: "POST" }),

  runEval: (suite: string) => request<{ job_id: string }>("/evals/suite", { method: "POST", body: JSON.stringify({ suite }) }),
  evalResults: () => request<{ results: string[] }>("/evals/results"),
  evalResult: (id: string) => request<EvalReport>(`/evals/results/${idPath(id)}`),

  backups: () => request<{ backups: BackupManifest[] }>("/backups"),
  createBackup: () => request<{ job_id: string }>("/backups", { method: "POST", body: "{}" }),
  verifyBackup: (id: string) => request<{ valid: boolean; problems: string[]; verified_at: string }>(`/backups/${idPath(id)}/verify`, { method: "POST" }),
  restoreBackup: (id: string) => request<{ job_id: string }>(`/backups/${idPath(id)}/restore`, { method: "POST", body: "{}" }),
  bundles: () => request<{ bundles: BundleManifest[] }>("/bundles"),
  createBundle: (profile: string, includeModels: string[]) => request<{ job_id: string }>("/bundles", {
    method: "POST", body: JSON.stringify({ profile, include_models: includeModels }),
  }),
  bundleDownloadURL: (id: string) => `${BASE}/bundles/${idPath(id)}/download`,

  credentials: () => request<{ credentials: CredentialMetadata[] }>("/provider-credentials"),
  createCredential: (value: { id: string; provider: string; label: string; secret: string }) => request<CredentialMetadata>("/provider-credentials", {
    method: "POST", body: JSON.stringify(value),
  }),
  deleteCredential: (id: string) => request<unknown>(`/provider-credentials/${idPath(id)}`, { method: "DELETE" }),
  gatewayKeys: () => request<GatewayKeyList>("/gateway/keys"),
  createGatewayKey: (value: { key_alias: string; models: string[]; max_budget?: number; tpm_limit?: number; rpm_limit?: number }) => request<IssuedGatewayKey>("/gateway/keys", {
    method: "POST", body: JSON.stringify(value),
  }),
  deleteGatewayKey: (id: string) => request<{ deleted: string }>(`/gateway/keys/${idPath(id)}`, { method: "DELETE" }),

  theme: () => request<import("./theme").Theme>("/theme"),
  branding: () => request<Branding>("/branding"),
  saveBranding: (value: Branding) => request<Branding>("/branding", { method: "PUT", body: JSON.stringify(value) }),
  applyBranding: () => request<unknown>("/workspace/branding/apply", { method: "POST", body: "{}" }),
  features: () => request<Features>("/features"),
  job: (id: string) => request<Job>(`/jobs/${idPath(id)}`),
  jobs: (limit = 50) => request<{ jobs: Job[] }>(`/jobs?limit=${limit}`),
  jobsEventsURL: () => `${BASE}/jobs/events`,
  cancelJob: (id: string) => request<Job>(`/jobs/${idPath(id)}/cancel`, { method: "POST" }),
  retryJob: (id: string) => request<{ job_id: string }>(`/jobs/${idPath(id)}/retry`, { method: "POST" }),
  waitJob: async (id: string, timeoutMs = 3_600_000): Promise<Job> => {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const job = await request<Job>(`/jobs/${idPath(id)}`);
      if (job.status === "succeeded") return job;
      if (job.status === "failed") throw new ApiError(500, job.error ?? `${job.kind} failed`);
      if (job.status === "canceled") throw new ApiError(409, `${job.kind} was canceled`, "canceled");
      await new Promise((resolve) => window.setTimeout(resolve, 2000));
    }
    throw new ApiError(408, "operation timed out");
  },
};
