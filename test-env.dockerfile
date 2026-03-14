# Test environment for ZSVO on Debian 13
FROM debian:13

# Install basic build tools
RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    git \
    make \
    pkg-config \
    wget \
    curl \
    golang-go \
    sudo \
    && rm -rf /var/lib/apt/lists/*

# Create zsvo user with sudo access
RUN useradd -m -s /bin/bash -G sudo zsvo
RUN echo "zsvo ALL=(ALL) NOPASSWD: ALL" >> /etc/sudoers

# Set working directory
WORKDIR /home/zsvo

# Copy zsvo binary and install script
COPY zsvo /usr/local/bin/zsvo
COPY install-script.sh /tmp/install-script.sh
RUN chmod +x /usr/local/bin/zsvo /tmp/install-script.sh

# Create necessary directories with proper permissions
RUN mkdir -p /tmp/pkg-work /home/zsvo/packages /var/lib/pkgdb && \
    chown -R zsvo:zsvo /tmp/pkg-work /home/zsvo/packages /var/lib/pkgdb

# Switch to zsvo user
USER zsvo

# Set environment
ENV PATH=/usr/local/bin:/usr/bin:/bin
ENV HOME=/home/zsvo

# Test command
CMD ["/bin/bash"]
