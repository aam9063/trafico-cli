#!/bin/sh
# install.sh — one-line installer for trafico-cli.
#
# Resolves the release to install (TRAFICO_VERSION, or the latest published
# release), downloads the GoReleaser asset that matches this platform, verifies
# its SHA-256 against the release's checksums.txt BEFORE unpacking anything,
# and installs the binary into ${TRAFICO_INSTALL_DIR:-$HOME/.local/bin}.
#
# POSIX sh. Only curl or wget, tar, sha256sum/shasum and mktemp are required,
# plus ordinary POSIX tools (sed, awk, head, grep).
#
# Comments are in English; user-facing messages are in Spanish, like the CLI.
# Tests source this file with TRAFICO_INSTALLER_SKIP_MAIN=1 and stub the
# network-facing functions (download, fetch_latest_version) and uname; see
# scripts/test-install.sh. No test in this repository touches the network.

set -eu

REPO="aam9063/trafico-cli"
BASE_URL="https://github.com/${REPO}/releases"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"

die() {
  # die "mensaje" [exit_code]
  echo "install.sh: $1" >&2
  exit "${2:-1}"
}

info() {
  echo "==> $1"
}

# detect_os maps the running system to the GoReleaser GOOS value.
# Windows is deliberately rejected with a plain message: this POSIX installer
# does not handle the .zip assets that GoReleaser publishes for windows.
detect_os() {
  case "$(uname -s)" in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    MINGW* | MSYS* | CYGWIN* | Windows_NT)
      die "Windows no está soportado por este instalador. Descarga el .zip de tu arquitectura desde ${BASE_URL}/latest y coloca trafico.exe en tu PATH." 1
      ;;
    *)
      die "Sistema operativo no soportado: $(uname -s). Descarga el archivo adecuado desde ${BASE_URL}/latest." 1
      ;;
  esac
}

# detect_arch maps the machine to the GoReleaser GOARCH value.
detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *)
      die "Arquitectura no soportada: $(uname -m). Descarga el archivo adecuado desde ${BASE_URL}/latest." 1
      ;;
  esac
}

# archive_for prints the release asset name for a version, GOOS and GOARCH.
# The version may carry the leading "v" of the git tag; the asset name does not.
archive_for() {
  version="$1"
  os="$2"
  arch="$3"
  case "$os" in
    linux | darwin) ext=tar.gz ;;
    *)
      die "Extensión de archivo desconocida para $os." 1
      ;;
  esac
  echo "trafico_${version#v}_${os}_${arch}.${ext}"
}

# download fetches url into dest with whichever of curl/wget exists.
download() {
  url="$1"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    die "Necesitas curl o wget para descargar la versión." 1
  fi
}

# fetch_latest_version prints the tag (for example v0.1.0) of the latest
# published release, parsed from the GitHub API without jq. A download
# directory is passed in so no extra temp file leaks outside the caller's
# cleanup.
fetch_latest_version() {
  dest_dir="$1"
  json="$dest_dir/latest.json"
  download "$API_URL" "$json" ||
    die "No se pudo consultar la última versión en GitHub. Puedes fijar una con TRAFICO_VERSION (por ejemplo: TRAFICO_VERSION=v0.1.0)." 1
  tag=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$json" | head -n 1)
  [ -n "$tag" ] ||
    die "No se pudo determinar la última versión publicada. Puedes fijar una con TRAFICO_VERSION (por ejemplo: TRAFICO_VERSION=v0.1.0)." 1
  echo "$tag"
}

# sha256_of prints the lowercase hex SHA-256 of a file.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "Necesitas sha256sum o shasum para verificar la suma de comprobación." 1
  fi
}

