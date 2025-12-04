package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	contentStr, fileName := getFileContents(w, r)
	if contentStr == "" {
		return
	}

	// chunk the content of the file
	chunks := simpleChunkDocument(fileName, contentStr, 2)

	// embed
	ctx := r.Context()
	embedder, err := NewEmbedderFromEnv()
	if err != nil {
		http.Error(w, "failed to NewEmbedderFromEnv", http.StatusInternalServerError)
		return
	}

	embeds, err := embedder.Embed(ctx, chunks)
	if err != nil {
		if err != nil {
			http.Error(w, "failed to Embed chunks", http.StatusInternalServerError)
			return
		} /* handle */
	}
	fmt.Println(embeds)
	// Next step (later): upsert {id, document, embedding, metadata} into ChromaDB.

}

func promptHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: implement prompt handling
	json.NewEncoder(w).Encode(map[string]string{"message": "Prompt endpoint not yet implemented"})
}

func getFileContents(w http.ResponseWriter, r *http.Request) (string, string) {
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return "", ""
	}

	var reChunkFile string
	if err := json.Unmarshal(b, &reChunkFile); err != nil {
		http.Error(w, "error body text not a string", http.StatusBadRequest)
		return "", ""
	}

	f, err := os.Open(reChunkFile)
	if err != nil {
		http.Error(w, "failed to open file", http.StatusNotFound)
		fmt.Println("err here: ", err.Error())
		return "", ""
	}
	defer f.Close()

	contentBytes, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return "", ""
	}
	contentStr := string(contentBytes)
	return contentStr, reChunkFile
}

