# Embeddings and Versioned Indexes

Embedding profiles bind an immutable model revision to pooling,
normalization, distance metric, query/document prefixes, chunking strategy,
preprocessing version, and modalities. Dimensions are never guessed from
configuration; Control discovers them by probing the dedicated service.

Both certified hosts use the pinned `gemma-default` profile through
`embeddinggemma.c` v0.3.1. It produces 768-dimensional, L2-normalized text
vectors. Queries use `task: search result | query: ` and documents use
`title: none | text: `. CUDA runs the service in a private sibling container;
Apple Silicon runs the Metal binary as a loopback-only launchd service.

The provider is one appliance-wide setting. Changing any embedding identity
requires new indexes for every workspace. In the portal:

1. Open **Embeddings**, add or validate the desired profile, and choose
   **Activate everywhere**.
2. Control creates an immutable pending index version for every workspace and
   puts retrieval into maintenance mode.
3. Each workspace re-embeds every source document into a version-qualified
   pgvector namespace while reporting document and vector counts.
4. Control validates dimensions and counts, then changes every binding and the
   appliance provider state in one database transaction.
5. Any failure restores the previous provider and indexes before maintenance
   ends. A workspace with no documents is a valid zero-vector index.

EmbeddingGemma covers the default text-retrieval case. Advanced profiles may
select a model-registry entry served by Sovereign Runtime or an
OpenAI-compatible service. CUDA loads the optional runtime role in the runtime
container. Metal uses the signed host agent's constrained, checksum-verified
embedding role API; arbitrary host paths and engine flags are not accepted.

Existing versions cannot be edited. An active version cannot be deleted until
a replacement is active, and a per-workspace rebuild can use only the current
appliance profile. The `embedding` and `retrieval` eval suites provide portable
gates on both certified profiles.
