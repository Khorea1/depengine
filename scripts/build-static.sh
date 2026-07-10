#!/bin/sh

# Ensure the version is injected correctly
if [ -z "$VERSION" ]; then
  echo "Error: VERSION environment variable is not set."
  exit 1
fi

# Build static binary for multiple platforms
# Using go build with ldflags to set version
# Output binaries will be placed in the 'dist' directory

# Ensure dist directory exists
mkdir -p dist

# Define platforms to build for
# linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
GOOS_LIST="linux darwin"
GOARCH_LIST="amd64 arm64"

for goos in $GOOS_LIST; do
  for goarch in $GOARCH_LIST; do
    echo "Building for $goos/$goarch..."
    FILENAME="depengine-${VERSION#v}-go${goos}-amd64"
    if [ "$goarch" = "arm64" ]; then
      FILENAME="depengine-${VERSION#v}-go${goos}-arm64"
    fi

    CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -ldflags "-s -w -X main.version=$VERSION" -o dist/$FILENAME . "$@"
    if [ $? -ne 0 ]; then
      echo "Build failed for $goos/$goarch"
      exit 1
    fi
  done
done

echo "Static binaries built successfully in dist/"
