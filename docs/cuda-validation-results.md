# CUDA Validation Results — Lazarus Sovereign Stack runtime (quixi-3090-02)

Status as of 2026-07-13. Box: 8× RTX 3090 (24GB); all appliance runs use
`CUDA_VISIBLE_DEVICES=0` — single-GPU is the product topology. Stack:
`~/svenv` with vLLM 0.25.0 CUDA wheels, `-e ~/sovereign-vllm` (Lazarus
overlay), `-e ~/SovereignStack/evals`. Runner: `~/run-cuda-validation.sh
single|multi`; all reports land in `~/cuda-validation/`.

## What passed

| Gate | Result | Report |
|---|---|---|
| Single-role conformance (gemma generation only) | **13 passed / 1 skipped** (embedding disabled by config) | `conformance-cuda-single.json` |
| M12 multimodal embedding validation (LCO alone, port 8976) | **16/16** — text, image, audio | `mm-validation.json` (harness: `validate-mm.py`) |
| Multi-role conformance (gemma + LCO, one process) | _pending — run in progress, section updated below_ | `conformance-cuda-multi.json` |

Models are the locked user decisions: generation `google/gemma-4-E2B-it`
(alias `assistant-large`), embedding
`LCO-Embedding/LCO-Embedding-Omni-3B-2605` (alias
`embedding-omni-default`). Embedding dimensionality is **probed** at
startup (2048 for LCO), never hardcoded; L2 norm verified ≈ 1.0.

Highlights from the 16 multimodal checks: image and audio embeddings
return the same 2048-dim L2-normalized vectors as text; a 3×3
image↔caption cosine matrix puts every image nearest its own caption
(semantic alignment, not just shape); remote media URLs are rejected with
400 (sovereignty rule — the runtime never fetches media).

## Multi-role conformance (this run)

_Pending — being filled in by the run currently executing._

## The two bugs fixed

### 1. flashinfer JIT could not find ninja → generation role dead on CUDA

**Symptom:** single-role start failed with `MODEL_LOAD_FAILED`;
underneath, `FileNotFoundError: 'ninja'` from flashinfer JIT-compiling its
CUDA sampling kernel at engine warmup.

**Cause:** the supervisor launches `run-sovereign-runtime` directly (venv
not activated), so the interpreter's `bin/` dir — where ninja lives — is
not on `PATH`, and `shutil.which('ninja')` returns `None`.

**Fix:** `lazarus/appliance/launcher.py` prepends `sys.executable`'s bin
dir to `PATH` before the engine subprocess is spawned, so the appliance
finds its own bundled toolchain regardless of launch context. Commit
`611fcff` in `~/sovereign-vllm`.

### 2. The M12 bug: LCO fails to load as a pooling model

**Symptom:** `AttributeError: 'Qwen2_5OmniProcessor' object has no
attribute '_get_num_multimodal_tokens'` at engine start, unaffected by
`--limit-mm-per-prompt '{"image":0,...}'`.

**Root cause:** LCO is a *thinker-only* Qwen2.5-Omni checkpoint —
`architectures=["Qwen2_5OmniThinkerForConditionalGeneration"]`, thinker
config at the top level of config.json, weights unprefixed (`model.*`,
`visual.*`, `audio_tower.*`). Pinned vLLM 0.25 has no registry entry for
that arch string, so it silently fell back to the **Transformers modeling
backend**, whose multimodal path calls
`Processor._get_num_multimodal_tokens` — a method the pinned transformers'
`Qwen2_5OmniProcessor` does not implement. That's why the mm-limit flag
couldn't suppress it: the failure was in the fallback backend's processor
plumbing, not in multimodal budgeting.

**Fix (fully out of tree — see "M12 plugin approach"):** register the
thinker arch onto vLLM's *native* omni-thinker model. Commit `ff4b419`.

### Related fix found on the way (worth knowing about)

`848ce92`: vLLM 0.25 renamed `--override-pooler-config` →
`--pooler-config` and `PoolerConfig.normalize` → `use_activation`. The
appliance's unknown-flag filter silently dropped the old spelling, so the
role config's `pooling: last` / `normalization: l2` never reached the
engine. macOS conformance had passed only because engine defaults happened
to match. Now the appliance emits the 0.25 spellings.

## The M12 plugin approach

All of it lives in `~/sovereign-vllm/lazarus/models/embedding/lco_omni/`
plus small appliance hooks — no edits to vLLM site-packages, no fork
patches needed for this feature.

1. **Arch registration via `vllm.general_plugins` entry point**
   (declared in `pyproject.toml`). vLLM loads general plugins in *every*
   process, including engine-core workers, so the registration survives
   process spawning. The plugin maps
   `Qwen2_5OmniThinkerForConditionalGeneration` onto
   `LCOOmniThinkerForConditionalGeneration` — a subclass of vLLM's native
   thinker model whose `WeightsMapper` additionally accepts the
   unprefixed checkpoint tensor names (prefix rules are ordered, so
   full-omni names still map identically). It is a no-op the day a future
   vLLM registers the arch natively.

2. **`normalize_thinker_config` hf_overrides callable** — wraps the bare
   top-level thinker config into the full `Qwen2_5OmniConfig` shape that
   every native code path (mrope detection, max_model_len resolution,
   processing info) expects, carrying the checkpoint's dtype so
   `dtype=auto` resolves correctly. The appliance backend injects it for
   embedding roles because callables can't be passed through CLI argv.

3. **Extended `/v1/embeddings` schema** (commit `8070bd2`, per the runtime
   contract): media arrives as a chat-style `messages` array (alternative
   to OpenAI `input`), **base64 data URIs only** — any remote URL is
   rejected 400 at the API layer. The manifest advertises an embedding
   modality only after a real probe request (tiny PNG / 0.5s WAV)
   round-trips with the same dimensionality the text probe established
   (contract §10.1: probed, never assumed).

4. **Audio dependencies** (commit `66e3484`): declared as a project extra
   using `soundfile` + `soxr` instead of `vllm[audio]`, because
   `vllm[audio]` pulls torchcodec, which dlopens system ffmpeg libs and
   raises `OSError` on import on no-root hosts (vLLM only guards
   `ImportError`). soundfile bundles libsndfile.

## Remaining gaps

- **Video embeddings**: not supported. Video decode requires torchcodec,
  which is deliberately uninstalled (needs system ffmpeg libs; no sudo on
  this box). The manifest honestly advertises `[text, image, audio]`
  because modalities are probed, not assumed.
- **Multi-GPU topologies**: untested; single-GPU (`CUDA_VISIBLE_DEVICES=0`)
  is the product topology and the only one validated here.
- **No load/soak testing on CUDA**: conformance and the 16-check
  multimodal suite are functional gates; sustained concurrency,
  generation-queue throttling of the embedding role
  (`throttle_when_generation_queue_above`), and long-run memory stability
  were not exercised under load.
- **`VLLM_BACKEND=cuda` env var** set by the runner is not a real vLLM
  variable (vLLM warns "Unknown environment variable"); harmless, but the
  runner could drop it.
