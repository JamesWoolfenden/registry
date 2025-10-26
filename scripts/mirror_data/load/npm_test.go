package main

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/genai"
)

func TestScanNpmPackages(t *testing.T) {
	type args struct {
		ctx    context.Context
		client *genai.Client
		model  string
		mcp    Server
	}

	var ctx = context.Background()
	const model = "gemini-2.5-flash"
	config := loadConfig()

	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		Project:  config.GCPProject,
		Location: "us-central1",
		Backend:  genai.BackendVertexAI,
	})

	type myPackage struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Transport    struct {
			Type string `json:"type"`
		} `json:"transport"`
		EnvironmentVariables []struct {
			Description string `json:"description"`
			Name        string `json:"name"`
			Format      string `json:"format,omitempty"`
			IsSecret    bool   `json:"isSecret,omitempty"`
		} `json:"environmentVariables,omitempty"`
	}

	var mcp, mcp2, mcp3 Server
	var pack myPackage

	mcp2.Packages = append(mcp2.Packages, pack)
	mcp3.Packages = append(mcp2.Packages, pack)
	mcp3.Packages[0].Identifier = "test package"

	pack.Identifier = "test package"

	tests := []struct {
		name    string
		args    args
		want    *PaloMeta
		wantErr bool
	}{
		{"nil", args{ctx, client, model, mcp}, nil, true},
		{"empty", args{ctx, client, model, mcp2}, nil, true},
		{"not exist", args{ctx, client, model, mcp3}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanNpmPackages(tt.args.ctx, tt.args.client, tt.args.model, tt.args.mcp)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanNpmPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanNpmPackages() got = %v, want %v", got, tt.want)
			}
		})
	}
}
