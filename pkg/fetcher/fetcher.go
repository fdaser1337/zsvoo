package fetcher

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mholt/archiver/v3"
	"golang.org/x/sync/errgroup"
)

// Fetcher handles downloading and extracting sources
type Fetcher struct {
	cacheDir string
}

// NewFetcher creates a new fetcher
func NewFetcher(cacheDir string) *Fetcher {
	return &Fetcher{
		cacheDir: cacheDir,
	}
}

// Download downloads a file from URL and verifies checksum when provided.
func (f *Fetcher) Download(url, expectedHash string) (string, error) {
	// Validate input parameters
	if url == "" {
		return "", fmt.Errorf("URL cannot be empty")
	}
	expectedHash = strings.ToLower(strings.TrimSpace(expectedHash))

	filename := filepath.Base(url)
	if filename == "" {
		return "", fmt.Errorf("invalid URL: no filename found")
	}

	cachePath := filepath.Join(f.cacheDir, filename)

	// Check if already downloaded and valid.
	if expectedHash != "" {
		if f.isValidCache(cachePath, expectedHash) {
			return cachePath, nil
		}
	} else {
		if _, err := os.Stat(cachePath); err == nil {
			return cachePath, nil
		}
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(f.cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Download file
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download %s: HTTP %d", url, resp.StatusCode)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp(f.cacheDir, "download-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	// Download to temp file
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, hasher), resp.Body); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	// Verify checksum
	if expectedHash != "" {
		calculatedHash := fmt.Sprintf("%x", hasher.Sum(nil))
		if calculatedHash != expectedHash {
			_ = os.Remove(tmpFile.Name())
			return "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, calculatedHash)
		}
	}

	// Move temp file to final location
	if err := os.Rename(tmpFile.Name(), cachePath); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to move downloaded file: %w", err)
	}

	return cachePath, nil
}

// Extract extracts an archive to destination
func (f *Fetcher) Extract(archivePath, destDir string) error {
	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Extract archive using archiver.Unarchive
	return archiver.Unarchive(archivePath, destDir)
}

// isValidCache checks if cached file exists and has correct checksum
func (f *Fetcher) isValidCache(path, expectedHash string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false
	}

	calculatedHash := fmt.Sprintf("%x", hasher.Sum(nil))
	return calculatedHash == expectedHash
}

// ApplyPatches applies patch files to source directory
func (f *Fetcher) ApplyPatches(sourceDir string, patchFiles []string) error {
	for _, patchFile := range patchFiles {
		if err := f.applyPatch(sourceDir, patchFile); err != nil {
			return fmt.Errorf("failed to apply patch %s: %w", patchFile, err)
		}
	}
	return nil
}

