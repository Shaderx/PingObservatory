#!/bin/sh
set -eu
for platform in windows linux; do
    for architecture in amd64 arm64; do
        output="build/$platform-$architecture/PingObservatory"
        [ "$platform" != windows ] || output="$output.exe"
        mkdir -p "build/$platform-$architecture"
        GOOS="$platform" GOARCH="$architecture" CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o "$output" ./cmd/pingobservatory
    done
done
