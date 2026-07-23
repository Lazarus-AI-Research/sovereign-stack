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

Changing any embedding identity requires a new index. In Control:

1. Open **Knowledge** and validate or activate the desired profile.
2. Choose an AnythingLLM workspace and start a rebuild.
3. Control creates an immutable pending index version and puts that workspace
   in maintenance mode.
4. The workspace re-embeds every source document into a version-qualified
   pgvector namespace while reporting document and vector counts.
5. Control validates dimensions and nonzero counts, atomically activates the
   new binding, and exits maintenance mode.
6. Any failure preserves the previous active index.

Existing versions cannot be edited. An active version cannot be deleted until
a replacement is active. The `embedding` and `retrieval` eval suites provide
portable gates on both certified profiles.
