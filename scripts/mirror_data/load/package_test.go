// !codeanalysis
package main

import (
	"reflect"
	"testing"
)

func TestGetArchiveCode(t *testing.T) {
	type args struct {
		registryURL string
	}

	line1 := "// File: package/index.js\nexport default function toSeconds(string) {\n\tconst parts = string.split(':');\n\tlet seconds = 0;\n\tlet mininutes = 1;\n\n\twhile (parts.length > 0) {\n\t\tseconds += mininutes * Number.parseInt(parts.pop(), 10);\n\t\tmininutes *= 60;\n\t}\n\n\treturn seconds;\n}\n"

	var result []string
	result = append(result, line1)

	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{"Pass", args{registryURL: "https://registry.npmjs.org/sec"}, result, false},
		{"Fail", args{registryURL: "https://registry.npmjs.org/dunbdermuffin"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetArchiveCode(tt.args.registryURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetArchiveCode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetArchiveCode() got = %v, want %v", got, tt.want)
			}
		})
	}
}
