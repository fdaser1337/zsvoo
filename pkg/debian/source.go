package debian

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
)

var packageNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*$`)

const defaultDebianMirror = "https://deb.debian.org/debian"

var (
	defaultSuites     = []string{"stable", "testing", "unstable"}
	defaultComponents = []string{"main", "contrib", "non-free", "non-free-firmware"}
)

// SourceInfo describes Debian source package coordinates resolved over HTTP.
type SourceInfo struct {
	RequestedPackage string
	SourcePackage    string
	DSCURL           string
	DSCSHA256        string
	DebianVersion    string
	UpstreamVersion  string
	Suite            string
	Component        string
	BuildDepends     []string // Build dependencies from DSC file
}

// Resolver queries Debian source metadata over HTTP.
type Resolver struct {
	client     *http.Client
	mirrors    []string
	suites     []string
	components []string
}

// ResolverOption customizes resolver behavior.
type ResolverOption func(*Resolver)

func WithHTTPClient(client *http.Client) ResolverOption {
	return func(r *Resolver) {
		if client != nil {
			r.client = client
		}
	}
}

func WithMirrors(mirrors []string) ResolverOption {
	return func(r *Resolver) {
		r.mirrors = normalizeList(mirrors)
	}
}

func WithSuites(suites []string) ResolverOption {
	return func(r *Resolver) {
		r.suites = normalizeList(suites)
	}
}

func WithComponents(components []string) ResolverOption {
	return func(r *Resolver) {
		r.components = normalizeList(components)
	}
}

func NewResolver(opts ...ResolverOption) *Resolver {
	r := &Resolver{
		client: &http.Client{Timeout: 30 * time.Second},
		mirrors: []string{
			defaultDebianMirror,
		},
		suites:     append([]string(nil), defaultSuites...),
		components: append([]string(nil), defaultComponents...),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}

	if len(r.mirrors) == 0 {
		r.mirrors = []string{defaultDebianMirror}
	}
	if len(r.suites) == 0 {
		r.suites = append([]string(nil), defaultSuites...)
	}
	if len(r.components) == 0 {
		r.components = append([]string(nil), defaultComponents...)
	}

	return r
}

func (r *Resolver) ResolveSource(pkg string) (*SourceInfo, error) {
	pkg = strings.TrimSpace(strings.ToLower(pkg))
	if !packageNamePattern.MatchString(pkg) {
		return nil, fmt.Errorf("invalid package name %q", pkg)
	}

	checked := make([]string, 0, len(r.mirrors)*len(r.suites)*len(r.components))
	for _, mirror := range r.mirrors {
		for _, suite := range r.suites {
			for _, component := range r.components {
				record, err := r.findPackageInIndex(mirror, suite, component, pkg)
				checked = append(checked, fmt.Sprintf("%s:%s/%s", mirror, suite, component))
				if err != nil {
					continue
				}

				return &SourceInfo{
					RequestedPackage: pkg,
					SourcePackage:    record.Package,
					DSCURL:           strings.TrimRight(mirror, "/") + "/" + path.Join(record.Directory, record.DSCName),
					DSCSHA256:        record.DSCSHA256,
					DebianVersion:    record.Version,
					UpstreamVersion:  normalizeUpstreamVersion(record.Version),
					Suite:            suite,
					Component:        component,
					BuildDepends:     record.BuildDepends,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf(
		"source package %s not found via HTTP (checked: %s)",
		pkg,
		strings.Join(checked, ", "),
	)
}

type sourceRecord struct {
	Package      string
	Version      string
	Directory    string
	DSCName      string
	DSCSHA256    string
	Binaries     []string
	BuildDepends []string
}

func (r *Resolver) findPackageInIndex(mirror, suite, component, pkg string) (*sourceRecord, error) {
	variants := []struct {
		ext     string
		decoder func(io.Reader) (io.Reader, error)
	}{
		{ext: "xz", decoder: decodeXZ},
		{ext: "gz", decoder: decodeGzip},
		{ext: "", decoder: passthrough},
	}

	var firstErr error
	for _, v := range variants {
		indexURL := buildSourcesURL(mirror, suite, component, v.ext)
		record, err := r.findInSingleIndex(indexURL, v.decoder, pkg)
		if err == nil {
			return record, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr == nil {
		firstErr = fmt.Errorf("could not read sources index")
	}
	return nil, firstErr
}

func (r *Resolver) findInSingleIndex(indexURL string, decoder func(io.Reader) (io.Reader, error), pkg string) (*sourceRecord, error) {
	resp, err := r.client.Get(indexURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	reader, err := decoder(resp.Body)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	paragraph := make([]string, 0, 32)

	flush := func() (*sourceRecord, bool, error) {
		if len(paragraph) == 0 {
			return nil, false, nil
		}
		rec, err := parseSourcesParagraph(paragraph)
		paragraph = paragraph[:0]
		if err != nil {
			return nil, false, nil
		}
		if rec.Package == pkg || containsSourceBinary(rec.Binaries, pkg) {
			return &rec, true, nil
		}
		return nil, false, nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			rec, ok, err := flush()
			if err != nil {
				return nil, err
			}
			if ok {
				return rec, nil
			}
			continue
		}
		paragraph = append(paragraph, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	rec, ok, err := flush()
	if err != nil {
		return nil, err
	}
	if ok {
		return rec, nil
	}

	return nil, fmt.Errorf("package not found in %s", indexURL)
}

func buildSourcesURL(mirror, suite, component, ext string) string {
	mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
	suite = strings.Trim(strings.TrimSpace(suite), "/")
	component = strings.Trim(strings.TrimSpace(component), "/")

	name := "Sources"
	if ext != "" {
		name += "." + ext
	}

	return mirror + "/" + path.Join("dists", suite, component, "source", name)
}

func parseSourcesParagraph(lines []string) (sourceRecord, error) {
	fields := make(map[string]string)
	var currentKey string

	for _, raw := range lines {
		if strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t") {
			if currentKey == "" {
				continue
			}
			fields[currentKey] += "\n" + strings.TrimSpace(raw)
			continue
		}

		idx := strings.IndexByte(raw, ':')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(raw[:idx])
		value := strings.TrimSpace(raw[idx+1:])
		fields[key] = value
		currentKey = key
	}

	pkg := fields["Package"]
	ver := fields["Version"]
	dir := fields["Directory"]
	if pkg == "" || ver == "" || dir == "" {
		return sourceRecord{}, fmt.Errorf("missing required fields")
	}

	dscName, dscHash := parseChecksumsForDSC(fields["Checksums-Sha256"])
	if dscName == "" {
		return sourceRecord{}, fmt.Errorf("no dsc in checksums")
	}

	dir = strings.Trim(strings.TrimSpace(dir), "/")

	return sourceRecord{
		Package:      pkg,
		Version:      ver,
		Directory:    dir,
		DSCName:      dscName,
		DSCSHA256:    dscHash,
		Binaries:     parseCommaSeparatedField(fields["Binary"]),
		BuildDepends: parseCommaSeparatedField(fields["Build-Depends"]),
	}, nil
}

func parseCommaSeparatedField(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func containsSourceBinary(names []string, wanted string) bool {
	wanted = strings.TrimSpace(strings.ToLower(wanted))
	if wanted == "" {
		return false
	}
	for _, name := range names {
		if strings.TrimSpace(strings.ToLower(name)) == wanted {
			return true
		}
	}
	return false
}

func parseChecksumsForDSC(raw string) (string, string) {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 {
			continue
		}
		name := fields[2]
		if strings.HasSuffix(strings.ToLower(name), ".dsc") {
			return name, strings.ToLower(fields[0])
		}
	}
	return "", ""
}

func decodeXZ(r io.Reader) (io.Reader, error) {
	return xz.NewReader(r)
}

func decodeGzip(r io.Reader) (io.Reader, error) {
	return gzip.NewReader(r)
}

func passthrough(r io.Reader) (io.Reader, error) {
	return r, nil
}

func normalizeUpstreamVersion(debianVersion string) string {
	v := strings.TrimSpace(debianVersion)
	if v == "" {
		return "0"
	}

	if idx := strings.IndexByte(v, ':'); idx >= 0 {
		v = v[idx+1:]
	}

	if idx := strings.LastIndex(v, "-"); idx > 0 {
		v = v[:idx]
	}

	replacer := strings.NewReplacer(
		"/", "_",
		":", "_",
		" ", "_",
	)
	v = strings.TrimSpace(replacer.Replace(v))
	if v == "" {
		return "0"
	}
	return v
}

func normalizeList(in []string) []string {
	set := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, exists := set[s]; exists {
			continue
		}
		set[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
