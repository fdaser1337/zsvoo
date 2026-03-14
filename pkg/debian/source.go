package debian

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"
)

var packageNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*$`)

const defaultDebianMirror = "https://deb.debian.org/debian"

var (
	defaultSuites     = []string{"stable"} // Только stable для скорости
	defaultComponents = []string{"main"}   // Только main для скорости
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

const maxAutoBuildDepth = 15 // Увеличим для сложных цепочек зависимостей
const parallelWorkers = 20   // Параллельный поиск зависимостей (HTTP запросы)
const buildWorkers = 4       // Параллельное построение пакетов

// CachedSources holds parsed Sources data for fast lookups
type CachedSources struct {
	packages map[string]*sourceRecord // key: package name
	path     string                   // cache file path
	mu       sync.RWMutex
}

// Resolver queries Debian source metadata over HTTP.
type Resolver struct {
	client        *http.Client
	mirrors       []string
	suites        []string
	components    []string
	cache         map[string]*SourceInfo    // Кеш найденных пакетов
	cacheMu       sync.RWMutex              // Защита кеша
	cachedSources map[string]*CachedSources // Кешированные Sources файлы по ключу
	sourcesMu     sync.RWMutex              // Защита cachedSources map
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
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		mirrors: []string{
			defaultDebianMirror, // deb.debian.org (CDN)
		},
		suites:        append([]string(nil), defaultSuites...),
		components:    append([]string(nil), defaultComponents...),
		cache:         make(map[string]*SourceInfo),
		cachedSources: make(map[string]*CachedSources),
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

	// Проверяем кеш
	r.cacheMu.RLock()
	if cached, ok := r.cache[pkg]; ok {
		r.cacheMu.RUnlock()
		return cached, nil
	}
	r.cacheMu.RUnlock()

	fmt.Printf("  [resolver] Looking up %s...\n", pkg)
	start := time.Now()
	defer func() {
		fmt.Printf("  [resolver] %s lookup took %v\n", pkg, time.Since(start))
	}()

	checked := make([]string, 0, len(r.mirrors)*len(r.suites)*len(r.components))
	for _, mirror := range r.mirrors {
		for _, suite := range r.suites {
			for _, component := range r.components {
				fmt.Printf("  [resolver] Checking %s/%s/%s...\n", mirror, suite, component)
				record, err := r.findPackageInIndex(mirror, suite, component, pkg)
				checked = append(checked, fmt.Sprintf("%s:%s/%s", mirror, suite, component))
				if err != nil {
					fmt.Printf("  [resolver] Not found in %s/%s/%s: %v\n", mirror, suite, component, err)
					continue
				}

				result := &SourceInfo{
					RequestedPackage: pkg,
					SourcePackage:    record.Package,
					DSCURL:           strings.TrimRight(mirror, "/") + "/" + path.Join(record.Directory, record.DSCName),
					DSCSHA256:        record.DSCSHA256,
					DebianVersion:    record.Version,
					UpstreamVersion:  normalizeUpstreamVersion(record.Version),
					Suite:            suite,
					Component:        component,
					BuildDepends:     record.BuildDepends,
				}

				// Сохраняем в кеш
				r.cacheMu.Lock()
				r.cache[pkg] = result
				r.cacheMu.Unlock()

				fmt.Printf("  [resolver] ✓ Found %s in %s/%s/%s\n", pkg, mirror, suite, component)
				return result, nil
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
	// First, ensure Sources file is loaded into memory
	cached, err := r.loadCachedSources(mirror, suite, component)
	if err != nil {
		return nil, err
	}

	// Lookup from in-memory map (O(1) instead of HTTP scan)
	if rec, found := r.lookupFromCache(pkg, cached); found {
		return rec, nil
	}

	return nil, fmt.Errorf("package not found in cache")
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

	// Find the last dash that separates upstream version from Debian revision
	// For versions like "1.0-1-ubuntu1", we want to cut at the first dash to get "1.0"
	if idx := strings.Index(v, "-"); idx >= 0 {
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

// cacheDir returns the cache directory for zsvo
func cacheDir() string {
	if dir := os.Getenv("ZSVO_CACHE"); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "zsvo")
	}
	return "/var/cache/zsvo"
}

// ensureCacheDir creates the cache directory if it doesn't exist
func ensureCacheDir() error {
	dir := cacheDir()
	return os.MkdirAll(dir, 0755)
}

// cachePath returns the path for a cached Sources file
func (r *Resolver) cachePath(mirror, suite, component string) string {
	host := strings.ReplaceAll(strings.TrimPrefix(mirror, "https://"), "/", "_")
	return filepath.Join(cacheDir(), fmt.Sprintf("Sources_%s_%s_%s.xz", host, suite, component))
}

// cacheKey returns a unique key for mirror/suite/component combination
func (r *Resolver) cacheKey(mirror, suite, component string) string {
	return fmt.Sprintf("%s:%s:%s", mirror, suite, component)
}

// loadCachedSources loads Sources.xz from cache or downloads it if not exists
func (r *Resolver) loadCachedSources(mirror, suite, component string) (*CachedSources, error) {
	key := r.cacheKey(mirror, suite, component)

	// Fast path: check if already loaded
	r.sourcesMu.RLock()
	if cached, exists := r.cachedSources[key]; exists {
		r.sourcesMu.RUnlock()
		return cached, nil
	}
	r.sourcesMu.RUnlock()

	// Slow path: load with write lock
	r.sourcesMu.Lock()
	defer r.sourcesMu.Unlock()

	// Double-check after acquiring write lock
	if cached, exists := r.cachedSources[key]; exists {
		return cached, nil
	}

	cachePath := r.cachePath(mirror, suite, component)
	cached := &CachedSources{
		packages: make(map[string]*sourceRecord),
		path:     cachePath,
	}

	// Try to load from disk cache first
	if _, err := os.Stat(cachePath); err == nil {
		// File exists on disk, parse it
		if err := r.parseSourcesFileToCache(cachePath, mirror, cached); err == nil {
			r.cachedSources[key] = cached
			return cached, nil
		}
	}

	// Download from HTTP
	url := buildSourcesURL(mirror, suite, component, "xz")
	if err := r.downloadSources(url, cachePath); err != nil {
		// Try gz
		url = buildSourcesURL(mirror, suite, component, "gz")
		cachePath = strings.TrimSuffix(cachePath, ".xz") + ".gz"
		if err := r.downloadSources(url, cachePath); err != nil {
			// Try uncompressed
			url = buildSourcesURL(mirror, suite, component, "")
			cachePath = strings.TrimSuffix(cachePath, ".gz")
			if err := r.downloadSources(url, cachePath); err != nil {
				return nil, err
			}
		}
	}

	// Parse the downloaded file
	if err := r.parseSourcesFileToCache(cachePath, mirror, cached); err != nil {
		return nil, err
	}

	r.cachedSources[key] = cached
	return cached, nil
}

// downloadSources downloads a Sources file from URL to local path
func (r *Resolver) downloadSources(url, localPath string) error {
	fmt.Printf("  [resolver] Downloading %s...\n", url)
	start := time.Now()

	resp, err := r.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}

	if err := ensureCacheDir(); err != nil {
		return err
	}

	file, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		os.Remove(localPath)
		return err
	}

	fmt.Printf("  [resolver] Downloaded in %v\n", time.Since(start))
	return nil
}

// parseSourcesFileToCache parses a local Sources file into specific CachedSources instance
func (r *Resolver) parseSourcesFileToCache(path, mirror string, cached *CachedSources) error {
	fmt.Printf("  [resolver] Parsing %s...\n", filepath.Base(path))
	start := time.Now()

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	var reader io.Reader = file

	// Decompress if needed
	if strings.HasSuffix(path, ".xz") {
		r, err := xz.NewReader(file)
		if err != nil {
			return err
		}
		reader = r
	} else if strings.HasSuffix(path, ".gz") {
		r, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer r.Close()
		reader = r
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	paragraph := make([]string, 0, 32)
	count := 0

	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		rec, err := parseSourcesParagraph(paragraph)
		paragraph = paragraph[:0]
		if err != nil {
			return
		}

		cached.mu.Lock()
		cached.packages[rec.Package] = &rec
		// Also index by binary names
		for _, bin := range rec.Binaries {
			bin = strings.TrimSpace(strings.ToLower(bin))
			if bin != "" && bin != rec.Package {
				cached.packages[bin] = &rec
			}
		}
		cached.mu.Unlock()
		count++
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		paragraph = append(paragraph, line)
	}

	flush()

	if err := scanner.Err(); err != nil {
		return err
	}

	cached.path = path
	fmt.Printf("  [resolver] Parsed %d packages in %v\n", count, time.Since(start))
	return nil
}

// lookupFromCache searches for a package in the in-memory cache
func (r *Resolver) lookupFromCache(pkg string, cached *CachedSources) (*sourceRecord, bool) {
	cached.mu.RLock()
	defer cached.mu.RUnlock()

	if rec, ok := cached.packages[pkg]; ok {
		return rec, true
	}
	return nil, false
}
