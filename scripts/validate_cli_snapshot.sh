#!/usr/bin/env bash
set -euo pipefail

root_dir="${1:-$PWD}"
dist_dir="$root_dir/dist"

if [[ ! -d "$dist_dir" ]]; then
	echo "dist directory not found: $dist_dir" >&2
	exit 1
fi

required_archives=(
	"coyote_*_darwin_amd64.tar.gz"
	"coyote_*_darwin_arm64.tar.gz"
	"coyote_*_linux_amd64.tar.gz"
	"coyote_*_linux_arm64.tar.gz"
	"coyote_*_windows_amd64.zip"
)

required_build_dirs=(
	"coyote_darwin_amd64_v1"
	"coyote_darwin_arm64_v8.0"
	"coyote_linux_amd64_v1"
	"coyote_linux_arm64_v8.0"
	"coyote_windows_amd64_v1"
)

for pattern in "${required_archives[@]}"; do
	matches=("$dist_dir"/$pattern)
	if [[ ! -e "${matches[0]}" ]]; then
		echo "missing expected snapshot artifact pattern: $pattern" >&2
		exit 1
	fi
done

for dir_name in "${required_build_dirs[@]}"; do
	if [[ ! -d "$dist_dir/$dir_name" ]]; then
		echo "missing expected build target directory: $dir_name" >&2
		exit 1
	fi
done

if [[ ! -f "$dist_dir/checksums.txt" ]]; then
	echo "missing checksums.txt" >&2
	exit 1
fi

windows_archive=("$dist_dir"/coyote_*_windows_amd64.zip)
if ! unzip -l "${windows_archive[0]}" | grep -q 'coyote.exe$'; then
	echo "windows amd64 archive does not contain coyote.exe" >&2
	exit 1
fi

for path in "$dist_dir"/coyote_*; do
	name="$(basename "$path")"
	case "$name" in
		coyote_*_darwin_amd64.tar.gz|coyote_*_darwin_arm64.tar.gz|coyote_*_linux_amd64.tar.gz|coyote_*_linux_arm64.tar.gz|coyote_*_windows_amd64.zip|coyote_*_windows_arm64.zip|checksums.txt)
			;;
		coyote_darwin_amd64_v1|coyote_darwin_arm64_v8.0|coyote_linux_amd64_v1|coyote_linux_arm64_v8.0|coyote_windows_amd64_v1|coyote_windows_arm64_v8.0)
			;;
		metadata.json|artifacts.json|config.yaml)
			;;
		*)
			echo "unexpected snapshot artifact name: $name" >&2
			exit 1
			;;
		esac
	done

echo "CLI snapshot artifacts validated"