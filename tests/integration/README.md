# Integration tests (release gate)

Cross-component tests required before publishing a release (design.md §16):

- Chat through LiteLLM works
- Embeddings through LiteLLM work
- AnythingLLM can ingest documents
- AnythingLLM writes vectors to pgvector
- Retrieval works after restart
- Changing embedding profiles requires index rebuild
- Lazarus branding is applied

These tests run against a full `deploy/` stack brought up from the compose
files, not against mocks.
