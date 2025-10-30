package main

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/genai"
)

func TestReadAllCodeFilesFromRepo(t *testing.T) {
	type args struct {
		repoURL string
		version string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{"pass",
			args{repoURL: "https://github.com/jameswoolfenden/pike", version: "v0.2.1"}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadAllCodeFilesFromRepo(tt.args.repoURL, tt.args.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadAllCodeFilesFromRepo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) == 0 {
				t.Errorf("No code found")
			}
		})
	}
}

func TestScanRepo(t *testing.T) {
	type args struct {
		client *genai.Client
		model  string
		mcp    Server
	}

	var mcpServer Server

	mcpServer.Repository.URL = "https://github.com/jameswoolfenden/pike"
	mcpServer.Version = "v0.1.1"

	const model = "gemini-2.5-flash"
	config := loadConfig()

	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		Project:  config.GCPProject,
		Location: "us-central1",
		Backend:  genai.BackendVertexAI,
	})

	tests := []struct {
		name    string
		args    args
		want    *PaloMeta
		wantErr bool
	}{
		{"pass", args{client, model, mcpServer}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanRepo(ctx, tt.args.client, tt.args.model, tt.args.mcp)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanRepo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanRepo() got = %v, want %v", got, tt.want)
			}
		})
	}
}

//nolint:all
func Test_isCodeFile(t *testing.T) {
	type args struct {
		filename string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCodeFile(tt.args.filename); got != tt.want {
				t.Errorf("isCodeFile() = %v, want %v", got, tt.want)
			}
		})
	}
}
