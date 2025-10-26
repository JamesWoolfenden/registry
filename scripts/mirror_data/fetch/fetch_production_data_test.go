package main

import (
	"encoding/json"
	"testing"
)

func Test_saveToFile(t *testing.T) {
	type args struct {
		servers  []json.RawMessage
		filename string
	}

	var testData []json.RawMessage

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"Pass", args{testData, "test.json"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := saveToFile(tt.args.servers, tt.args.filename); (err != nil) != tt.wantErr {
				t.Errorf("saveToFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
