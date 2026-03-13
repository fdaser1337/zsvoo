package fetcher

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadAndExtractDebianDSCUsesOnlyOrig(t *testing.T) {
	origArchive := buildTarGz(t, map[string]string{
		"demo-1.0/README": "hello upstream\n",
	})
	debianPatch := []byte("pretend debian patch content")

	origHash := hashHex(origArchive)
	debianHash := hashHex(debianPatch)

	dsc := fmt.Sprintf(`Format: 3.0 (quilt)
Source: demo
Checksums-Sha256:
 %s %d demo_1.0.orig.tar.gz
 %s %d demo_1.0-1.debian.tar.xz
`, origHash, len(origArchive), debianHash, len(debianPatch))

	hits := map[string]int{}
	withHTTPTransport(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hits[req.URL.Path]++
		switch req.URL.Path {
		case "/pool/main/d/demo/demo_1.0-1.dsc":
			return httpResponse(req, http.StatusOK, []byte(dsc)), nil
		case "/pool/main/d/demo/demo_1.0.orig.tar.gz":
			return httpResponse(req, http.StatusOK, origArchive), nil
		case "/pool/main/d/demo/demo_1.0-1.debian.tar.xz":
			return httpResponse(req, http.StatusOK, debianPatch), nil
		default:
			return httpResponse(req, http.StatusNotFound, []byte("not found")), nil
		}
	}))

	workDir := t.TempDir()
	f := NewFetcher(filepath.Join(workDir, "cache"))
	destDir := filepath.Join(workDir, "src")

	err := f.DownloadAndExtract("https://deb.example/pool/main/d/demo/demo_1.0-1.dsc", "", destDir)
	if err != nil {
		t.Fatalf("DownloadAndExtract() error = %v", err)
	}

	readmePath := filepath.Join(destDir, "demo-1.0", "README")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed to read extracted file %s: %v", readmePath, err)
	}
	if string(content) != "hello upstream\n" {
		t.Fatalf("unexpected extracted file content: %q", string(content))
	}

	debianHits := hits["/pool/main/d/demo/demo_1.0-1.debian.tar.xz"]
	origHits := hits["/pool/main/d/demo/demo_1.0.orig.tar.gz"]
	dscHits := hits["/pool/main/d/demo/demo_1.0-1.dsc"]

	if dscHits == 0 || origHits == 0 {
		t.Fatalf("expected dsc and orig to be downloaded, got dsc=%d orig=%d", dscHits, origHits)
	}
	if debianHits != 0 {
		t.Fatalf("expected debian patch archive to be ignored, got %d requests", debianHits)
	}
}

func TestDownloadAndExtractDebianDSCChecksumMismatch(t *testing.T) {
	withHTTPTransport(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(req, http.StatusOK, []byte("Format: 3.0 (quilt)\nSource: demo\n")), nil
	}))

	f := NewFetcher(filepath.Join(t.TempDir(), "cache"))
	err := f.DownloadAndExtract("https://deb.example/demo_1.0-1.dsc", "deadbeef", t.TempDir())
	if err == nil {
		t.Fatalf("expected checksum mismatch error")
	}
}

func TestIsDebianOrigArchive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "pkg_1.0.orig.tar.gz", want: true},
		{name: "pkg_1.0.orig.tar.xz", want: true},
		{name: "pkg_1.0.orig.tar", want: true},
		{name: "pkg_1.0.orig-data.tar.zst", want: true},
		{name: "pkg_1.0-1.debian.tar.xz", want: false},
		{name: "pkg_1.0-1.diff.gz", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isDebianOrigArchive(tt.name); got != tt.want {
				t.Fatalf("isDebianOrigArchive(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withHTTPTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = orig
	})
}

func httpResponse(req *http.Request, code int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write tar body: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	return buf.Bytes()
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
