package debian

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResolveSourceHTTPFallbackToGzip(t *testing.T) {
	t.Parallel()

	sources := strings.TrimSpace(`
Package: neofetch
Binary: neofetch
Version: 7.1.0-4
Directory: pool/main/n/neofetch
Checksums-Sha256:
 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1978 neofetch_7.1.0-4.dsc
 bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 73791 neofetch_7.1.0.orig.tar.gz
`) + "\n"

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(sources)); err != nil {
		t.Fatalf("failed to write gzip sources: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/debian/dists/stable/main/source/Sources.xz":
				return httpResponse(req, http.StatusNotFound, []byte("not found")), nil
			case "/debian/dists/stable/main/source/Sources.gz":
				return httpResponse(req, http.StatusOK, gz.Bytes()), nil
			default:
				return httpResponse(req, http.StatusNotFound, []byte("missing")), nil
			}
		}),
	}

	r := NewResolver(
		WithHTTPClient(client),
		WithMirrors([]string{"https://mirror.example/debian"}),
		WithSuites([]string{"stable"}),
		WithComponents([]string{"main"}),
	)

	info, err := r.ResolveSource("neofetch")
	if err != nil {
		t.Fatalf("ResolveSource() error = %v", err)
	}

	if info.SourcePackage != "neofetch" {
		t.Fatalf("unexpected source package: %s", info.SourcePackage)
	}
	if info.DebianVersion != "7.1.0-4" {
		t.Fatalf("unexpected debian version: %s", info.DebianVersion)
	}
	if info.UpstreamVersion != "7.1.0" {
		t.Fatalf("unexpected upstream version: %s", info.UpstreamVersion)
	}
	wantURL := "https://mirror.example/debian/pool/main/n/neofetch/neofetch_7.1.0-4.dsc"
	if info.DSCURL != wantURL {
		t.Fatalf("unexpected dsc url:\nwant: %s\ngot:  %s", wantURL, info.DSCURL)
	}
}

func TestResolveSourceByBinaryName(t *testing.T) {
	t.Parallel()

	sources := strings.TrimSpace(`
Package: pkgconf
Binary: pkgconf, pkg-config
Version: 2.0.3-1
Directory: pool/main/p/pkgconf
Checksums-Sha256:
 cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc 1800 pkgconf_2.0.3-1.dsc
`) + "\n"

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/Sources") {
				return httpResponse(req, http.StatusOK, []byte(sources)), nil
			}
			return httpResponse(req, http.StatusNotFound, []byte("missing")), nil
		}),
	}

	r := NewResolver(
		WithHTTPClient(client),
		WithMirrors([]string{"https://mirror.example/debian"}),
		WithSuites([]string{"stable"}),
		WithComponents([]string{"main"}),
	)

	info, err := r.ResolveSource("pkg-config")
	if err != nil {
		t.Fatalf("ResolveSource() error = %v", err)
	}

	if info.SourcePackage != "pkgconf" {
		t.Fatalf("unexpected source package: %s", info.SourcePackage)
	}
	wantURL := "https://mirror.example/debian/pool/main/p/pkgconf/pkgconf_2.0.3-1.dsc"
	if info.DSCURL != wantURL {
		t.Fatalf("unexpected dsc url:\nwant: %s\ngot:  %s", wantURL, info.DSCURL)
	}
}

func TestResolveSourceHTTPNotFound(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return httpResponse(req, http.StatusNotFound, []byte("not found")), nil
		}),
	}

	r := NewResolver(
		WithHTTPClient(client),
		WithMirrors([]string{"https://mirror.example/debian"}),
		WithSuites([]string{"stable"}),
		WithComponents([]string{"main"}),
	)

	_, err := r.ResolveSource("no-such-package")
	if err == nil {
		t.Fatalf("expected not found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseSourcesParagraph(t *testing.T) {
	t.Parallel()

	paragraph := []string{
		"Package: foo",
		"Version: 1:2.3.4-5",
		"Directory: pool/main/f/foo",
		"Checksums-Sha256:",
		" aaaa 10 foo_1:2.3.4-5.dsc",
		" bbbb 20 foo_2.3.4.orig.tar.gz",
	}

	rec, err := parseSourcesParagraph(paragraph)
	if err != nil {
		t.Fatalf("parseSourcesParagraph() error = %v", err)
	}
	if rec.Package != "foo" || rec.Version != "1:2.3.4-5" {
		t.Fatalf("unexpected record identity: %s %s", rec.Package, rec.Version)
	}
	if rec.Directory != "pool/main/f/foo" {
		t.Fatalf("unexpected directory: %s", rec.Directory)
	}
	if rec.DSCName != "foo_1:2.3.4-5.dsc" {
		t.Fatalf("unexpected dsc name: %s", rec.DSCName)
	}
	if rec.DSCSHA256 != "aaaa" {
		t.Fatalf("unexpected dsc hash: %s", rec.DSCSHA256)
	}
	if got := normalizeUpstreamVersion(rec.Version); got != "2.3.4" {
		t.Fatalf("unexpected normalized version: %s", got)
	}
}

func TestResolveSourceInvalidPackageName(t *testing.T) {
	t.Parallel()

	r := NewResolver()
	_, err := r.ResolveSource("bad/name")
	if err == nil {
		t.Fatalf("expected package name validation error")
	}
}

func httpResponse(req *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
	}
}
