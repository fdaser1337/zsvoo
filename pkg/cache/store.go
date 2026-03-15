package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store implements content-addressable storage like Nix
type Store struct {
	basePath string
	mu       sync.RWMutex
	remote   RemoteCache
}

// RemoteCache interface for binary substitution
type RemoteCache interface {
	Get(narHash string) (string, error) // Download to local path
	Put(localPath, narHash string) error
	Has(narHash string) bool
}

// NewStore creates content-addressable store
func NewStore(basePath string) (*Store, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, err
	}

	// Create subdirectories like Nix: /nix/store/xx/xxxxx...
	for i := 0; i < 256; i++ {
		dir := filepath.Join(basePath, fmt.Sprintf("%02x", i))
		os.MkdirAll(dir, 0755)
	}

	return &Store{basePath: basePath}, nil
}

// ComputeHash computes content hash like Nix narHash
func (s *Store) ComputeHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// StorePath returns store path for content hash
func (s *Store) StorePath(narHash string) string {
	// Nix-style: /store/xx/xxxxxxxxxxxx...
	prefix := narHash[:2]
	return filepath.Join(s.basePath, prefix, narHash)
}

// Add adds file to store, returns store path
func (s *Store) Add(srcPath string) (string, error) {
	hash, err := s.ComputeHash(srcPath)
	if err != nil {
		return "", err
	}

	dstPath := s.StorePath(hash)

	// Atomic add: create temp, rename
	tempPath := dstPath + ".tmp"

	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.Create(tempPath)
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tempPath)
		return "", err
	}

	dst.Close()

	// Atomic rename
	if err := os.Rename(tempPath, dstPath); err != nil {
		os.Remove(tempPath)
		return "", err
	}

	return dstPath, nil
}

// Has checks if content exists in store
func (s *Store) Has(narHash string) bool {
	path := s.StorePath(narHash)
	_, err := os.Stat(path)
	return err == nil
}

// Get retrieves file from store or remote cache
func (s *Store) Get(narHash string) (string, error) {
	localPath := s.StorePath(narHash)

	// Check local store
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// Try remote cache
	if s.remote != nil && s.remote.Has(narHash) {
		if _, err := s.remote.Get(narHash); err == nil {
			return localPath, nil
		}
	}

	return "", fmt.Errorf("content not found: %s", narHash)
}

// BinaryCache implements remote binary cache
type BinaryCache struct {
	urls []string
	keys []string // signing keys
}

// NewBinaryCache creates binary cache client
func NewBinaryCache(urls []string) *BinaryCache {
	return &BinaryCache{urls: urls}
}

// Query queries remote cache for package availability
func (b *BinaryCache) Query(pkgName, version, platform string) (string, bool) {
	// Check all configured caches
	for _, url := range b.urls {
		narPath := fmt.Sprintf("%s/%s-%s-%s.nar.zst", url, pkgName, version, platform)
		// HEAD request to check existence
		if exists(narPath) {
			return narPath, true
		}
	}
	return "", false
}

func exists(url string) bool {
	// Simplified - would do actual HTTP HEAD
	return false
}

// Download downloads and verifies package
func (b *BinaryCache) Download(narUrl, dstPath string) error {
	// Download with progress
	// Verify signature
	// Extract to store
	return nil
}

// Upload uploads to cache (for CI/build farm)
func (b *BinaryCache) Upload(srcPath, narUrl string) error {
	return nil
}

// SubstitutionPlan plans binary substitution like Nix
type SubstitutionPlan struct {
	store     *Store
	cache     *BinaryCache
	needed    []string          // hashes needed
	available map[string]string // hash -> store path
}

// NewSubstitutionPlan creates substitution plan
func NewSubstitutionPlan(store *Store, cache *BinaryCache) *SubstitutionPlan {
	return &SubstitutionPlan{
		store:     store,
		cache:     cache,
		available: make(map[string]string),
	}
}

// AddDependency adds dependency to plan
func (p *SubstitutionPlan) AddDependency(pkgName, version, platform string) {
	if path, ok := p.cache.Query(pkgName, version, platform); ok {
		hash := extractHashFromPath(path)
		p.available[hash] = path
	}
}

// CanSubstitute checks if we can avoid building
func (p *SubstitutionPlan) CanSubstitute() bool {
	return len(p.needed) == len(p.available)
}

// Execute executes substitution plan
func (p *SubstitutionPlan) Execute() error {
	for hash, remotePath := range p.available {
		localPath := p.store.StorePath(hash)
		if err := p.cache.Download(remotePath, localPath); err != nil {
			return err
		}
	}
	return nil
}

func extractHashFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}
