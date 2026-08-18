# SovereignStack One-Click Installer Execution Plan

Date: 2026-08-18

This plan supersedes any priority label that treats container tooling as a
user-managed prerequisite. A release is not installable until a fresh
supported operating system can reach a verified first chat without
preinstalled developer or container tools.

## Product contract

The only permitted starting assumptions are:

- supported hardware and operating system;
- a local user who can approve clearly explained privilege prompts;
- network access, unless the user supplies a complete signed offline bundle;
- operating-system facilities present in a clean installation.

The installer must not assume Git, Homebrew, Docker, Compose, Colima, Lima, Go,
Python, Node.js, Rust, Pkl, model files, appliance configuration, or a prior
SovereignStack release.

The initial supported matrix is deliberately explicit:

- Apple Silicon macOS 15 or newer with at least 32 GiB unified memory;
- Ubuntu 24.04 x86_64 with a supported NVIDIA GPU and at least 24 GiB VRAM.

Unsupported hardware or operating systems must fail during preflight, before
large downloads or host changes, with the exact unmet requirement. “Any
environment” means any clean machine in this published support matrix; support
for another operating system is a separate tested matrix addition.

## Definition of done

From a clean supported host, one signed package or one copy-and-paste command
must:

1. Explain downloads, disk use, permissions, managed components, and expected
   duration before making host changes.
2. Install and verify every required runtime dependency.
3. Create an isolated container-engine environment without changing an
   unrelated global Docker context or configuration.
4. Download and verify the signed SovereignStack release, images, native
   services, and model assets.
5. Start services in dependency order and continuously report progress.
6. Open the local administrator claim flow and complete a real chat and
   embedding smoke test.
7. Resume safely after interruption, reboot, network loss, or a repeated
   installer invocation.
8. Support start, stop, repair, update, backup, restore, uninstall-preserve,
   and explicit purge without developer commands.

No release may be promoted when any required clean-machine cell is skipped.

## Bootstrap architecture

### Signed bootstrap executable

Replace the growing shell-only orchestration path with a small static
`sovereign-bootstrap` executable. The macOS package, Ubuntu package, and
scriptable download path invoke the same binary and therefore the same state
machine. The shell entry point may locate and verify the bootstrap executable,
but it must not contain a second implementation of installation policy.

The bootstrapper writes atomic structured events and a resumable journal under
`~/.sovereign/state`. Stages are:

1. platform and hardware preflight;
2. capacity and network preflight;
3. privilege approval;
4. container-engine selection and provisioning;
5. release and signature verification;
6. image and model download;
7. native service installation;
8. portal startup and administrator claim;
9. runtime readiness and smoke tests;
10. completed installation record.

Every stage records inputs, exact versions/digests, completion evidence, last
error, data-safety status, and the automatic or user recovery action. Human
output and JSON output are projections of the same events.

### Manifest-owned dependencies

The signed release manifest must describe every installer-owned dependency by
platform, including version, URL, digest, byte size, signer rule when
available, license, and minimum host version. This includes:

- Colima;
- Lima and required guest assets;
- Docker CLI;
- Docker Compose plugin;
- signature verification tooling;
- native lifecycle helpers;
- Ubuntu repository keys and package-version policy;
- NVIDIA Container Toolkit packages.

Release CI downloads and verifies every declared dependency from its public
URL before publishing. An unavailable, mutable, unsigned where signing is
expected, or digest-mismatched dependency blocks the release.

## Container-engine providers

All Docker and Compose use must go through one provider interface. The selected
provider is persisted with its executable paths, context, socket, versions,
ownership, and lifecycle policy. `install`, `sovereign`, hostd, repair,
offline-bundle, backup, update, and uninstall must use that provider rather
than invoking an ambient `docker` executable.

### macOS: managed Colima is the default

When no compatible daemon exists, the bootstrapper downloads the pinned
Apple-Silicon Colima, Lima, Docker CLI, and Compose artifacts into a versioned
directory under `~/.sovereign/tools`. It does not install Homebrew and does not
edit the user’s shell profile.

It creates a dedicated `sovereign` Colima profile using Apple’s Virtualization
framework, reviewed CPU/memory/disk values, a writable mount limited to the
SovereignStack data root, and `--activate=false`. The installer maintains its
own Docker configuration directory and always passes the persisted
`colima-sovereign` context. It never changes the user’s active global context.

The provider owns start, stop, repair, and optional removal of this profile.
Ordinary uninstall preserves it by default when appliance data remains. Purge
previews and separately confirms deletion of the managed VM and its data.

If Docker Desktop or another compatible daemon is already running, the
installer can use it after compatibility probes. Existing-engine mode never
stops, upgrades, removes, or reconfigures that engine. A stopped or
incompatible engine does not prevent offering the managed Colima default.

### Ubuntu: managed Docker and NVIDIA integration

On Ubuntu, preflight distinguishes NVIDIA hardware, loaded driver, Docker
Engine, Compose, NVIDIA Container Toolkit, daemon state, and GPU container
access. The installer obtains only the privilege required for missing system
components and explains each system change before approval.

