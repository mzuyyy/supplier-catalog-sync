package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"catalogsync/internal/cli"
)

func TestCLIOutageAndResume(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "catalog.db")
	var out, stderr bytes.Buffer
	args := []string{"--db", path, "sync", "start", "--source", "../../fixtures/updates/basic.json", "--fail-before-batch", "3"}
	// When
	code := cli.Run(context.Background(), args, &out, &stderr)
	// Then
	if code != 2 || !strings.Contains(out.String(), "INTERRUPTED") {
		t.Fatalf("exit=%d out=%s err=%s", code, out.String(), stderr.String())
	}
	fields := strings.Fields(out.String())
	if len(fields) < 2 || fields[0] != "Job:" {
		t.Fatal(out.String())
	}
	jobID := fields[1]
	out.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"--db", path, "sync", "resume", "--job", jobID}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "COMPLETED") {
		t.Fatalf("exit=%d out=%s err=%s", code, out.String(), stderr.String())
	}
}
