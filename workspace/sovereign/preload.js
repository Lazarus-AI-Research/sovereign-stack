/*
 * Sovereign versioned-index integration for the pinned AnythingLLM image.
 * Loaded only in the main server process through NODE_OPTIONS. This file is
 * deliberately self-contained so upstream updates fail visibly in image tests.
 */
(() => {
  if (process.argv[1] !== "/app/server/index.js") return;
  if (process.env.SOVEREIGN_VERSIONED_INDEXES !== "true") return;

  const crypto = require("crypto");
  const http = require("http");
  const pg = require("pg");
  const { PGVector } = require("/app/server/utils/vectorDbProviders/pgvector");

  const table = process.env.PGVECTOR_TABLE_NAME || "anythingllm_vectors";
  const pool = new pg.Pool({ connectionString: process.env.PGVECTOR_CONNECTION_STRING });

  async function binding(slug) {
    const result = await pool.query(
      `SELECT b.maintenance, b.active_index_version::text AS active_index_version,
              i.query_prefix, i.document_prefix
         FROM vectors.workspace_bindings b
         LEFT JOIN vectors.index_versions i ON i.id = b.active_index_version
        WHERE b.provider_slug = $1`,
      [slug]
    );
    return result.rows[0] || null;
  }

  async function resolveNamespace(namespace) {
    if (!namespace || namespace.includes("::")) return namespace;
    const current = await binding(namespace);
    if (!current) return namespace;
    if (current.maintenance) return null;
    if (!current.active_index_version) return namespace;
    return `${namespace}::${current.active_index_version}`;
  }

  async function ensureVectorSchema() {
    await pool.query(`CREATE EXTENSION IF NOT EXISTS vector`);
    const exists = await pool.query(`SELECT to_regclass($1) AS name`, [`public.${table}`]);
    if (!exists.rows[0]?.name) return;
    await pool.query(`ALTER TABLE "${table}" ALTER COLUMN embedding TYPE vector`);
    await pool.query(`ALTER TABLE "${table}" ADD COLUMN IF NOT EXISTS index_version UUID`);
  }

  const originalCreateTableSql = PGVector.prototype.createTableSql;
  PGVector.prototype.createTableSql = function () {
    return `CREATE TABLE IF NOT EXISTS "${table}" (id UUID PRIMARY KEY, namespace TEXT, embedding vector, metadata JSONB, index_version UUID, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`;
  };

  function patchNamespaceMethod(name, maintenanceResult) {
    const original = PGVector.prototype[name];
    if (typeof original !== "function") throw new Error(`AnythingLLM pgvector method ${name} is missing`);
    PGVector.prototype[name] = async function (namespace, ...args) {
      const resolved = await resolveNamespace(namespace);
      if (!resolved) return typeof maintenanceResult === "function" ? maintenanceResult() : maintenanceResult;
      return original.call(this, resolved, ...args);
    };
  }

  patchNamespaceMethod("hasNamespace", false);
  patchNamespaceMethod("namespaceCount", 0);
  patchNamespaceMethod("addDocumentToNamespace", () => ({
    vectorized: false,
    error: "Workspace retrieval is in maintenance mode while its embedding index is rebuilt.",
  }));
  patchNamespaceMethod("deleteDocumentFromNamespace", false);

  const originalSearch = PGVector.prototype.performSimilaritySearch;
  PGVector.prototype.performSimilaritySearch = async function (options = {}) {
    const resolved = await resolveNamespace(options.namespace);
    if (!resolved) return { contextTexts: [], sources: [], message: "index maintenance" };
    return originalSearch.call(this, { ...options, namespace: resolved });
  };

  const originalUpdate = PGVector.prototype.updateOrCreateCollection;
  PGVector.prototype.updateOrCreateCollection = async function (options = {}) {
    const result = await originalUpdate.call(this, options);
    const match = String(options.namespace || "").match(/::([0-9a-f-]{36})$/i);
    if (match) {
      await pool.query(`UPDATE "${table}" SET index_version=$1 WHERE namespace=$2 AND index_version IS NULL`, [
        match[1],
        options.namespace,
      ]);
    }
    return result;
  };

  async function refreshPrefixes() {
    const result = await pool.query(
      `SELECT i.query_prefix, i.document_prefix
         FROM vectors.workspace_bindings b
         JOIN vectors.index_versions i ON i.id=b.active_index_version
        WHERE NOT b.maintenance AND i.status='active'
        ORDER BY i.activated_at DESC LIMIT 1`
    );
    const active = result.rows[0];
    if (!active) return;
    process.env.GENERIC_OPEN_AI_EMBEDDING_QUERY_PREFIX = active.query_prefix || "";
    process.env.GENERIC_OPEN_AI_EMBEDDING_PASSAGE_PREFIX = active.document_prefix || "";
  }

  function authorized(request) {
    const expected = process.env.WORKSPACE_INDEX_ADMIN_TOKEN || "";
    const actual = (request.headers.authorization || "").replace(/^Bearer\s+/i, "");
    if (!expected || actual.length !== expected.length) return false;
    return crypto.timingSafeEqual(Buffer.from(actual), Buffer.from(expected));
  }

  function readJSON(request) {
    return new Promise((resolve, reject) => {
      let raw = "";
      request.on("data", (chunk) => {
        raw += chunk;
        if (raw.length > 1024 * 1024) request.destroy();
      });
      request.on("end", () => {
        try { resolve(JSON.parse(raw || "{}")); } catch (error) { reject(error); }
      });
      request.on("error", reject);
    });
  }

  async function rebuild(body) {
    const slug = String(body.workspace_slug || "");
    const indexVersion = String(body.index_version || "");
    if (!/^[a-zA-Z0-9_-]+$/.test(slug) || !/^[0-9a-f-]{36}$/i.test(indexVersion)) {
      throw new Error("workspace_slug and a UUID index_version are required");
    }
    process.env.GENERIC_OPEN_AI_EMBEDDING_QUERY_PREFIX = String(body.query_prefix || "");
    process.env.GENERIC_OPEN_AI_EMBEDDING_PASSAGE_PREFIX = String(body.document_prefix || "");

    const { Workspace } = require("/app/server/models/workspace");
    const { Document } = require("/app/server/models/documents");
    const { fileData } = require("/app/server/utils/files");
    const { getVectorDbClass } = require("/app/server/utils/helpers");
    const workspace = await Workspace.get({ slug });
    if (!workspace) throw new Error(`workspace ${slug} was not found`);
    const documents = await Document.forWorkspace(workspace.id);
    const namespace = `${slug}::${indexVersion}`;
    await ensureVectorSchema();
    await pool.query(`DELETE FROM "${table}" WHERE namespace=$1`, [namespace]);
    await pool.query(
      `UPDATE vectors.index_versions SET document_count=$2, processed_documents=0,
              vector_count=0, started_at=COALESCE(started_at,now()), error=NULL
        WHERE id=$1`,
      [indexVersion, documents.length]
    );

    const failures = [];
    let processed = 0;
    for (const document of documents) {
      const data = await fileData(document.docpath);
      if (!data) {
        failures.push(`${document.docpath}: source document unavailable`);
        continue;
      }
      const VectorDb = getVectorDbClass("pgvector");
      const result = await VectorDb.addDocumentToNamespace(
        namespace,
        { ...data, docId: document.docId },
        document.docpath,
        true
      );
      if (!result?.vectorized) failures.push(`${document.docpath}: ${result?.error || "embedding failed"}`);
      else processed += 1;
      const count = await pool.query(`SELECT count(*)::bigint AS count FROM "${table}" WHERE namespace=$1`, [namespace]);
      await pool.query(
        `UPDATE vectors.index_versions SET processed_documents=$2, vector_count=$3 WHERE id=$1`,
        [indexVersion, processed, Number(count.rows[0].count)]
      );
    }
    const count = await pool.query(`SELECT count(*)::bigint AS count FROM "${table}" WHERE namespace=$1`, [namespace]);
    return {
      workspace_slug: slug,
      index_version: indexVersion,
      document_count: documents.length,
      processed_documents: processed,
      vector_count: Number(count.rows[0].count),
      failures,
    };
  }

  function workspaceUUID(slug) {
    const hex = crypto.createHash("sha256").update(`sovereign-workspace:${slug}`).digest("hex").slice(0, 32);
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-5${hex.slice(13, 16)}-a${hex.slice(17, 20)}-${hex.slice(20)}`;
  }

  async function listWorkspaces() {
    const { Workspace } = require("/app/server/models/workspace");
    const rows = await Workspace.where({}, null, { name: "asc" });
    return rows.map((workspace) => ({
      id: workspaceUUID(workspace.slug),
      upstream_id: workspace.id,
      name: workspace.name,
      slug: workspace.slug,
    }));
  }

  const server = http.createServer(async (request, response) => {
    response.setHeader("Content-Type", "application/json");
    if (request.method === "GET" && request.url === "/health") {
      response.end(JSON.stringify({ status: "ok", versioned_indexes: true }));
      return;
    }
    if (request.method === "GET" && request.url === "/internal/indexes/workspaces") {
      if (!authorized(request)) {
        response.statusCode = 401;
        response.end(JSON.stringify({ error: "unauthorized" }));
        return;
      }
      try {
        response.end(JSON.stringify({ workspaces: await listWorkspaces() }));
      } catch (error) {
        response.statusCode = 500;
        response.end(JSON.stringify({ error: error.message }));
      }
      return;
    }
    if (request.method !== "POST" || request.url !== "/internal/indexes/rebuild") {
      response.statusCode = 404;
      response.end(JSON.stringify({ error: "not found" }));
      return;
    }
    if (!authorized(request)) {
      response.statusCode = 401;
      response.end(JSON.stringify({ error: "unauthorized" }));
      return;
    }
    try {
      response.end(JSON.stringify(await rebuild(await readJSON(request))));
    } catch (error) {
      response.statusCode = 500;
      response.end(JSON.stringify({ error: error.message }));
    }
  });

  ensureVectorSchema().catch((error) => console.warn("Sovereign vector schema pending:", error.message));
  refreshPrefixes().catch(() => {});
  setInterval(() => refreshPrefixes().catch(() => {}), 5000).unref();
  server.listen(Number(process.env.SOVEREIGN_INDEX_ADMIN_PORT || 3011), "0.0.0.0", () => {
    console.log("Sovereign versioned-index administration listening on 3011");
  });

  // Retain a reference for image-level compatibility tests.
  global.__sovereignOriginalCreateTableSql = originalCreateTableSql;
})();
