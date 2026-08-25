#!/bin/sh
set -eu

repo="jhleao/stamp"
install_dir="${STAMP_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) echo "Stamp supports macOS and Linux." >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *) echo "Unsupported CPU architecture: $(uname -m)" >&2; exit 1 ;;
esac

latest_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")"
tag="${latest_url##*/}"
version="${tag#v}"
if [ -z "$version" ] || [ "$version" = "$latest_url" ]; then
  echo "Could not determine the latest Stamp release." >&2
  exit 1
fi

archive="stamp_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/stamp-install.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT INT TERM

echo "Downloading Stamp $version..."
curl -fsSL "$base/$archive" -o "$temporary/$archive"
curl -fsSL "$base/checksums.txt" -o "$temporary/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file || $2 == "./" file { print $1 }' "$temporary/checksums.txt")"
if [ -z "$expected" ]; then
  echo "The release checksum does not list $archive." >&2
  exit 1
fi
actual="$(shasum -a 256 "$temporary/$archive" | awk '{ print $1 }')"
if [ "$actual" != "$expected" ]; then
  echo "Stamp download failed checksum verification." >&2
  exit 1
fi

tar -xzf "$temporary/$archive" -C "$temporary"
binary="$temporary/stamp_${version}_${os}_${arch}/stamp"
if [ ! -x "$binary" ]; then
  echo "The Stamp release archive is malformed." >&2
  exit 1
fi

mkdir -p "$install_dir"
install -m 0755 "$binary" "$install_dir/stamp"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *)
    if [ "$install_dir" = "$HOME/.local/bin" ]; then
      shell_name="$(basename "${SHELL:-sh}")"
      profile="$HOME/.profile"
      [ "$shell_name" = "zsh" ] && profile="$HOME/.zprofile"
      path_line='export PATH="$HOME/.local/bin:$PATH"'
      if ! grep -Fqx "$path_line" "$profile" 2>/dev/null; then
        printf '\n%s\n' "$path_line" >> "$profile"
        echo "Added $install_dir to PATH in $profile."
      fi
    else
      echo "Add $install_dir to your PATH."
    fi
    ;;
esac

echo "Installed Stamp $version at $install_dir/stamp"
echo "Next: open a new terminal and run: stamp setup"
