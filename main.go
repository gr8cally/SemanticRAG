package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
)

var chromaClient chroma.Client

func initChroma(baseURL string) error {
	c, err := chroma.NewHTTPClient(
		chroma.WithBaseURL(baseURL),
	)
	if err != nil {
		return fmt.Errorf("creating Chroma client: %w", err)
	}
	chromaClient = c
	return nil
}

func initGeminiLLM(ctx context.Context, apiKey string) error {
	gem, err := NewGeminiLLMFromEnv(ctx, apiKey)
	if err != nil {
		return fmt.Errorf("creating Gemini LLM: %w", err)
	}

	geminiLLM = gem
	return nil
}

// source ./chroma/bin/activate
// chroma run --path ./chroma-data --host 0.0.0.0 --port 8000
func main() {
	var err error
	currentConfig, err = Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
		return
	}

	err = initChroma(currentConfig.ChromaDBHost)
	if err != nil {
		log.Fatalf("failed to init chroma: %v", err)
		return
	}
	defer func() {
		if err := chromaClient.Close(); err != nil {
			log.Printf("Error closing Chroma client: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = initChromaCollection(ctx)
	if err != nil {
		log.Fatalf("failed to init chroma collection: %v", err)
		return
	}

	err = initGeminiLLM(ctx, currentConfig.GeminiAPIKey)
	if err != nil {
		log.Fatalf("failed to init gemini LLM: %v", err)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/upload", requirePost(uploadHandler))   // POST
	mux.HandleFunc("/chat", requirePost(promptHandler))     // POST
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

type Config struct {
	HFAPIKey       string // HF_API_KEY (required)
	EmbedModelName string // EMBED_MODEL_NAME
	GeminiAPIKey   string // GEMINI_API_KEY
	LLMModelName   string // LLM_MODEL_NAME
	ChromaDBHost   string // CHROMA_DB_HOST
	RAGDataDir     string // RAG_DATA_DIR
	ChunkLength    int    // CHUNK_LENGTH
	Port           int    // PORT
}

var currentConfig Config
var collection chroma.Collection

func initChromaCollection(ctx context.Context) error {
	c, err := chromaClient.GetOrCreateCollection(ctx, "rag_demo")
	if err != nil {
		return fmt.Errorf("GetOrCreateCollection failed: %w", err)
	}
	collection = c
	return nil
}

// Load loads .env-style files then reads process env.
// In production, prefer real environment variables and skip files.
func Load() (Config, error) {
	// Soft-load these files if present (order: base -> local overrides)
	_ = loadDotEnv(".env")
	_ = loadDotEnv(".env.local")

	cfg := Config{
		HFAPIKey:       os.Getenv("HF_API_KEY"),
		EmbedModelName: getEnvOr("EMBED_MODEL_NAME", "sentence-transformers/all-MiniLM-L6-v2"),
		GeminiAPIKey:   os.Getenv("GEMINI_API_KEY"),
		LLMModelName:   getEnvOr("LLM_MODEL_NAME", "gemini-2.5-flash"),
		ChromaDBHost:   getEnvOr("CHROMA_DB_HOST", "http://localhost:8000"),
		RAGDataDir:     getEnvOr("RAG_DATA_DIR", "./data"),
		ChunkLength:    getIntOr("CHUNK_LENGTH", 800),
		Port:           getIntOr("PORT", 8080),
	}
	if cfg.HFAPIKey == "" {
		return cfg, fmt.Errorf("missing required env: HF_API_KEY")
	}
	return cfg, nil
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err // ignore upstream
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Support "export KEY=VALUE"
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export"))
		}
		// Split on first '='
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		// Strip surrounding quotes if present
		val = stripQuotes(val)
		// Do not overwrite if already set in environment
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return sc.Err()
}

func stripQuotes(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
	}
	return v
}

func getEnvOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getIntOr(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
