// !codeanalysis
package main

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/genai"
)

//nolint:all
func TestScanNpmPackages(t *testing.T) {

	type args struct {
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
	mcp3.Packages = append(mcp3.Packages, pack)
	mcp3.Packages[0].Identifier = "test package"

	pack.Identifier = "test package"

	tests := []struct {
		name    string
		args    args
		want    *PaloMeta
		wantErr bool
	}{
		{"nil", args{client, model, mcp}, nil, true},
		{"empty", args{client, model, mcp2}, nil, true},
		{"not exist", args{client, model, mcp3}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			ctx := context.Background()
			got, err := ScanNpmPackages(ctx, tt.args.client, tt.args.model, tt.args.mcp)
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
