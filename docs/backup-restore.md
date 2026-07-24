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

Or use **Backups & Recovery** in the portal. Each backup directory has a JSON manifest
with byte sizes and SHA-256 hashes. Verification recomputes every entry before
restore.

Restore is deliberately explicit:

```bash
sovereign restore 20260714-120000 --yes
```

The job rejects an invalid manifest and creates a fresh verified rollback point
before changing live data. It then overlays backed-up config and branding and
restores each database through the fixed pgvector tool image. If either stage
fails, the fresh rollback point is reapplied automatically. Keep a separate
off-appliance copy for disaster recovery; model weights must still be restored
separately or downloaded again.
