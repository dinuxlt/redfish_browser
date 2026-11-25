#!/usr/bin/env bash
# Builds ./redfish-browser (and ./mockbmc).
# Works around a missing libxxf86vm-dev package by linking against the runtime library.
set -euo pipefail
cd "$(dirname "$0")"

libdirs="/usr/lib/x86_64-linux-gnu /usr/lib/aarch64-linux-gnu /usr/lib64 /usr/lib"
have_dev=""; lib=""
for d in $libdirs; do
	[ -e "$d/libXxf86vm.so" ] && have_dev=1
	[ -z "$lib" ] && [ -e "$d/libXxf86vm.so.1" ] && lib="$d/libXxf86vm.so.1"
done
if [ -z "$have_dev" ]; then
	if [ -n "$lib" ]; then
		mkdir -p .buildlibs
		ln -sf "$lib" .buildlibs/libXxf86vm.so
		export CGO_LDFLAGS="${CGO_LDFLAGS:-} -L$PWD/.buildlibs"
	fi
fi

go build -trimpath -ldflags="-s -w" -o redfish-browser .
go build -trimpath -ldflags="-s -w" -o mockbmc ./cmd/mockbmc
echo "Built ./redfish-browser and ./mockbmc"
