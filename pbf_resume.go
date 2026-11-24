package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A download accumulates in "<dest>.partial" beside its destination, with a
// "<dest>.partial.meta" recording the upstream version the bytes came from. An
// interrupted download leaves both, and the next attempt continues from them
// only if upstream still serves that version. The destination itself is only
// ever replaced, by rename, with a complete file.

// abandonedPartialAge is how long an unrelated partial can sit untouched before
// a later download clears it away. Its region is presumably no longer loaded.
const abandonedPartialAge = 7 * 24 * time.Hour

func partialPath(destPath string) string     { return destPath + ".partial" }
func partialMetaPath(destPath string) string { return partialPath(destPath) + ".meta" }

// version identifies an upstream file by its validators. Either may be empty.
type version struct{ etag, lastModified string }

func versionOf(h http.Header) version {
	return version{etag: h.Get("ETag"), lastModified: h.Get("Last-Modified")}
}

// ifRange is the validator to send in If-Range, so the server answers with the
// rest of the file only when it still has this version, or else the whole new
// file. A weak ETag is not allowed there. It is empty when v cannot vouch for a
// version.
func (v version) ifRange() string {
	if v.etag != "" && !strings.HasPrefix(v.etag, "W/") {
		return v.etag
	}
	return v.lastModified
}

// matches reports whether a response's headers describe this version.
func (v version) matches(h http.Header) bool {
	got := versionOf(h)
	if v.etag != "" && !strings.HasPrefix(v.etag, "W/") {
		return got.etag == v.etag
	}
	return v.lastModified != "" && got.lastModified == v.lastModified
}

func (v version) encode() []byte { return []byte(v.etag + "\n" + v.lastModified + "\n") }

func decodeVersion(b []byte) version {
	lines := strings.Split(string(b), "\n")
	if len(lines) < 2 {
		return version{}
	}
	return version{etag: lines[0], lastModified: lines[1]}
}

// resumePoint returns how many bytes of destPath's download are on disk and
// the version they came from. With nothing usable (no partial, or one with no
// record of its version) it discards what is there and returns zero.
func resumePoint(destPath string) (offset int64, v version) {
	fi, err := os.Stat(partialPath(destPath))
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		discardPartial(destPath)
		return 0, version{}
	}
	b, err := os.ReadFile(partialMetaPath(destPath))
	if err != nil {
		discardPartial(destPath)
		return 0, version{}
	}
	v = decodeVersion(b)
	if v.ifRange() == "" {
		discardPartial(destPath)
		return 0, version{}
	}
	return fi.Size(), v
}

func discardPartial(destPath string) {
	os.Remove(partialPath(destPath))     //nolint:errcheck // best effort
	os.Remove(partialMetaPath(destPath)) //nolint:errcheck // best effort
}

// contentRange parses a "bytes first-last/total" Content-Range. total is -1
// when the server says "*".
func contentRange(h string) (first, total int64, ok bool) {
	rest, found := strings.CutPrefix(h, "bytes ")
	if !found {
		return 0, 0, false
	}
	span, totalStr, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, false
	}
	firstStr, _, found := strings.Cut(span, "-")
	if !found {
		return 0, 0, false
	}
	first, err := strconv.ParseInt(firstStr, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	total = -1
	if totalStr != "*" {
		if total, err = strconv.ParseInt(totalStr, 10, 64); err != nil {
			return 0, 0, false
		}
	}
	return first, total, true
}

// removeOrphanedPartials clears away download debris other than keep's own
// partial: temp files of the previous scheme, which was named at random and so
// never found again, and partials nobody has touched for a long time.
func removeOrphanedPartials(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	keepNames := map[string]bool{
		filepath.Base(partialPath(keep)):     true,
		filepath.Base(partialMetaPath(keep)): true,
	}
	for _, en := range entries {
		name := en.Name()
		if keepNames[name] || !en.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, name)
		switch {
		case strings.HasPrefix(name, ".download-") && strings.HasSuffix(name, ".osm.pbf"):
			os.Remove(path) //nolint:errcheck // best effort
		case strings.HasSuffix(name, ".partial"):
			// The partial's own age says whether anyone is still writing it.
			if fi, err := en.Info(); err == nil && time.Since(fi.ModTime()) > abandonedPartialAge {
				os.Remove(path)           //nolint:errcheck // best effort
				os.Remove(path + ".meta") //nolint:errcheck // its version record goes with it
			}
		case strings.HasSuffix(name, ".partial.meta"):
			// A version record is written once, at the start, so its age says
			// nothing about a slow download. Only one with no partial is debris.
			if _, err := os.Stat(strings.TrimSuffix(path, ".meta")); os.IsNotExist(err) {
				if fi, err := en.Info(); err == nil && time.Since(fi.ModTime()) > abandonedPartialAge {
					os.Remove(path) //nolint:errcheck // best effort
				}
			}
		}
	}
}

// errShortFile reports a download that ended before the size the server stated.
func errShortFile(got, want int64) error {
	return fmt.Errorf("download incomplete: have %d of %d bytes", got, want)
}
