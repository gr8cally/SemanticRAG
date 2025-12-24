package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
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
		}
	}

	// 2) Build aligned slices: ids and embeddings
	ids := make([]chroma.DocumentID, 0, len(chunks))
	embs := make([]embeddings.Embedding, 0, len(chunks))
	texts := make([]string, 0, len(chunks))
	metas := make([]chroma.DocumentMetadata, 0, len(chunks))

	for _, c := range chunks {
		vec, ok := embeds[c.ID]
		if !ok {
			http.Error(w, "missing embedding for chunk "+c.ID, http.StatusBadRequest)
			return
		}

		ids = append(ids, chroma.DocumentID(c.ID))
		embs = append(embs, embeddings.NewEmbeddingFromFloat32(vec))
		texts = append(texts, c.Text)

		metas = append(metas, chroma.NewDocumentMetadata(
			chroma.NewStringAttribute("context", fileName), // or whatever “context” means to you
			chroma.NewStringAttribute("doc_id", c.ID),
			chroma.NewIntAttribute("len", int64(len(c.Text))),
		))
	}

	// 3) Add to Chroma using IDs + Embeddings
	//    All slice lengths must match; otherwise the client will return a validation error.

	err = collection.Add(ctx,
		chroma.WithIDs(ids...),
		chroma.WithEmbeddings(embs...),
		chroma.WithTexts(texts...),
		chroma.WithMetadatas(metas...),
	)
	if err != nil {
		http.Error(w, "failed to add to chroma: "+err.Error(), http.StatusInternalServerError)
		return
	}

	count, err := collection.Count(ctx)
	if err != nil {
		log.Fatalf("Error counting collection: %s \n", err)
		return
	}
	fmt.Printf("Count collection: %d\n", count)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK: upserted " + strconv.Itoa(len(ids)) + " chunks"))
}

type ChatRequest struct {
	Query string `json:"query"`
}

type ChatResponse struct {
	Answer  string   `json:"answer"`
	Context []string `json:"context"`
}

var (
	geminiLLM *GeminiLLM
)

func readChatRequest(r *http.Request) (ChatRequest, error) {
	ct := r.Header.Get("Content-Type")

	// JSON
	if strings.Contains(ct, "application/json") {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return ChatRequest{}, err
		}
		var req ChatRequest
		if err := json.Unmarshal(b, &req); err != nil {
			return ChatRequest{}, err
		}
		return req, nil
	}

	// Form (x-www-form-urlencoded or multipart)
	_ = r.ParseMultipartForm(10 << 20)
	_ = r.ParseForm()

	return ChatRequest{
		Query: r.FormValue("query"),
	}, nil
}

func promptHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	ctx := r.Context()

	req, err := readChatRequest(r)
	if err != nil || req.Query == "" {
		http.Error(w, "expected {context, query}", http.StatusBadRequest)
		return
	}

	// 1) Embed query (adapt this to your embedder API)
	embedder, err := NewEmbedderFromEnv()
	if err != nil {
		http.Error(w, "failed to NewEmbedderFromEnv", http.StatusInternalServerError)
		return
	}

	// create a single “chunk” to embed (adjust to your chunk type)
	qChunk := Chunk{ID: "q", Text: req.Query}
	m, err := embedder.Embed(ctx, []Chunk{qChunk})
	if err != nil {
		http.Error(w, "failed to embed query", http.StatusInternalServerError)
		return
	}

	qVec, ok := m["q"]
	if !ok {
		http.Error(w, "missing query embedding", http.StatusInternalServerError)
		return
	}

	// 2) Query Chroma with metadata filter: context == req.Context
	qr, err := collection.Query(ctx,
		chroma.WithQueryEmbeddings(embeddings.NewEmbeddingFromFloat32(qVec)),
		chroma.WithNResults(5),
		chroma.WithIncludeQuery(chroma.IncludeDocuments, chroma.IncludeMetadatas),
	)
	if err != nil {
		http.Error(w, "chroma query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3) Pull retrieved texts (documents)
	retrieved := make([]string, 0, 5)
	docsGroups := qr.GetDocumentsGroups()
	if len(docsGroups) > 0 {
		for _, d := range docsGroups[0] {
			if d == nil {
				continue
			}
			retrieved = append(retrieved, d.ContentString())
		}
	}

	contextBlock := strings.Join(retrieved, "\n")

	// 4) Prompt Gemini
	prompt := fmt.Sprintf(
		"Context:\n%s\n\nQuestion: %s\n\nBased on the context above, generate a succinct answer.",
		contextBlock, req.Query,
	)

	answer, err := geminiLLM.Generate(ctx, prompt)
	if err != nil {
		http.Error(w, "gemini failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 5) Return JSON
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ChatResponse{
		Answer:  answer,
		Context: retrieved,
	})
}

func getFileContents(w http.ResponseWriter, r *http.Request) (string, string) {
	// 1. Parse the multipart form (32MB limit)
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "failed to parse multipart form", http.StatusBadRequest)
		return "", ""
	}

	// 2. Access the "files" slice directly from the form
	files := r.MultipartForm.File["files"]

	// 3. Check if no files were provided
	if len(files) == 0 {
		http.Error(w, "no file provided in 'files' field", http.StatusBadRequest)
		return "", ""
	}

	// 4. Strict check: error if more than one file is uploaded
	if len(files) > 1 {
		http.Error(w, "multiple files not allowed; please upload exactly one file", http.StatusBadRequest)
		return "", ""
	}

	// 5. Open the single file
	fileHeader := files[0]
	file, err := fileHeader.Open()
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return "", ""
	}
	defer file.Close()

	// 6. Read content
	contentBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read file content", http.StatusInternalServerError)
		return "", ""
	}

	return string(contentBytes), fileHeader.Filename
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
