package supplier_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"catalogsync/internal/supplier"
)

func TestLoadChecksManifestBeforeProcessingEvents(t *testing.T) {
	tests := []struct {
		name, body string
		valid      bool
	}{
		{"valid", `{"supplier_id":"S","update_id":"U","declared_batch_count":1,"batches":[{"batch_index":0,"is_last":true,"events":[]}]}`, true},
		{"invalid event deferred", `{"supplier_id":"S","update_id":"U","declared_batch_count":1,"batches":[{"batch_index":0,"is_last":true,"events":[{"revision":"bad"}]}]}`, true},
		{"missing marker", `{"supplier_id":"S","update_id":"U","declared_batch_count":1,"batches":[{"batch_index":0,"events":[]}]}`, false},
		{"empty", `{"supplier_id":"S","update_id":"U","declared_batch_count":0,"batches":[]}`, false},
		{"gap", `{"supplier_id":"S","update_id":"U","declared_batch_count":1,"batches":[{"batch_index":1,"is_last":true,"events":[]}]}`, false},
		{"missing index", `{"supplier_id":"S","update_id":"U","declared_batch_count":1,"batches":[{"is_last":true,"events":[]}]}`, false},
		{"trailing JSON", `{"supplier_id":"S","update_id":"U","declared_batch_count":1,"batches":[{"batch_index":0,"is_last":true,"events":[]}]} {}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), "source.json")
			if err := os.WriteFile(path, []byte(tt.body), 0600); err != nil {
				t.Fatal(err)
			}
			// When
			_, err := supplier.Load(context.Background(), path)
			// Then
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v err=%v", tt.valid, err)
			}
		})
	}
}

func TestBatchOutageIsInvocationLocal(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "source.json")
	body := `{"supplier_id":"S","update_id":"U","declared_batch_count":1,"batches":[{"batch_index":0,"is_last":true,"events":[]}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	src, err := supplier.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	src.FailBeforeBatch = 1
	// When
	_, err = src.Batch(context.Background(), 0)
	// Then
	if err == nil {
		t.Fatal("expected outage")
	}
	src.FailBeforeBatch = 0
	if _, err := src.Batch(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}
