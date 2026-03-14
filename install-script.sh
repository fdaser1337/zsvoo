#!/bin/bash
# Simple install script for auto-generated packages
set -e

PKGDIR="$1"

echo "Installing to $PKGDIR"

# Try CMake
if [ -f build/cmake_install.cmake ]; then
    echo "Using CMake install"
    DESTDIR="$PKGDIR" cmake --install build
    exit 0
fi

# Try Meson  
if [ -f build/meson-private/coredata.dat ]; then
    echo "Using Meson install"
    DESTDIR="$PKGDIR" meson install -C build ${ZSVO_MESON_INSTALL_ARGS}
    exit 0
fi

# Try Makefile
if [ -f Makefile ] || [ -f makefile ] || [ -f GNUmakefile ]; then
    echo "Using Makefile install"
    make DESTDIR="$PKGDIR" PREFIX=/usr install ${ZSVO_MAKE_INSTALL_FLAGS}
    exit 0
fi

# Fallback - create minimal structure
echo "Warning: No standard build system found, installing common directories"
mkdir -p "$PKGDIR/usr/bin" "$PKGDIR/usr/lib" "$PKGDIR/usr/include"
