#!/usr/bin/env bash
# Build swag-apifox with the version derived from the current git tag.
#
#   ./build.sh                       -> ./swag-apifox[.exe]
#   ./build.sh bin/swag-apifox.exe   -> custom output path
#
# `git describe --tags --always --dirty` yields exactly the tag on a clean
# tagged commit (v1.0.1), or v1.0.1-2-gabcdef[-dirty] for later commits.
set -euo pipefail

cd "$(dirname "$0")"

pkg=github.com/Robert-Du0001/swag-apifox/cmd
version=$(git describe --tags --always --dirty 2>/dev/null || echo unknown)

out=${1:-}
if [ -z "$out" ]; then
	case "$(go env GOOS)" in
		windows) out=swag-apifox.exe ;;
		*)       out=swag-apifox ;;
	esac
fi

go build -trimpath -ldflags "-X $pkg.Version=$version" -o "$out" .
printf 'built %s  (%s)\n' "$out" "$version"
