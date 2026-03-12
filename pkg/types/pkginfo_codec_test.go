package types

import (
	"bytes"
	"reflect"
	"testing"
)

func TestPkgInfoCodecRoundTrip(t *testing.T) {
	t.Parallel()

	original := &PkgInfo{
		Name:         "demo",
		Version:      "1.2.3",
		Description:  "demo package",
		Dependencies: []string{"glibc", "zlib"},
		Files:        []string{"usr/bin/demo", "usr/share/doc/demo.txt"},
		InstallDate:  "2026-03-12T20:00:00Z",
	}

	var buf bytes.Buffer
	if err := WritePkgInfo(&buf, original); err != nil {
		t.Fatalf("WritePkgInfo() error = %v", err)
	}

	decoded, err := ReadPkgInfo(&buf)
	if err != nil {
		t.Fatalf("ReadPkgInfo() error = %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("decoded pkginfo mismatch:\nwant: %#v\ngot:  %#v", original, decoded)
	}
}
