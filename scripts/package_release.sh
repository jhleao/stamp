#!/usr/bin/env bash
set -euo pipefail

version="${1#v}"
if [[ -z "$version" ]]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_dir="$repo_root/dist"
rm -rf "$release_dir"
mkdir -p "$release_dir"

targets=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
  "windows amd64"
)

for target in "${targets[@]}"; do
  read -r os arch <<< "$target"
  archive="stamp_${version}_${os}_${arch}"
  stage="$release_dir/$archive"
  mkdir -p "$stage"

  binary="stamp"
  [[ "$os" == "windows" ]] && binary="stamp.exe"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
    -trimpath \
    -ldflags "-s -w -X main.version=$version" \
    -o "$stage/$binary" \
    "$repo_root/cmd/stamp"
  cp "$repo_root/README.md" "$stage/README.md"

  if [[ "$os" == "windows" ]]; then
    (cd "$release_dir" && zip -qr "$archive.zip" "$archive")
  else
    tar -C "$release_dir" -czf "$release_dir/$archive.tar.gz" "$archive"
  fi
  rm -rf "$stage"
done

(
  cd "$release_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./*.tar.gz ./*.zip > checksums.txt
  else
    shasum -a 256 ./*.tar.gz ./*.zip > checksums.txt
  fi
)
