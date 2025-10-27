package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/genai"
)

func Dispatch(ctx context.Context, allCode []string, client *genai.Client, model string) (*PaloMeta, error) {
	var paloExtensions PaloMeta

	if len(allCode) == 0 {
		log.Info().Msg("No code files found for review")
		paloExtensions.SourceCodeAnalysis.Review = "No Code"
		paloExtensions.SourceCodeAnalysis.Score = 0
		paloExtensions.SourceCodeAnalysis.Updated = timestamp()
		return &paloExtensions, nil
	}

	combinedCode := strings.Join(allCode, "\n\n")
	response, err := GenerateWithTextClient(ctx, client, combinedCode, "all files", model)
	if err != nil {
		log.Error().Msgf("Error generating review: %v", err)
		paloExtensions.SourceCodeAnalysis.Review = fmt.Sprintf("Analysis Failed %v", err)
		paloExtensions.SourceCodeAnalysis.Score = -999
		paloExtensions.SourceCodeAnalysis.Updated = timestamp()
		return &paloExtensions, err
	}

	log.Info().Msgf("Aggregate Score: %d", response.Score)
	log.Info().Msgf("Comment:")
	log.Info().Msgf("%s", response.Comment)

	if len(response.Comment) > 5000 {
		response.Comment = response.Comment[:5000]
	}

	paloExtensions.SourceCodeAnalysis.Review = response.Comment
	paloExtensions.SourceCodeAnalysis.Score = response.Score
	paloExtensions.SourceCodeAnalysis.Updated = timestamp()
	return &paloExtensions, nil
}

func timestamp() string {
	t := time.Now().UTC()
	return t.Format("20060102150405")
}

// GenerateWithTextClient shows how to generate text using a text prompt.
func GenerateWithTextClient(ctx context.Context, client *genai.Client, code string, file string, model string) (*ReviewResult, error) {
	resp, err := client.Models.GenerateContent(ctx,
		model,
		genai.Text("Reviewer the following code for security issues. Act as a security expert and score the repository for critical vulnerabilities (-5), high risk (-4), medium risk (-3), low risk (-2), and informational (-1). Return ONLY a valid JSON object with fields 'score' and 'comment', being concise in you comment, at less than 5000 characters,  with NO markdown formatting, code blocks, or extra text:\n\n"+code),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	type Parsed struct {
		Score   int    `json:"score"`
		Comment string `json:"comment"`
	}

	var parsed Parsed
	if err := json.Unmarshal([]byte(resp.Text()), &parsed); err != nil {
		return nil, fmt.Errorf("error parsing review for %s: %w", file, err)
	}

	return &ReviewResult{File: file, Score: parsed.Score, Comment: parsed.Comment}, nil
}