# verify_checksum compares the SHA-256 of the downloaded archive against the
# entry for that exact asset in the release's checksums.txt. It prints its own
# Spanish error and returns non-zero on any mismatch, so callers abort before
# unpacking: a failed checksum installs nothing.
verify_checksum() {
  checksums="$1"
  asset="$2"
  archive="$3"
  expected=$(awk -v f="$asset" '$2 == f { print $1; exit }' "$checksums")
  if [ -z "$expected" ]; then
    echo "install.sh: checksums.txt no contiene ninguna entrada para $asset; abortando." >&2
    return 1
  fi
  actual=$(sha256_of "$archive")
  if [ "$actual" != "$expected" ]; then
    echo "install.sh: la suma SHA-256 no coincide para $asset." >&2
    echo "  esperado: $expected" >&2
    echo "  obtenido: $actual" >&2
    echo "  No se ha instalado nada. Reintenta más tarde o descarga el archivo a mano desde ${BASE_URL}/latest." >&2
    return 1
  fi
  return 0
}

# path_hint prints the exact PATH line to add when install_dir is not already
# on PATH, and prints nothing when it is.
path_hint() {
  dir="$1"
  case ":$PATH:" in
    *":$dir:"*) return 0 ;;
  esac
  echo ""
  echo "$dir no está en tu PATH. Añádelo a tu shell:"
  echo "  export PATH=\"$dir:\$PATH\""
}

usage() {
  cat <<EOF
Uso: curl -fsSL https://raw.githubusercontent.com/${REPO}/dev/install.sh | sh

Variables de entorno:
  TRAFICO_VERSION       versión a instalar (por ejemplo v0.1.0); por defecto, la última.
  TRAFICO_INSTALL_DIR   directorio de instalación; por defecto \$HOME/.local/bin.
EOF
}

main() {
  for arg in "$@"; do
    case "$arg" in
      -h | --help)
        usage
        return 0
        ;;
      *)
        die "Argumento desconocido: $arg. Sin argumentos instala la última versión; usa --help para más información." 2
        ;;
    esac
  done

  os=$(detect_os)
  arch=$(detect_arch)
  install_dir="${TRAFICO_INSTALL_DIR:-$HOME/.local/bin}"

  tmpdir=$(mktemp -d) || die "No se pudo crear un directorio temporal." 1
  # Every exit path — success, failed download, failed checksum, failed
  # unpack — removes the temporary directory.
  trap 'rm -rf "$tmpdir"' EXIT INT TERM HUP

  if [ -n "${TRAFICO_VERSION:-}" ]; then
    version=$TRAFICO_VERSION
    info "Instalando la versión $version para $os/$arch"
  else
    version=$(fetch_latest_version "$tmpdir")
    info "Instalando la última versión ($version) para $os/$arch"
  fi

  asset=$(archive_for "$version" "$os" "$arch")
  base="$BASE_URL/download/$version"

  info "Descargando $asset"
  download "$base/$asset" "$tmpdir/$asset" ||
    die "No se pudo descargar $asset. ¿Existe la versión $version?" 1
  info "Descargando checksums.txt"
  download "$base/checksums.txt" "$tmpdir/checksums.txt" ||
    die "No se pudo descargar checksums.txt." 1

  info "Verificando la suma SHA-256"
  verify_checksum "$tmpdir/checksums.txt" "$asset" "$tmpdir/$asset" || exit 1
  info "Suma verificada"

  mkdir -p "$tmpdir/extract"
  tar -xzf "$tmpdir/$asset" -C "$tmpdir/extract" ||
    die "No se pudo desempaquetar $asset." 1

  binary="$tmpdir/extract/trafico"
  [ -f "$binary" ] ||
    die "El archivo $asset no contiene el binario trafico; abortando." 1

  mkdir -p "$install_dir"
  if [ -e "$install_dir/trafico" ]; then
    info "Ya existía una instalación en $install_dir/trafico; reemplazándola."
  fi
  # Copy-then-rename so a re-run over a running binary is safe.
  cp "$binary" "$install_dir/trafico.tmp.$$" &&
    mv "$install_dir/trafico.tmp.$$" "$install_dir/trafico"
  chmod 0755 "$install_dir/trafico"

  info "Instalado $version en $install_dir/trafico"
  info "Ejecuta 'trafico version' para comprobarlo."
  path_hint "$install_dir"
}

# Tests source this file and drive the functions directly with stubs; see
# scripts/test-install.sh.
if [ "${TRAFICO_INSTALLER_SKIP_MAIN:-0}" != "1" ]; then
  main "$@"
fi
