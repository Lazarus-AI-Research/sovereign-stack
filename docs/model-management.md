# Model Management

The product registry supports pinned Hugging Face and ModelScope repositories,
local/offline artifacts, OpenAI-compatible remote endpoints, and cloud presets
for OpenAI, Anthropic, and Gemini. Catalog models require immutable revisions;
local release defaults also carry artifact checksums where a discrete file is
used.

Use **Models** in Control to register, load, test, or remove routes. Local model
loads rewrite `runtime.yaml`, restart only the runtime through the restricted
proxy, wait for readiness, and require a smoke test. Remote/cloud entries
regenerate the private LiteLLM configuration and restart the gateway without
changing the runtime.

Provider API keys are separate encrypted credential records. A model references
a credential ID; registry YAML and API list responses never contain the secret.
The generated gateway config is mode `0600` and is excluded from backups and
release archives.

Stable aliases such as `assistant-large`, `embedding-omni-default`, and
`embedding-text-compact` insulate the workspace from engine-specific model
names. Loading an unvalidated model is allowed with a visible warning so the
operator can test new hardware/model combinations without weakening the
shipped defaults.
