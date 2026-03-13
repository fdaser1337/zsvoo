# Dockerfile for ZSVO Package Manager
# Linux-based build with build tools

FROM ubuntu:22.04

# Set non-interactive frontend for package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install build tools and runtime dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    meson \
    ninja-build \
    pkg-config \
    wget \
    curl \
    git \
    tar \
    gzip \
    xz-utils \
    zstd \
    python3 \
    python3-pip \
    lua5.1 \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# Install Go
RUN wget -O /tmp/go.tar.gz https://golang.org/dl/go1.23.0.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz

# Set Go environment
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/root/go"
ENV GOBIN="/root/go/bin"
ENV CGO_ENABLED=0

# Create app directory
WORKDIR /app

# Copy source code
COPY . .

# Build ZSVO for Linux
RUN go build -o zsvo .

# Create necessary directories for ZSVO
RUN mkdir -p /tmp/pkg-work \
    && mkdir -p /var/lib/pkgdb \
    && mkdir -p /var/cache/packages

# Make it executable
RUN chmod +x /app/zsvo

# Set default command
CMD ["/app/zsvo", "--help"]
