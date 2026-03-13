#!/bin/bash

# ZSVO Linux Build Script
# Supports building for Linux architectures

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
BINARY_NAME="zsvo"
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS="-ldflags \"-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}\""

# Linux platforms
declare -A PLATFORMS=(
    ["amd64"]="x86_64-linux-gnu"
    ["arm64"]="aarch64-linux-gnu"
    ["386"]="i386-linux-gnu"
    ["arm"]="arm-linux-gnueabihf"
)

# Functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_dependencies() {
    log_info "Checking dependencies..."
    
    if ! command -v go &> /dev/null; then
        log_error "Go is not installed"
        exit 1
    fi
    
    if ! command -v git &> /dev/null; then
        log_warning "Git is not installed (version detection may not work)"
    fi
    
    log_success "Dependencies check passed"
}

clean_build() {
    log_info "Cleaning build artifacts..."
    rm -rf bin/
    rm -rf dist/
    rm -rf release/
    go clean -cache
    log_success "Clean completed"
}

build_platform() {
    local arch=$1
    local output_dir="dist/$arch"
    local output_name="$BINARY_NAME-$arch"
    
    log_info "Building $arch..."
    
    mkdir -p "$output_dir"
    
    GOOS=linux GOARCH=$arch go build $LDFLAGS -o "$output_dir/$output_name" .
    
    if [ $? -eq 0 ]; then
        log_success "Built $arch successfully"
        
        # Create checksum
        cd "$output_dir"
        if command -v sha256sum &> /dev/null; then
            sha256sum "$output_name" > "$output_name.sha256"
        elif command -v shasum &> /dev/null; then
            shasum -a 256 "$output_name" > "$output_name.sha256"
        fi
        cd - > /dev/null
        
        # Get binary size
        local size=$(du -h "$output_dir/$output_name" | cut -f1)
        log_info "Binary size: $size"
    else
        log_error "Failed to build $arch"
        return 1
    fi
}

build_all() {
    log_info "Building ZSVO for Linux..."
    log_info "Version: $VERSION"
    log_info "Build Time: $BUILD_TIME"
    
    local total=${#PLATFORMS[@]}
    local current=0
    
    for arch in "${!PLATFORMS[@]}"; do
        ((current++))
        log_info "[$current/$total] Building $arch..."
        
        if build_platform "$arch"; then
            log_success "[$current/$total] ✓ $arch"
        else
            log_error "[$current/$total] ✗ $arch"
        fi
    done
    
    log_success "Build complete! Binaries are in dist/"
}

build_specific() {
    local target_arch=$1
    
    if [ -z "$target_arch" ]; then
        log_error "Usage: $0 build <arch>"
        log_info "Example: $0 build amd64"
        log_info "Available: ${!PLATFORMS[@]}"
        exit 1
    fi
    
    if [[ -z "${PLATFORMS[$target_arch]}" ]]; then
        log_error "Unsupported architecture: $target_arch"
        log_info "Available architectures:"
        for arch in "${!PLATFORMS[@]}"; do
            echo "  - $arch"
        done
        exit 1
    fi
    
    build_platform "$target_arch"
}

create_release() {
    log_info "Creating release packages..."
    
    mkdir -p release
    
    for arch in "${!PLATFORMS[@]}"; do
        local output_dir="dist/$arch"
        local binary_name="$BINARY_NAME-$arch"
        local release_name="$BINARY_NAME-$VERSION-$arch"
        local release_dir="release/$release_name"
        
        if [ -f "$output_dir/$binary_name" ]; then
            log_info "Creating package for $arch..."
            
            mkdir -p "$release_dir"
            cp "$output_dir/$binary_name" "$release_dir/$BINARY_NAME"
            cp "$output_dir/$binary_name.sha256" "$release_dir/" 2>/dev/null || true
            cp README.md "$release_dir/" 2>/dev/null || true
            cp LICENSE "$release_dir/" 2>/dev/null || true
            
            # Create archive
            cd release
            tar -czf "$release_name.tar.gz" "$release_name"
            log_success "Created $release_name.tar.gz"
            cd - > /dev/null
        else
            log_warning "Binary not found for $arch: $output_dir/$binary_name"
        fi
    done
    
    log_success "Release packages created in release/"
}

show_info() {
    log_info "ZSVO Build Information:"
    echo "  Binary: $BINARY_NAME"
    echo "  Version: $VERSION"
    echo "  Build Time: $BUILD_TIME"
    echo "  Go Version: $(go version)"
    echo ""
    echo "Supported Architectures:"
    for arch in "${!PLATFORMS[@]}"; do
        echo "  - $arch (${PLATFORMS[$arch]})"
    done
}

# Main script
case "${1:-help}" in
    "clean")
        clean_build
        ;;
    "build")
        build_specific "$2"
        ;;
    "all")
        check_dependencies
        clean_build
        build_all
        ;;
    "release")
        check_dependencies
        clean_build
        build_all
        create_release
        ;;
    "info")
        show_info
        ;;
    "help"|*)
        echo "ZSVO Linux Build Script"
        echo ""
        echo "Usage: $0 [command] [options]"
        echo ""
        echo "Commands:"
        echo "  clean                    Clean build artifacts"
        echo "  build <arch>              Build for specific architecture"
        echo "  all                      Build for all architectures"
        echo "  release                  Build and create release packages"
        echo "  info                     Show build information"
        echo "  help                     Show this help"
        echo ""
        echo "Architectures:"
        for arch in "${!PLATFORMS[@]}"; do
            echo "  - $arch"
        done
        echo ""
        echo "Examples:"
        echo "  $0 all"
        echo "  $0 build amd64"
        echo "  $0 build arm64"
        echo "  $0 release"
        echo ""
        echo "Environment Variables:"
        echo "  VERSION                  Override version (default: git describe)"
        exit 0
        ;;
esac
