#!/usr/bin/env bash
set -euo pipefail

version="${1#v}"
if [[ -z "$version" ]]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi

required_google_variables=(
  GOOGLE_OAUTH_CLIENT_ID
  GOOGLE_OAUTH_CLIENT_SECRET
  GOOGLE_PICKER_API_KEY
)
for variable in "${required_google_variables[@]}"; do
  if [[ -z "${!variable:-}" ]]; then
    echo "missing $variable; refusing to publish a release without Google credentials" >&2
    exit 1
  fi
done

google_ldflags=(
  "-X github.com/jhleao/stamp/internal/drive.DefaultOAuthClientID=$GOOGLE_OAUTH_CLIENT_ID"
  "-X github.com/jhleao/stamp/internal/drive.DefaultOAuthClientSecret=$GOOGLE_OAUTH_CLIENT_SECRET"
  "-X github.com/jhleao/stamp/internal/drive.DefaultPickerAPIKey=$GOOGLE_PICKER_API_KEY"
)

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
    -ldflags "-s -w -X main.version=$version ${google_ldflags[*]}" \
    -o "$stage/$binary" \
    "$repo_root/cmd/stamp"
  cp "$repo_root/README.md" "$stage/README.md"
  cp "$repo_root/CHANGELOG.md" "$stage/CHANGELOG.md"

  if [[ "$os" == "darwin" ]]; then
    required_apple_variables=(
      APPLE_KEYCHAIN_PATH
      APPLE_NOTARY_ISSUER_ID
      APPLE_NOTARY_KEY_ID
      APPLE_NOTARY_KEY_PATH
      APPLE_SIGNING_IDENTITY
    )
    for variable in "${required_apple_variables[@]}"; do
      if [[ -z "${!variable:-}" ]]; then
        echo "missing $variable; refusing to publish an unsigned macOS binary" >&2
        exit 1
      fi
    done

    codesign \
      --force \
      --options runtime \
      --timestamp \
      --sign "$APPLE_SIGNING_IDENTITY" \
      --keychain "$APPLE_KEYCHAIN_PATH" \
      "$stage/$binary"
    codesign --verify --strict --verbose=2 "$stage/$binary"

    submission="$release_dir/$archive.notarization.zip"
    ditto -c -k --keepParent "$stage" "$submission"
    xcrun notarytool submit "$submission" \
      --key "$APPLE_NOTARY_KEY_PATH" \
      --key-id "$APPLE_NOTARY_KEY_ID" \
      --issuer "$APPLE_NOTARY_ISSUER_ID" \
      --wait \
      --timeout 30m
    rm -f "$submission"
  fi

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
