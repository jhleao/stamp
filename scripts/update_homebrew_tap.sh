#!/usr/bin/env bash
set -euo pipefail

version="${1#v}"
if [[ -z "$version" ]]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi
if [[ -z "${HOMEBREW_TAP_DEPLOY_KEY:-}" ]]; then
  echo "missing HOMEBREW_TAP_DEPLOY_KEY" >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checksums="$repo_root/dist/checksums.txt"
temporary="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/stamp-homebrew.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT

key="$temporary/deploy-key"
printf '%s\n' "$HOMEBREW_TAP_DEPLOY_KEY" > "$key"
chmod 600 "$key"
export GIT_SSH_COMMAND="ssh -i $key -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"
git clone git@github.com:jhleao/homebrew-tap.git "$temporary/tap"

checksum() {
  local archive="$1"
  local value
  value="$(awk -v file="$archive" '$2 == file || $2 == "./" file { print $1 }' "$checksums")"
  if [[ -z "$value" ]]; then
    echo "missing checksum for $archive" >&2
    exit 1
  fi
  printf '%s' "$value"
}

darwin_arm64="stamp_${version}_darwin_arm64.tar.gz"
darwin_amd64="stamp_${version}_darwin_amd64.tar.gz"
linux_arm64="stamp_${version}_linux_arm64.tar.gz"
linux_amd64="stamp_${version}_linux_amd64.tar.gz"

mkdir -p "$temporary/tap/Formula"
cat > "$temporary/tap/Formula/stamp.rb" <<RUBY
class Stamp < Formula
  desc "Turn Markdown, TSX components, and Tailwind themes into polished PDFs"
  homepage "https://github.com/jhleao/stamp"
  depends_on "pandoc"
  depends_on "tailwindcss"
  on_macos do
    on_arm do
      url "https://github.com/jhleao/stamp/releases/download/v$version/$darwin_arm64"
      sha256 "$(checksum "$darwin_arm64")"
    end
    on_intel do
      url "https://github.com/jhleao/stamp/releases/download/v$version/$darwin_amd64"
      sha256 "$(checksum "$darwin_amd64")"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/jhleao/stamp/releases/download/v$version/$linux_arm64"
      sha256 "$(checksum "$linux_arm64")"
    end
    on_intel do
      url "https://github.com/jhleao/stamp/releases/download/v$version/$linux_amd64"
      sha256 "$(checksum "$linux_amd64")"
    end
  end

  def install
    bin.install "stamp"
  end

  def caveats
    <<~EOS
      Run stamp setup to install the remaining macOS authoring tools,
      connect Google Drive, and open your first project.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/stamp version")
  end
end
RUBY

cd "$temporary/tap"
if [[ -z "$(git status --porcelain -- Formula/stamp.rb)" ]]; then
  echo "Homebrew formula is already current."
  exit 0
fi
git config user.name "Stamp Release"
git config user.email "actions@github.com"
git add Formula/stamp.rb
git commit -m "stamp $version"
git push origin HEAD:main
