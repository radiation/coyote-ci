#!/usr/bin/env bash
set -euo pipefail

root_dir="${1:-$PWD}"
dist_dir="$root_dir/dist"

if [[ ! -d "$dist_dir" ]]; then
	echo "dist directory not found: $dist_dir" >&2
	exit 1
fi

required_archives=(
	"coyote_darwin_amd64.tar.gz"
	"coyote_darwin_arm64.tar.gz"
	"coyote_linux_amd64.tar.gz"
	"coyote_linux_arm64.tar.gz"
	"coyote_windows_amd64.zip"
	"coyote_windows_arm64.zip"
)

required_build_dirs=(
	"coyote_darwin_amd64_v1"
	"coyote_darwin_arm64_v8.0"
	"coyote_linux_amd64_v1"
	"coyote_linux_arm64_v8.0"
	"coyote_windows_amd64_v1"
	"coyote_windows_arm64_v8.0"
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

for architecture in amd64 arm64; do
	if ! unzip -l "$dist_dir/coyote_windows_$architecture.zip" | grep -q 'coyote.exe$'; then
		echo "windows $architecture archive does not contain coyote.exe" >&2
		exit 1
	fi
done

(cd "$dist_dir" && shasum -a 256 -c checksums.txt)

for path in "$dist_dir"/coyote_*; do
	name="$(basename "$path")"
	case "$name" in
				coyote_darwin_amd64.tar.gz|coyote_darwin_arm64.tar.gz|coyote_linux_amd64.tar.gz|coyote_linux_arm64.tar.gz|coyote_windows_amd64.zip|coyote_windows_arm64.zip|checksums.txt)
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