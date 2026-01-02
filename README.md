# semanticRAG

A small **Go** project that demonstrates a basic **RAG (Retrieval‑Augmented Generation)** pipeline:

1. Upload a document (currently best with plain text)
2. Chunk it into smaller passages
3. Generate embeddings (Hugging Face Inference API)
4. Store vectors + text in **ChromaDB**
5. Ask questions later and retrieve the most relevant chunks
6. Send retrieved context + question to **Gemini** to generate an answer

---

## What’s in this repo

- **ChromaDB** as the vector database (API v2)
- **Hugging Face Inference** for embeddings (default model: `sentence-transformers/all-MiniLM-L6-v2`)
- **Gemini** for answer generation (via `google.golang.org/genai`)
- Simple endpoints for uploading and chatting
- A lightweight **embedding cache** for development (so you don’t re‑embed the same doc every run)

---

## Requirements

- Go **1.25+**
- A running ChromaDB instance (v2 API)

---

## Run ChromaDB locally

The code expects Chroma at `http://localhost:8000` by default.

Example:

```bash
chroma run --path ./chroma-data --host 0.0.0.0 --port 8000
```

Healthcheck (should return OK):

```bash
curl -i http://localhost:8000/api/v2/healthcheck
```

---

## Configuration

The server loads `.env` and `.env.local` if present, and then reads environment variables.

### Required

- `HF_API_KEY` — Hugging Face API token (for embeddings)
- `GEMINI_API_KEY` — Gemini API key

### Optional (with defaults)

- `EMBED_MODEL_NAME` (default: `sentence-transformers/all-MiniLM-L6-v2`)
- `LLM_MODEL_NAME` (default: `gemini-2.5-flash`)
- `CHROMA_DB_HOST` (default: `http://localhost:8000`)
- `PORT` (default: `8080`)
- `CHUNK_LENGTH` (reserved)
- `RAG_DATA_DIR` (default: `./data`)

Example `.env`:

```bash
HF_API_KEY=hf_********************************
GEMINI_API_KEY=********************************
CHROMA_DB_HOST=http://localhost:8000
LLM_MODEL_NAME=gemini-2.5-flash
PORT=8080
```

---

## Run the server

```bash
go run .
```

The server listens on `:8080` unless you changed `PORT`.

---

## API

### `POST /health`

Simple health check for the Go server:

```bash
curl -i -X POST http://localhost:8080/health
```

### `POST /upload`

Uploads a single file and indexes it into Chroma.

- Expects a multipart form field named **`files`**
- Only **one** file is accepted
- Each chunk is stored with metadata:
  - `context` = original filename (used later for filtering)
  - `doc_id` = chunk ID (e.g. `filename-0`)
  - `len` = chunk length

Example:

```bash
curl -X POST http://localhost:8080/upload \
  -F "files=@./example.txt"
```

### `POST /chat`

Queries indexed chunks and uses Gemini to answer.

Accepts JSON:

```json
{
  "context": "example.txt",
  "query": "What is this document about?"
}
```

Example:

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"context":"example.txt","query":"What is this document about?"}'
```

Response:

```json
{
  "answer": "...",
  "context": ["retrieved chunk 1", "retrieved chunk 2", "..."]
}
```

### `POST /rechunk`

Returns the computed chunks for an uploaded file (useful for debugging chunking):

```bash
curl -X POST http://localhost:8080/rechunk \
  -F "files=@./example.txt"
```

---

## Embedding cache (dev/testing)

To avoid re‑embedding the same document during testing, the upload flow supports an on‑disk cache.

- Cache file: `tmp/embeddings_cache.json`
- Controlled by `EMBED_CACHE_MODE`:

| Mode | Behaviour |
|------|-----------|
| `auto` (default) | Load cache if it matches; otherwise embed and save |
| `load` | Only load cache; error if not present/matching |
| `off`  | Always call embedding API (no cache) |

Typical workflow:

1) First upload (creates cache):

```bash
EMBED_CACHE_MODE=auto go run .
```

2) Subsequent runs (no embedding API calls):

```bash
EMBED_CACHE_MODE=load go run .
```

> Note: caching assumes chunk IDs are stable (deterministic). If you change chunking or chunk IDs, regenerate the cache.

---

## Notes / limitations

- Upload currently treats file bytes as text. For **PDF/DOCX**, add a text‑extraction step (e.g. `pdftotext` or a Go library) before chunking/embedding.
- Chunking is intentionally simple (sentence-ish splitting). It’s a demo baseline, not production chunking.

---

## License

No license file included yet (add one if you plan to open‑source publicly).
