package supplier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidManifest = errors.New("invalid manifest")
var ErrUnavailable = errors.New("supplier unavailable")

type Manifest struct {
	SupplierID         string `json:"supplier_id"`
	UpdateID           string `json:"update_id"`
	DeclaredBatchCount int    `json:"declared_batch_count"`
}

type Batch struct {
	Index  int               `json:"batch_index"`
	IsLast bool              `json:"is_last"`
	Events []json.RawMessage `json:"events"`
}

type Source interface {
	Manifest() Manifest
	Batch(context.Context, int) (Batch, error)
}

type Fixture struct {
	Info            Manifest
	Path            string
	SHA256          string
	FailBeforeBatch int
	batches         []Batch
}

func Load(ctx context.Context, path string) (*Fixture, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load source: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("absolute source path: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	var envelope struct {
		Manifest
		Batches []struct {
			Index  *int              `json:"batch_index"`
			IsLast bool              `json:"is_last"`
			Events []json.RawMessage `json:"events"`
		} `json:"batches"`
	}
	if err := Decode(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}
	m := envelope.Manifest
	if strings.TrimSpace(m.SupplierID) == "" || strings.TrimSpace(m.UpdateID) == "" || m.DeclaredBatchCount < 1 || m.DeclaredBatchCount != len(envelope.Batches) {
		return nil, fmt.Errorf("%w: nonempty IDs and matching positive batch count required", ErrInvalidManifest)
	}
	src := &Fixture{Info: m, Path: abs, SHA256: Digest(data)}
	for i, b := range envelope.Batches {
		if b.Index == nil || *b.Index != i || b.IsLast != (i == m.DeclaredBatchCount-1) || b.Events == nil {
			return nil, fmt.Errorf("%w: batch %d needs contiguous index, events array and correct final marker", ErrInvalidManifest, i)
		}
		src.batches = append(src.batches, Batch{Index: i, IsLast: b.IsLast, Events: b.Events})
	}
	return src, nil
}

func (f *Fixture) Manifest() Manifest { return f.Info }

func (f *Fixture) Batch(ctx context.Context, index int) (Batch, error) {
	if err := ctx.Err(); err != nil {
		return Batch{}, fmt.Errorf("fetch batch: %w", err)
	}
	if index < 0 || index >= len(f.batches) {
		return Batch{}, fmt.Errorf("%w: missing batch %d", ErrInvalidManifest, index)
	}
	if f.FailBeforeBatch == index+1 {
		return Batch{}, fmt.Errorf("%w: simulated outage before batch %d", ErrUnavailable, index+1)
	}
	return f.batches[index], nil
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func Decode(data []byte, target interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("trailing JSON: %w", err)
		}
		return errors.New("unexpected trailing JSON value")
	}
	return nil
}
