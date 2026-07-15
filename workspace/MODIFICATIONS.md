# Sovereign Workspace modifications

This image derives from AnythingLLM 1.15.0, revision
`1c2b2a7523b83b3640858c2aaf9f9e0ff8847536`.

SovereignStack adds an authenticated, internal-only index rebuild endpoint and
patches the pgvector provider at process startup so workspace namespaces resolve
through `vectors.workspace_bindings`. It does not expose the AnythingLLM admin
API or change the user-facing workspace API.