The managed path installs pinned/supported Docker Engine, Compose, and NVIDIA
Container Toolkit packages from authenticated repositories, configures the
NVIDIA runtime, enables the daemon, and verifies an actual GPU container. When
a supported driver installation or upgrade requires reboot, the journal is
committed first and a narrowly scoped continuation service resumes at the
engine-verification stage after reboot. Existing compatible drivers and
engines are reused and never replaced gratuitously.

Secure Boot, unsupported GPUs, missing kernel support, repository failure, and
required reboot are distinct states with explicit recovery. The installer
must not claim success before `nvidia-smi` works on the host and in a pinned
container.

## Compatibility probes

An engine is usable only after all of these pass with the persisted provider:

- daemon API and architecture;
- Compose v2;
- pull and inspect of a small digest-pinned test image;
- writable named volume;
- bind mount from the SovereignStack data root;
- loopback-only published port;
- container-to-host `host.docker.internal` connectivity on macOS;
- GPU enumeration and a minimal CUDA operation on the CUDA profile;
- adequate real host space and engine data-disk space.

Probe resources are uniquely labeled and removed after the test. Failure never
removes unrelated resources.

## User experience

The signed macOS package installs a small launcher that displays bootstrap
events, permission requests, progress, remaining work, and recovery. Package
installation must not start an invisible background job whose only failure is
buried in a log. The headless command renders the same events in the terminal.

The Ubuntu package and headless path print one resumable status command and one
portal URL. Both platforms keep the user informed at least every 30 seconds.
Every error names the component, cause, whether existing data is safe, what is
being retried, and one next action.

## Lifecycle requirements

- `install` provisions dependencies and appliance assets.
- `up` uses installed assets and starts a managed engine when necessary; it
  does not contact registries by default.
- `down` stops all SovereignStack containers and native model services. It does
  not stop an unrelated engine. It may stop a managed Colima profile when no
  other managed workload uses it.
- `repair` re-runs provider probes and reconciles missing tools, contexts,
  services, images, configuration, and interrupted downloads.
- `update` verifies a backup, stages new tools and release assets alongside the
  old version, switches only after health and smoke gates, and rolls back both
  appliance and provider metadata on failure.
- `uninstall` preserves user data and managed-engine data by default.
- `uninstall --purge` previews every path, volume, and managed VM before a
  second explicit confirmation. It never removes shared engines or unrelated
  resources.

## Test and release gates

### Hermetic repository tests

Fake executables and isolated homes cover every detection branch, command,
state transition, interruption point, and rollback without reading host
Docker configuration. Tests assert exact context propagation for every Docker
and Compose invocation. Shell tests remain for the downloader; the bootstrap
state machine receives unit and integration coverage in its implementation
language.

### Disposable clean machines

Reset to a pristine snapshot before every journey:

- macOS with no package manager or container tools;
- macOS with Docker Desktop;
- macOS with an unrelated active Docker context and existing config;
- macOS with a stopped or damaged managed Colima profile;
- Ubuntu with GPU driver only;
- Ubuntu with neither driver nor container tooling, including reboot/resume;
- Ubuntu with an existing compatible CUDA container stack.

Each cell runs install, first administrator, first chat, embedding, backup,
down/up while offline, same-version reinstall, interrupted update recovery,
uninstall-preserve, reinstall with preserved data, and purge isolation.

A local macOS virtual machine is suitable for bootstrap, dependency, and
lifecycle coverage when nested virtualization is available. It is not accepted
as the sole Metal inference gate unless the guest exposes a verified usable
Metal compute device. Final Metal validation runs on clean bare-metal Apple
Silicon; CUDA validation runs on a clean supported NVIDIA host.

### Release workflow

Candidate packages are installed from the exact artifacts that will be
published, not from a source checkout. Promotion occurs only after the full
matrix reports artifact digests, provider versions, first-chat evidence,
embedding evidence, persistence evidence, cleanup evidence, and redacted logs.

## Delivery order

1. Add provider-neutral engine state and route every Docker/Compose invocation
   through it.
2. Add manifest-pinned macOS tool artifacts and managed Colima provisioning.
3. Add compatibility probes, capacity checks, repair, and lifecycle ownership.
4. Add Ubuntu privileged provisioning and journaled driver reboot/resume.
5. Implement the shared bootstrap state machine and adapt package entry points.
6. Add the macOS progress launcher and headless structured output.
7. Build disposable VM automation and clean bare-metal release runners.
8. Run the full matrix, fix every failure, and only then promote a release.

The open installer-hardening PR remains a prerequisite, not the completion of
this plan. It must not be presented as a finished one-click installer until
these gates pass.

## Implementation status (2026-08-18)

Delivery items 1 and 2 are implemented. The macOS portion of item 3 now runs
digest-pinned API, architecture, Compose, engine-capacity, named-volume,
bind-mount, loopback-port, and container-to-host probes. Managed Colima can be
started, stopped, repaired from verified cached artifacts, and deleted only
through the previewed `--purge --yes` boundary. Same-platform offline bundles
carry the pinned installer dependencies, Cosign, and Docker Compose Sigstore
bundle, including the Colima guest disk image; a hermetic fresh install proves
that this path makes no network call.

Item 3 still needs disposable-VM repetition and damaged-VM validation beyond
the hermetic recovery tests. Items 4 through 8 remain open, including Ubuntu
driver/container provisioning, the shared bootstrap state machine, visible
package progress, and the clean-machine release matrix.
