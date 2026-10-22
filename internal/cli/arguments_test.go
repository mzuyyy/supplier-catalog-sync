package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"catalogsync/internal/cli"
)

func TestHelpAndInvalidArgumentsAvoidCreatingDatabase(t *testing.T) {
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0},
		{[]string{"sync", "start", "--help"}, 0},
		{[]string{"sync", "resume"}, 1},
		{[]string{"sync", "start", "--source", "x", "--fail-before-batch", "-1"}, 1},
		{[]string{"sync", "start", "--source", "x", "--delay-between-batches", "-1s"}, 1},
		{[]string{"sync", "list", "extra"}, 1},
		{[]string{"catalog", "unknown"}, 1},
		{[]string{"sync", "list", "--unknown"}, 1},
	}
	for _, tt := range cases {
		t.Run(filepath.Join(tt.args...), func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), "must-not-exist.db")
			args := append([]string{"--db", path}, tt.args...)
			var out, stderr bytes.Buffer
			// When
			code := cli.Run(context.Background(), args, &out, &stderr)
			// Then
			if code != tt.code {
				t.Fatalf("code=%d want=%d out=%s stderr=%s", code, tt.code, out.String(), stderr.String())
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("DB created on parse/help: %v", err)
			}
		})
	}
}

func TestConflictAndReadOnlyInspectionHaveDifferentExitCodes(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "catalog.db")
	var out, stderr bytes.Buffer
	// When
	code := cli.Run(context.Background(), []string{"--db", path, "sync", "start", "--source", "../../fixtures/updates/conflict.json"}, &out, &stderr)
	// Then
	if code != 3 {
		t.Fatalf("exit=%d out=%s err=%s", code, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"--db", path, "sync", "list"}, &out, &stderr); code != 0 {
		t.Fatalf("read-only exit=%d err=%s", code, stderr.String())
	}
}