// applyPatch applies a single patch file
func (f *Fetcher) applyPatch(sourceDir, patchFile string) error {
	patchFile = filepath.Clean(patchFile)

	// Try common strip levels used by patch files.
	for _, stripLevel := range []string{"1", "0"} {
		cmd := exec.Command("patch", "-p"+stripLevel, "-i", patchFile)
		cmd.Dir = sourceDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("failed to apply patch with -p1 and -p0: %s", patchFile)
}

// DownloadAndExtract downloads and extracts source
func (f *Fetcher) DownloadAndExtract(url, expectedHash, destDir string) error {
	if isDebianDSCURL(url) {
		return f.downloadAndExtractFromDebianDSC(url, expectedHash, destDir)
	}

	archivePath, err := f.Download(url, expectedHash)
	if err != nil {
		return err
	}

	return f.Extract(archivePath, destDir)
}

func (f *Fetcher) downloadAndExtractFromDebianDSC(dscURL, dscHash, destDir string) error {
	dscPath, err := f.Download(dscURL, dscHash)
	if err != nil {
		return fmt.Errorf("failed to download debian dsc: %w", err)
	}

	entries, err := parseDebianDSCSHA256Entries(dscPath)
	if err != nil {
		return err
	}

	origEntries := make([]debianDSCEntry, 0, len(entries))
	for _, entry := range entries {
		if isDebianOrigArchive(entry.Name) {
			origEntries = append(origEntries, entry)
		}
	}
	if len(origEntries) == 0 {
		return fmt.Errorf("debian dsc does not contain upstream orig archive")
	}

	base, err := neturl.Parse(dscURL)
	if err != nil {
		return fmt.Errorf("invalid dsc URL %q: %w", dscURL, err)
	}

	for _, entry := range origEntries {
		ref, err := neturl.Parse(entry.Name)
		if err != nil {
			return fmt.Errorf("invalid dsc entry filename %q: %w", entry.Name, err)
		}

		fileURL := base.ResolveReference(ref).String()
		archivePath, err := f.Download(fileURL, entry.SHA256)
		if err != nil {
			return fmt.Errorf("failed to download upstream source %s: %w", entry.Name, err)
		}
		if err := f.Extract(archivePath, destDir); err != nil {
			return fmt.Errorf("failed to extract upstream source %s: %w", entry.Name, err)
		}
	}

	return nil
}

type debianDSCEntry struct {
	Name   string
	SHA256 string
}

func parseDebianDSCSHA256Entries(path string) ([]debianDSCEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open dsc file %s: %w", path, err)
	}
	defer file.Close()

	var entries []debianDSCEntry
	scanner := bufio.NewScanner(file)
	inSHA256Section := false

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if !inSHA256Section {
			if strings.HasPrefix(trimmed, "Checksums-Sha256:") {
				inSHA256Section = true
			}
			continue
		}

		if !isIndentedLine(line) {
			break
		}

		fields := strings.Fields(trimmed)
		if len(fields) < 3 {
			return nil, fmt.Errorf("invalid Checksums-Sha256 entry in dsc: %q", line)
		}
		entries = append(entries, debianDSCEntry{
			SHA256: strings.ToLower(fields[0]),
			Name:   fields[2],
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read dsc file %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("debian dsc missing Checksums-Sha256 entries")
	}

	return entries, nil
}

func isIndentedLine(line string) bool {
	if line == "" {
		return false
	}
	return line[0] == ' ' || line[0] == '\t'
}

func isDebianOrigArchive(name string) bool {
	idx := strings.Index(name, ".orig")
	if idx < 0 {
		return false
	}

	rest := name[idx+len(".orig"):]
	if hasTarSuffix(rest) {
		return true
	}
	if strings.HasPrefix(rest, "-") {
		tarIdx := strings.Index(rest, ".tar")
		if tarIdx >= 0 {
			componentTar := rest[tarIdx:]
			if hasTarSuffix(componentTar) {
				return true
			}
		}
	}
	return false
}

func hasTarSuffix(s string) bool {
	if !strings.HasPrefix(s, ".tar") {
		return false
	}
	if len(s) == len(".tar") {
		return true
	}
	return strings.HasPrefix(s[len(".tar"):], ".")
}

func isDebianDSCURL(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return false
	}
	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.HasSuffix(raw, ".dsc")
}

// DownloadMultiple downloads multiple files concurrently
func (f *Fetcher) DownloadMultiple(urls []string, hashes []string) ([]string, error) {
	if len(urls) != len(hashes) {
		return nil, fmt.Errorf("urls and hashes length mismatch")
	}

	paths := make([]string, len(urls))
	var g errgroup.Group
	for i, url := range urls {
		i, url := i, url
		g.Go(func() error {
			path, err := f.Download(url, hashes[i])
			if err != nil {
				return err
			}
			paths[i] = path
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return paths, nil
}

// GetCacheDir returns the cache directory
func (f *Fetcher) GetCacheDir() string {
	return f.cacheDir
}

// CleanCache removes old cached files
func (f *Fetcher) CleanCache() error {
	return os.RemoveAll(f.cacheDir)
}

// ListCachedFiles lists all cached files
func (f *Fetcher) ListCachedFiles() ([]string, error) {
	files, err := os.ReadDir(f.cacheDir)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, file := range files {
		if !file.IsDir() {
			result = append(result, file.Name())
		}
	}
	return result, nil
}
