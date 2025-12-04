package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ----- Public API -----

// UpsertChunksToChroma ensures the collection exists and upserts ids+docs+embeddings.
func UpsertChunksToChroma(ctx context.Context, collectionName string, chunks []Chunk, embedMap map[string][]float32) error {
	if len(chunks) == 0 {
		return nil
	}
	cc := newChromaClient(currentConfig.ChromaDBHost, "") // add token if you secure Chroma
	colID, err := cc.getOrCreateCollection(ctx, collectionName, nil)
	if err != nil {
		return err
	}
	// Build aligned arrays (ids, documents, embeddings) in the same order.
	const batch = 64
	for i := 0; i < len(chunks); i += batch {
		j := i + batch
		if j > len(chunks) {
			j = len(chunks)
		}
		part := chunks[i:j]

		ids := make([]string, len(part))
		docs := make([]string, len(part))
		embs := make([][]float32, len(part))
		for k, c := range part {
			vec, ok := embedMap[c.ID]
			if !ok {
				return fmt.Errorf("missing embedding for chunk id=%s", c.ID)
			}
			ids[k] = c.ID
			docs[k] = c.Text
			embs[k] = vec
		}
		if err := cc.add(ctx, colID, ids, docs, embs, nil); err != nil {
			return err
		}
	}
	return nil
}

// ----- Minimal Chroma HTTP client -----

type chromaClient struct {
	bases  []string // try /api/v1 then /v1
	client *http.Client
	token  string // optional auth if you front the server
}

func newChromaClient(host, token string) *chromaClient {
	base := strings.TrimRight(host, "/")
	return &chromaClient{
		bases: []string{base + "/api/v1", base + "/v1"},
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		token: token,
	}
}

type collection struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

func (c *chromaClient) getOrCreateCollection(ctx context.Context, name string, metadata map[string]interface{}) (string, error) {
	// 1) try get-by-name
	col, _, err := c.getCollectionByName(ctx, name)
	if err == nil && col != nil && col.ID != "" {
		return col.ID, nil
	}
	// 2) create
	created, _, err := c.createCollection(ctx, name, metadata)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *chromaClient) getCollectionByName(ctx context.Context, name string) (*collection, string, error) {
	var lastErr error
	for _, b := range c.bases {
		u := fmt.Sprintf("%s/collections/%s", b, url.PathEscape(name))
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		c.setAuth(req)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			lastErr = fmt.Errorf("not found")
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("get collection non-200: %d %s", resp.StatusCode, readAll(resp.Body))
			continue
		}
		var col collection
		if err := json.NewDecoder(resp.Body).Decode(&col); err != nil {
			lastErr = err
			continue
		}
		return &col, b, nil
	}
	return nil, "", lastErr
}

func (c *chromaClient) createCollection(ctx context.Context, name string, metadata map[string]interface{}) (*collection, string, error) {
	payload := map[string]interface{}{
		"name": name,
	}
	if metadata != nil {
		payload["metadata"] = metadata
	}
	body, _ := json.Marshal(payload)

	var lastErr error
	for _, b := range c.bases {
		u := b + "/collections"
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		c.setAuth(req)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			lastErr = fmt.Errorf("create collection non-2xx: %d %s", resp.StatusCode, readAll(resp.Body))
			continue
		}
		var col collection
		if err := json.NewDecoder(resp.Body).Decode(&col); err != nil {
			lastErr = err
			continue
		}
		return &col, b, nil
	}
	return nil, "", lastErr
}

func (c *chromaClient) add(ctx context.Context, collectionID string, ids []string, documents []string, embeddings [][]float32, metadatas []map[string]interface{}) error {
	if len(ids) == 0 {
		return errors.New("no items to add")
	}
	// Chroma REST expects arrays: ids, embeddings ([][]float), documents ([]string), metadatas ([]object)
	reqBody := struct {
		IDs        []string                 `json:"ids"`
		Embeddings [][]float32              `json:"embeddings,omitempty"`
		Documents  []string                 `json:"documents,omitempty"`
		Metadatas  []map[string]interface{} `json:"metadatas,omitempty"`
	}{
		IDs:        ids,
		Embeddings: embeddings,
		Documents:  documents,
		Metadatas:  metadatas,
	}
	body, _ := json.Marshal(reqBody)

	var lastErr error
	for _, b := range c.bases {
		u := fmt.Sprintf("%s/collections/%s/add", b, url.PathEscape(collectionID))
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		c.setAuth(req)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("add non-200: %d %s", resp.StatusCode, readAll(resp.Body))
			continue
		}
		return nil
	}
	return lastErr
}

func (c *chromaClient) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func readAll(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return string(b)
}
