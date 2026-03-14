#!/bin/bash

# Test script for ZSVO on Debian 13
set -e

echo "🚀 Building ZSVO for Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -o zsvo .

echo "🐧 Building test Docker image..."
docker build -f test-env.dockerfile -t zsvo-test .

echo "🧪 Running tests in container..."
docker run --rm -it -v $(pwd)/zsvo:/usr/local/bin/zsvo zsvo-test /bin/bash -c "
echo '📋 ZSVO version:'
zsvo --help | head -3

echo ''
echo '🔍 Testing help command:'
zsvo install --help

echo ''
echo '📦 Testing dry-run with htop:'
zsvo install --dry-run htop

echo ''
echo '🎯 Testing neovim installation (this will try to build cmake, git, lua5.1):'
zsvo install --dry-run neovim

echo ''
echo '✅ All tests completed!'
"

echo "🎉 Test completed!"
