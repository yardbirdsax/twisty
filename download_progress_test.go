package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestTermDownloadBar_RenderWithContentLength(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{
		w:          &buf,
		fileName:   "us-northeast",
		fileIndex:  1,
		totalFiles: 3,
		totalBytes: 200 * 1024 * 1024, // 200 MB
	}

	bar.BytesDownloaded(100 * 1024 * 1024) // 100 MB downloaded

	out := buf.String()
	if !strings.Contains(out, "us-northeast (2/3)") {
		t.Errorf("expected file label in output, got: %q", out)
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("expected 50%% in output, got: %q", out)
	}
	if !strings.Contains(out, "100.0") {
		t.Errorf("expected downloaded MB in output, got: %q", out)
	}
	if !strings.Contains(out, "200.0") {
		t.Errorf("expected total MB in output, got: %q", out)
	}
	if !strings.HasPrefix(out, "\r") {
		t.Errorf("expected output to start with carriage return, got: %q", out)
	}
}

func TestTermDownloadBar_RenderNoContentLength(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{
		w:          &buf,
		fileName:   "us-west",
		fileIndex:  0,
		totalFiles: 2,
		totalBytes: -1, // unknown size
	}

	bar.BytesDownloaded(50 * 1024 * 1024) // 50 MB

	out := buf.String()
	if !strings.Contains(out, "us-west (1/2)") {
		t.Errorf("expected file label in output, got: %q", out)
	}
	if !strings.Contains(out, "50.0 MB") {
		t.Errorf("expected downloaded bytes in output, got: %q", out)
	}
	// No percentage should appear when Content-Length is unknown
	if strings.Contains(out, "%") {
		t.Errorf("expected no percentage when Content-Length unknown, got: %q", out)
	}
}

func TestTermDownloadBar_Done(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{
		w:          &buf,
		totalFiles: 3,
	}
	bar.StartFile("file1", 0, 3, 100*1024*1024)
	bar.BytesDownloaded(50 * 1024 * 1024)
	buf.Reset()

	bar.Done()

	out := buf.String()
	if !strings.Contains(out, "Downloads complete") {
		t.Errorf("expected 'Downloads complete' in output, got: %q", out)
	}
	if !strings.Contains(out, "3 files") {
		t.Errorf("expected '3 files' in output, got: %q", out)
	}
	if !strings.Contains(out, "MB") {
		t.Errorf("expected MB in Done output, got: %q", out)
	}
	if !strings.Contains(out, "50.0") {
		t.Errorf("expected 50.0 MB in Done output, got: %q", out)
	}
}

func TestLogDownloadProgress_PrintsEvery10MB(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{
		w:          &buf,
		fileName:   "us-midwest",
		totalBytes: 100 * 1024 * 1024,
		totalFiles: 1,
	}
	p.StartFile("us-midwest", 0, 1, 100*1024*1024)

	// Less than 10 MB — should not print
	p.BytesDownloaded(5 * 1024 * 1024)
	if buf.Len() != 0 {
		t.Errorf("expected no output before 10 MB, got: %q", buf.String())
	}

	// Cross 10 MB threshold
	p.BytesDownloaded(6 * 1024 * 1024)
	out := buf.String()
	if !strings.Contains(out, "downloaded") {
		t.Errorf("expected progress line after 10 MB, got: %q", out)
	}
	if !strings.Contains(out, "%") {
		t.Errorf("expected percentage in log line, got: %q", out)
	}
}

func TestLogDownloadProgress_NoContentLength(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{
		w:          &buf,
		fileName:   "us-west",
		totalBytes: -1,
		totalFiles: 1,
	}
	p.StartFile("us-west", 0, 1, -1)

	// Cross 10 MB threshold
	p.BytesDownloaded(11 * 1024 * 1024)
	out := buf.String()
	if !strings.Contains(out, "downloaded") {
		t.Errorf("expected progress line after 10 MB, got: %q", out)
	}
	// No percentage when total is unknown
	if strings.Contains(out, "%") {
		t.Errorf("expected no percentage when Content-Length unknown, got: %q", out)
	}
}

func TestLogDownloadProgress_FileComplete(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{
		w:          &buf,
		fileName:   "us-northeast",
		totalBytes: 100 * 1024 * 1024,
		totalFiles: 2,
	}
	p.StartFile("us-northeast", 0, 2, 100*1024*1024)
	p.BytesDownloaded(100 * 1024 * 1024)
	buf.Reset()
	p.FileComplete()

	out := buf.String()
	if !strings.Contains(out, "done") {
		t.Errorf("expected 'done' in FileComplete output, got: %q", out)
	}
	if !strings.Contains(out, "MB") {
		t.Errorf("expected MB in FileComplete output, got: %q", out)
	}
}

func TestLogDownloadProgress_Done(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{
		w:          &buf,
		totalFiles: 2,
	}
	p.StartFile("file1", 0, 2, 50*1024*1024)
	p.BytesDownloaded(30 * 1024 * 1024)
	buf.Reset()

	p.Done()

	out := buf.String()
	if !strings.Contains(out, "Downloads complete") {
		t.Errorf("expected 'Downloads complete' in Done output, got: %q", out)
	}
	if !strings.Contains(out, "2 files") {
		t.Errorf("expected '2 files' in Done output, got: %q", out)
	}
	if !strings.Contains(out, "MB") {
		t.Errorf("expected MB in Done output, got: %q", out)
	}
	if !strings.Contains(out, "30.0") {
		t.Errorf("expected 30.0 MB in Done output, got: %q", out)
	}
}

func TestNewDownloadProgress_NonTerminal(t *testing.T) {
	// Use a regular file (not a terminal) — should return logDownloadProgress.
	f, err := os.CreateTemp(t.TempDir(), "testfile")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	defer f.Close()

	p := newDownloadProgress(f)
	if _, ok := p.(*logDownloadProgress); !ok {
		t.Errorf("expected *logDownloadProgress for non-terminal, got %T", p)
	}
}