func rechunkHandler(w http.ResponseWriter, r *http.Request) {
	contentStr, fileName := getFileContents(w, r)
	if contentStr == "" {
		return
	}

	// chunk the content of the file
	chunks := simpleChunkDocument(fileName, contentStr, 2)

	result := struct {
		Chunks []Chunk
	}{
		chunks,
	}

	resStr, err := json.Marshal(result)
	if err != nil {
		http.Error(w, "failed to marshall response", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(resStr)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", requirePost(uploadHandler))   // POST
	mux.HandleFunc("/prompt", requirePost(promptHandler))   // POST
	mux.HandleFunc("/rechunk", requirePost(rechunkHandler)) // POST

	log.Fatal(http.ListenAndServe(":8081", mux))
}

func requirePost(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

// simpleChunkDocument splits the text into sentences and groups them into chunks
// of up to sentencesPerChunk sentences each.
func simpleChunkDocument(docID, text string, sentencesPerChunk int) []Chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")

	// Naive sentence split on "."
	rawSentences := strings.Split(text, ".")
	var sentences []string
	for _, s := range rawSentences {
		s = strings.TrimSpace(s)
		if s != "" {
			sentences = append(sentences, s+".")
		}
	}

	var chunks []Chunk
	var current []string
	index := 0

	for _, s := range sentences {
		current = append(current, s)
		if len(current) >= sentencesPerChunk {
			chunkText := strings.Join(current, " ")
			chunks = append(chunks, Chunk{
				ID:   fmt.Sprintf("%s-%d", docID, index),
				Text: chunkText,
			})
			index++
			current = nil
		}
	}
	if len(current) > 0 {
		chunkText := strings.Join(current, " ")
		chunks = append(chunks, Chunk{
			ID:   fmt.Sprintf("%s-%d", docID, index),
			Text: chunkText,
		})
	}

	return chunks
}

// embeddings.go
// Go-only embeddings client for sentence-transformers/all-MiniLM-L6-v2 hosted online.
// Supports Hugging Face Inference API or a generic TEI (/embed) endpoint, chosen via env.
//
// Env:
//   # Option 1: Hugging Face Inference API (default if TEI not set)
//   HUGGINGFACE_API_TOKEN=hf_xxx
//   HUGGINGFACE_MODEL=sentence-transformers/all-MiniLM-L6-v2
//   HUGGINGFACE_POOLING=mean           # mean|max (default: mean)
//   HUGGINGFACE_NORMALIZE=true         # true|false (default: true)
//   HUGGINGFACE_WAIT_FOR_MODEL=true    # default: true
//
//   # Option 2: TEI (Text Embeddings Inference) or compatible /embed service
//   TEI_URL=https://your-tei-hostname  # if set, TEI is used (POST {TEI_URL}/embed)
//   TEI_API_TOKEN=optional_bearer_token
//
//   # Batching
//   EMBEDDING_BATCH_SIZE=64            # default: 64

type Chunk struct {
	ID, Text string
}

// Embedder is a minimal interface you can call from your upload flow.
type Embedder interface {
	Embed(ctx context.Context, chunks []Chunk) (map[string][]float32, error)
}

func NewEmbedderFromEnv() (Embedder, error) {
	if base := strings.TrimSpace(os.Getenv("TEI_URL")); base != "" {
		return &teiEmbedder{
			baseURL: strings.TrimRight(base, "/"),
			token:   os.Getenv("TEI_API_TOKEN"),
			client:  &http.Client{Timeout: 60 * time.Second},
			batch:   getBatchSize(),
		}, nil
	}
	token := os.Getenv("HUGGINGFACE_API_TOKEN")
	if token == "" {
		return nil, errors.New("HUGGINGFACE_API_TOKEN not set (or set TEI_URL to use TEI)")
	}
	model := os.Getenv("HUGGINGFACE_MODEL")
	if model == "" {
		model = "sentence-transformers/all-MiniLM-L6-v2"
	}
	pooling := os.Getenv("HUGGINGFACE_POOLING")
	if pooling == "" {
		pooling = "mean"
	}
	normalize := parseBoolDefault(os.Getenv("HUGGINGFACE_NORMALIZE"), true)
	wait := parseBoolDefault(os.Getenv("HUGGINGFACE_WAIT_FOR_MODEL"), true)

	return &hfEmbedder{
		client:    &http.Client{Timeout: 60 * time.Second},
		token:     token,
		model:     model,
		pooling:   pooling,
		normalize: normalize,
		wait:      wait,
		batch:     getBatchSize(),
	}, nil
}

func getBatchSize() int {
	if v := os.Getenv("EMBEDDING_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 64
}

func parseBoolDefault(s string, def bool) bool {
	if s == "" {
		return def
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return def
	}
	return b
}

// -------------------- Hugging Face Inference API --------------------

type hfEmbedder struct {
	client          *http.Client
	token, model    string
	pooling         string // mean|max
	normalize, wait bool
	batch           int
}

func (h *hfEmbedder) Embed(ctx context.Context, chunks []Chunk) (map[string][]float32, error) {
	if len(chunks) == 0 {
		return map[string][]float32{}, nil
	}
	out := make(map[string][]float32, len(chunks))

	// Endpoint (feature-extraction pipeline with pooling & normalization)
	// https://api-inference.huggingface.co/pipeline/feature-extraction/{model}?pooling=mean&normalize=true
	base := "https://api-inference.huggingface.co/pipeline/feature-extraction/"
	url := fmt.Sprintf("%s%s?pooling=%s&normalize=%t&wait_for_model=%t",
		base, h.model, h.pooling, h.normalize, h.wait)

	type reqBody struct {
		Inputs []string `json:"inputs"`
	}
	// Response with pooling returns [][]float (one vector per input).
	var resBody [][]float32

	for i := 0; i < len(chunks); i += h.batch {
		j := i + h.batch
		if j > len(chunks) {
			j = len(chunks)
		}
		batch := chunks[i:j]

		inputs := make([]string, len(batch))
		for k, c := range batch {
			inputs[k] = c.Text
		}
		payload, _ := json.Marshal(reqBody{Inputs: inputs})

		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+h.token)

		resp, err := h.client.Do(req)
		if err != nil {
			return nil, err
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				var dbg bytes.Buffer
				_, _ = dbg.ReadFrom(resp.Body)
				err = fmt.Errorf("HF API non-200: %d: %s", resp.StatusCode, dbg.String())
				return
			}
			dec := json.NewDecoder(resp.Body)
			// When pooling=mean, the API returns a 2D array. Without pooling it returns 3D (tokens).
			if err = dec.Decode(&resBody); err != nil {
				return
			}
		}()
		if err != nil {
			return nil, err
		}
		if len(resBody) != len(batch) {
			return nil, fmt.Errorf("HF API embeddings count mismatch: have %d want %d", len(resBody), len(batch))
		}
		for k, c := range batch {
			// Copy to avoid re-use of backing array across iterations
			vec := make([]float32, len(resBody[k]))
			copy(vec, resBody[k])
			out[c.ID] = vec
		}
	}
	return out, nil
}

// -------------------- TEI (/embed) compatible client --------------------

type teiEmbedder struct {
	baseURL string
	token   string
	client  *http.Client
	batch   int
}

func (t *teiEmbedder) Embed(ctx context.Context, chunks []Chunk) (map[string][]float32, error) {
	if len(chunks) == 0 {
		return map[string][]float32{}, nil
	}
	out := make(map[string][]float32, len(chunks))

	type reqBody struct {
		Inputs []string `json:"inputs"`
	}
	type respBody struct {
		Embeddings [][]float32 `json:"embeddings"`
	}

	url := t.baseURL + "/embed"

	for i := 0; i < len(chunks); i += t.batch {
		j := i + t.batch
		if j > len(chunks) {
			j = len(chunks)
		}
		batch := chunks[i:j]

		inputs := make([]string, len(batch))
		for k, c := range batch {
			inputs[k] = c.Text
		}
		payload, _ := json.Marshal(reqBody{Inputs: inputs})

		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if t.token != "" {
			req.Header.Set("Authorization", "Bearer "+t.token)
		}

		resp, err := t.client.Do(req)
		if err != nil {
			return nil, err
		}
		var rb respBody
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				var dbg bytes.Buffer
				_, _ = dbg.ReadFrom(resp.Body)
				err = fmt.Errorf("TEI non-200: %d: %s", resp.StatusCode, dbg.String())
				return
			}
			err = json.NewDecoder(resp.Body).Decode(&rb)
		}()
		if err != nil {
			return nil, err
		}
		if len(rb.Embeddings) != len(batch) {
			return nil, fmt.Errorf("TEI embeddings count mismatch: have %d want %d", len(rb.Embeddings), len(batch))
		}
		for k, c := range batch {
			vec := make([]float32, len(rb.Embeddings[k]))
			copy(vec, rb.Embeddings[k])
			out[c.ID] = vec
		}
	}
	return out, nil
}
