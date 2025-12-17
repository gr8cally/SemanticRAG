package main

import (
	"context"

	"google.golang.org/genai"
)

type GeminiLLM struct {
	client *genai.Client
	model  string
}

func NewGeminiLLMFromEnv(ctx context.Context, apiKey string) (*GeminiLLM, error) {
	model := "Gemini Flash 2.5 LLM"

	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, err
	}

	return &GeminiLLM{client: c, model: model}, nil
}

func (g *GeminiLLM) Generate(ctx context.Context, prompt string) (string, error) {
	res, err := g.client.Models.GenerateContent(ctx, g.model, genai.Text(prompt), nil)
	if err != nil {
		return "", err
	}
	return res.Text(), nil
}
