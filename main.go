package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: implement upload handling
	json.NewEncoder(w).Encode(map[string]string{"message": "Upload endpoint not yet implemented"})
}

func promptHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: implement prompt handling
	json.NewEncoder(w).Encode(map[string]string{"message": "Prompt endpoint not yet implemented"})
}

func rechunkHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var reChunkFile string
	err = json.Unmarshal(b, &reChunkFile)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error body text not a string"))
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("received bytes: " + string(len(b))))
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

type Chunk struct {
	ID, Text string
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
