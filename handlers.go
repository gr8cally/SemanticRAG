package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

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
	//ctx := r.Context()
	//embedder, err := NewEmbedderFromEnv()
	//if err != nil {
	//	http.Error(w, "failed to NewEmbedderFromEnv", http.StatusInternalServerError)
	//	return
	//}

	//embeds, err := embedder.Embed(ctx, chunks)
	//if err != nil {
	//	if err != nil {
	//		http.Error(w, "failed to Embed chunks", http.StatusInternalServerError)
	//		return
	//	} /* handle */
	//}

	embeds := map[string][]float32{"Hello": []float32{0.101, 1.1, 1.2}}

	// ... you already have: chunks []Chunk and embeds map[string][]float32
	// chunks[i].ID must correspond to embeds[chunks[i].ID]

	ctx := r.Context()

	// 2) Build aligned slices: ids and embeddings
	ids := make([]chroma.DocumentID, 0, len(chunks))
	embs := make([]embeddings.Embedding, 0, len(chunks))
	metas := make([]chroma.DocumentMetadata, 0, len(chunks)) // optional

	for _, c := range chunks {
		vec, ok := embeds[c.ID]
		if !ok {
			// skip or handle error; here we error out to keep dataset consistent
			http.Error(w, "missing embedding for chunk "+c.ID, http.StatusBadRequest)
			return
		}

		ids = append(ids, chroma.DocumentID(c.ID))
		embs = append(embs, embeddings.NewEmbeddingFromFloat32(vec))

		// Optional: attach metadata per chunk
		metas = append(metas, chroma.NewDocumentMetadata(
			chroma.NewStringAttribute("doc_id", c.ID),
			chroma.NewIntAttribute("len", int64(len(c.Text))),
		))
	}

	// 3) Add to Chroma using IDs + Embeddings
	//    All slice lengths must match; otherwise the client will return a validation error.
	if err := collection.Add(ctx,
		chroma.WithIDs(ids...),
		chroma.WithEmbeddings(embs...),
		chroma.WithMetadatas(metas...), // optional; drop this if you don’t need per-chunk metadata
	); err != nil {
		http.Error(w, "failed to add to chroma: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK: upserted " + strconv.Itoa(len(ids)) + " chunks"))

	fmt.Println(embeds)
	// Next step (later): upsert {id, document, embedding, metadata} into ChromaDB.

	count, err := collection.Count(context.Background())
	if err != nil {
		log.Fatalf("Error counting collection: %s \n", err)
		return
	}
	fmt.Printf("Count collection: %d\n", count)

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
