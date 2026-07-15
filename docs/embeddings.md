# Embeddings and Versioned Indexes

Embedding profiles bind an immutable model revision to pooling,
normalization, distance metric, query/document prefixes, chunking strategy,
preprocessing version, and modalities. Dimensions are never guessed from
configuration; Control discovers them from the loaded runtime manifest.

The CUDA default is the pinned LCO Omni profile for text, image, and audio. A
pinned Nomic v1.5 text profile remains available as a compact fallback. Metal
uses the Nomic Q8 GGUF profile with `search_query:` and `search_document:`
prefixes.

Changing any embedding identity requires a new index. In Control:

1. Open **Knowledge** and validate or activate the desired profile.
2. Choose an AnythingLLM workspace and start a rebuild.
3. Control creates an immutable pending index version and puts that workspace
   in maintenance mode.
4. The workspace re-embeds every source document into a version-qualified
   pgvector namespace while reporting document and vector counts.
5. Control validates dimensions and nonzero counts, atomically activates the
   new binding, and exits maintenance mode.
6. Any failure restores the previous runtime profile and active index.

Existing versions cannot be edited. An active version cannot be deleted until
a replacement is active. The `embedding` and `retrieval` eval suites provide
portable gates; `omni-embedding` adds image/audio and cross-modal checks on the
CUDA profile.
