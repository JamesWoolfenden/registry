// !codeanalysis
package main

import (
	"context"
	"testing"

	"google.golang.org/genai"
)

func TestScanPypiPackages(t *testing.T) {
	type args struct {
		ctx    context.Context
		client *genai.Client
		model  string
		mcp    Server
	}

	const model = "gemini-2.5-flash"
	config := loadConfig()

	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		Project:  config.GCPProject,
		Location: "us-central1",
		Backend:  genai.BackendVertexAI,
	})

	var test Server

	test.Packages = append(test.Packages, Package{
		RegistryType: "pypi",
		Identifier:   "mcpcap",
		Version:      "0.4.3",
		Transport: Transport{
			Type: "stdio",
		},
	})

	tests := []struct {
		name    string
		args    args
		want    *PaloMeta
		wantErr bool
	}{
		{"Pass", args{ctx, client, model, test}, nil, false},
		{"EmptyPackages", args{ctx, client, model, Server{Packages: []Package{}}}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanPypiPackages(tt.args.ctx, tt.args.client, tt.args.model, tt.args.mcp)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanPypiPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil {
				t.Errorf("ScanPypiPackages() failed to retieve analysis")
			}
		})
	}
}
