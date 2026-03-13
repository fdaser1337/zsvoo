#!/bin/bash

# ZSVO Docker Build Script
# Linux-only Docker builds

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
IMAGE_NAME="zsvo"
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
REGISTRY=${REGISTRY:-"localhost:5000"}

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

check_docker() {
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed"
        exit 1
    fi
    
    if ! docker info &> /dev/null; then
        log_error "Docker daemon is not running"
        exit 1
    fi
    
    log_success "Docker check passed"
}

build_image() {
    local tag=${1:-$IMAGE_NAME:$VERSION}
    
    log_info "Building Docker image..."
    docker build -t $tag .
    log_success "Built Docker image: $tag"
}

run_container() {
    local tag=${1:-$IMAGE_NAME:$VERSION}
    local command=${2:-"--help"}
    
    log_info "Running container: $tag"
    log_info "Command: zsvo $command"
    docker run --rm -it $tag zsvo $command
}

push_image() {
    local tag=${1:-$IMAGE_NAME:$VERSION}
    local remote=${2:-$REGISTRY}
    
    log_info "Pushing image: $tag"
    docker tag $tag $remote/$tag
    docker push $remote/$tag
    log_success "Image pushed: $remote/$tag"
}

clean_images() {
    log_info "Cleaning Docker images..."
    
    docker images $IMAGE_NAME --format "table {{.Repository}}:{{.Tag}}" | grep -v REPOSITORY | while read line; do
        if [ -n "$line" ]; then
            docker rmi $line 2>/dev/null || true
            log_info "Removed: $line"
        fi
    done
    
    log_success "Docker cleanup complete"
}

show_info() {
    log_info "ZSVO Docker Information:"
    echo "  Image: $IMAGE_NAME"
    echo "  Version: $VERSION"
    echo "  Registry: $REGISTRY"
    echo ""
    echo "Available images:"
    docker images $IMAGE_NAME 2>/dev/null | head -10 || echo "  No images found"
}

# Main script
case "${1:-help}" in
    "build")
        build_image "$2"
        ;;
    "run")
        run_container "$2" "$3"
        ;;
    "push")
        push_image "$2" "$3"
        ;;
    "clean")
        clean_images
        ;;
    "info")
        show_info
        ;;
    "help"|*)
        echo "ZSVO Docker Build Script"
        echo ""
        echo "Usage: $0 [command] [options]"
        echo ""
        echo "Commands:"
        echo "  build [tag]               Build Docker image"
        echo "  run [tag] [command]        Run container"
        echo "  push [tag] [registry]      Push image to registry"
        echo "  clean                     Clean Docker images"
        echo "  info                      Show Docker information"
        echo "  help                      Show this help"
        echo ""
        echo "Examples:"
        echo "  $0 build"
        echo "  $0 build zsvo:latest"
        echo "  $0 run zsvo:latest --help"
        echo "  $0 push zsvo:latest my-registry.com"
        echo ""
        echo "Environment Variables:"
        echo "  VERSION                   Override version"
        echo "  REGISTRY                  Docker registry"
        exit 0
        ;;
esac
