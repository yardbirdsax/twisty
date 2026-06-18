package main

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestNoopFileDownloadProgress_ImplementsInterface(t *testing.T) {
	var p DownloadProgress = NoopDownloadProgress{}
	fp := p.StartFile("test.osm.pbf", 0, 1, 100)
	fp.BytesDownloaded(50)
	fp.FileComplete()
	p.Done()
}

func TestTermDownloadBar_RenderWithContentLength(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf}

	fp := bar.StartFile("us-northeast", 1, 3, 200*1024*1024)
	fp.BytesDownloaded(100 * 1024 * 1024)

	out := buf.String()
	if !strings.Contains(out, "MB") {
		t.Errorf("expected MB in output, got: %q", out)
	}
	if !strings.HasPrefix(out, "\r") {
		t.Errorf("expected output to start with carriage return, got: %q", out)
	}
}

func TestTermDownloadBar_RenderNoContentLength(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf}

	fp := bar.StartFile("us-west", 0, 2, -1)
	fp.BytesDownloaded(50 * 1024 * 1024)

	out := buf.String()
	if !strings.Contains(out, "50.0 MB") {
		t.Errorf("expected downloaded bytes in output, got: %q", out)
	}
}

func TestTermDownloadBar_Done(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf}

	fp := bar.StartFile("file1", 0, 3, 100*1024*1024)
	fp.BytesDownloaded(50 * 1024 * 1024)
	buf.Reset()

	bar.Done()

	out := buf.String()
	if !strings.Contains(out, "Downloads complete") {
		t.Errorf("expected 'Downloads complete' in output, got: %q", out)
	}
	if !strings.Contains(out, "3 files") {
		t.Errorf("expected '3 files' in output, got: %q", out)
	}
	if !strings.Contains(out, "50.0") {
		t.Errorf("expected 50.0 MB in Done output, got: %q", out)
	}
}

func TestTermDownloadBar_ConcurrentSafe(t *testing.T) {
	var buf bytes.Buffer
	bar := &termDownloadBar{w: &buf}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fp := bar.StartFile("file", idx, 4, 10*1024*1024)
			for j := 0; j < 10; j++ {
				fp.BytesDownloaded(1024 * 1024)
			}
			fp.FileComplete()
		}(i)
	}
	wg.Wait()
	bar.Done()
}

func TestLogDownloadProgress_PrintsEvery10MB(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{w: &buf}
	fp := p.StartFile("us-midwest", 0, 1, 100*1024*1024)

	fp.BytesDownloaded(5 * 1024 * 1024)
	if buf.Len() == 0 {
		// StartFile prints a "Downloading" line; reset before testing threshold
	}
	buf.Reset()

	fp.BytesDownloaded(6 * 1024 * 1024)
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
	p := &logDownloadProgress{w: &buf}
	fp := p.StartFile("us-west", 0, 1, -1)
	buf.Reset()

	fp.BytesDownloaded(11 * 1024 * 1024)
	out := buf.String()
	if !strings.Contains(out, "downloaded") {
		t.Errorf("expected progress line after 10 MB, got: %q", out)
	}
	if strings.Contains(out, "%") {
		t.Errorf("expected no percentage when Content-Length unknown, got: %q", out)
	}
}

func TestLogDownloadProgress_FileComplete(t *testing.T) {
	var buf bytes.Buffer
	p := &logDownloadProgress{w: &buf}
	fp := p.StartFile("us-northeast", 0, 2, 100*1024*1024)
	fp.BytesDownloaded(100 * 1024 * 1024)
	buf.Reset()
	fp.FileComplete()

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
	p := &logDownloadProgress{w: &buf}
	fp := p.StartFile("file1", 0, 2, 50*1024*1024)
	fp.BytesDownloaded(30 * 1024 * 1024)
	buf.Reset()

	p.Done()

	out := buf.String()
	if !strings.Contains(out, "Downloads complete") {
		t.Errorf("expected 'Downloads complete' in Done output, got: %q", out)
	}
	if !strings.Contains(out, "2 files") {
		t.Errorf("expected '2 files' in Done output, got: %q", out)
	}
	if !strings.Contains(out, "30.0") {
		t.Errorf("expected 30.0 MB in Done output, got: %q", out)
	}
}

func TestNewDownloadProgress_NonTerminal(t *testing.T) {
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
