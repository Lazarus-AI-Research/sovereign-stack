# Sovereign Workspace modifications

This image derives from AnythingLLM 1.15.0, revision
`1c2b2a7523b83b3640858c2aaf9f9e0ff8847536`.

SovereignStack adds authenticated, internal-only index and identity adapters.
Control mirrors users, roles, and workspace membership through that private
adapter and receives AnythingLLM's existing single-use Simple SSO tokens; user
passwords remain exclusively in Control. The preload also patches pgvector so
workspace namespaces resolve through `vectors.workspace_bindings`. No
AnythingLLM admin API is exposed at ingress.
