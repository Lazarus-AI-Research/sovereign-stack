# Backup and Restore

Normal backups contain PostgreSQL databases (`sovereign_control`, `litellm`,
`workspace`, `phoenix`, and `vectors`) plus product config and branding. They
exclude model weights, Hugging Face caches, compilation caches, `.env`, the
vault key, agent token, gateway configuration containing provider secrets, and
first-admin claim material.

Create and verify from the CLI:

```bash
sovereign backup
```

Or use **Resilience** in Control. Each backup directory has a JSON manifest
with byte sizes and SHA-256 hashes. Verification recomputes every entry before
restore.

Restore is deliberately explicit:

```bash
sovereign restore 20260714-120000 --yes
```

The job rejects an invalid manifest, restores each database through the fixed
pgvector tool image, and then overlays backed-up config and branding. Keep a
copy of the current backup outside the appliance before a destructive recovery.
Model weights must be restored separately or downloaded again.
