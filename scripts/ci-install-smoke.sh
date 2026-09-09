#!/usr/bin/env bash
set -euo pipefail

# Run from the repository root. --draft uses authenticated, prefetched assets;
# --public exercises real curl/wget against public GitHub release endpoints.
mode="${1:---public}"
case "$mode:$#" in
  --public:0|--public:1) ;;
  --draft:3) ;;
  *) echo 'usage: ci-install-smoke.sh [--public | --draft assets-directory tag]' >&2; exit 1 ;;
esac
repo_root="$PWD"
workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT
binary=cmdshape
goos="$(uname -s)"
goarch="$(uname -m)"
case "$goos" in Darwin) goos=darwin ;; Linux) goos=linux ;; MINGW*|MSYS*|CYGWIN*) goos=windows; binary=cmdshape.exe ;; *) exit 1 ;; esac
case "$goarch" in x86_64|amd64) goarch=amd64 ;; aarch64|arm64) goarch=arm64 ;; *) exit 1 ;; esac

# Isolate downloader availability as well as profiles, without changing the
# runner's installed tools. Wrappers invoke the actual installed executables.
prepare_tools() {
  local tools_dir="$1" client_mode="$2" tool tool_path
  mkdir -p "$tools_dir"
  for tool in awk bash cat chmod cp cygpath dirname grep mkdir mktemp mv rm sed sh sleep tr uname unzip wc sha256sum shasum curl wget; do
    [[ "$client_mode:$tool" == curl:wget || "$client_mode:$tool" == wget:curl ]] && continue
    if tool_path="$(command -v "$tool")"; then
      printf '#!/bin/sh\nexec %q "$@"\n' "$tool_path" > "$tools_dir/$tool"
      chmod +x "$tools_dir/$tool"
    fi
  done
  if [[ "$client_mode" == fallback || "$client_mode" == draft ]]; then
    cp scripts/testdata/installer-downloader.sh "$tools_dir/curl"
    chmod +x "$tools_dir/curl"
  fi
  if [[ "$client_mode" == draft ]]; then
    cp scripts/testdata/installer-downloader.sh "$tools_dir/wget"
    chmod +x "$tools_dir/wget"
  fi
}

install_case() (
  local label="$1" requested="$2" expected="$3" client_mode="$4"
  local case_dir="$workspace/$label" installed
  mkdir -p "$case_dir/home" "$case_dir/install"
  prepare_tools "$case_dir/tools" "$client_mode"
  echo "[installer-smoke] $label ($goos/$goarch): requested=$requested expected=$expected"
  env -u GH_TOKEN -u GITHUB_TOKEN \
    HOME="$case_dir/home" USERPROFILE="$case_dir/home" SHELL=/bin/sh \
    PATH="$case_dir/tools" VERSION="$requested" CMDSHAPE_INSTALL_DIR="$case_dir/install" \
    sh "$repo_root/scripts/install.sh"
  installed="$case_dir/install/$binary"
  test -f "$installed"
  env HOME="$case_dir/home" USERPROFILE="$case_dir/home" \
    bash "$repo_root/scripts/ci-smoke.sh" "$installed" "$expected"
  echo "[installer-smoke] $label passed"
)

if [[ "$mode" == --draft ]]; then
  assets_dir="$(cd "$2" && pwd -P)"
  tag="$3"
  [[ "$tag" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo 'invalid draft tag' >&2; exit 1; }
  export CMDSHAPE_SMOKE_MODE=draft CMDSHAPE_SMOKE_ASSETS="$assets_dir" CMDSHAPE_SMOKE_TAG="$tag"
  export CMDSHAPE_SMOKE_ASSET="cmdshape_${tag}_${goos}_${goarch}.zip"
  install_case draft "$tag" "$tag" draft
else
  command -v curl >/dev/null || { echo 'installer smoke requires real curl' >&2; exit 1; }
  command -v wget >/dev/null || { echo 'installer smoke requires real GNU wget' >&2; exit 1; }
  # An independent public lookup supplies the expected version for the latest
  # case. No installed-binary output is used as its own expected value.
  release_url="$(curl --disable --proto '=https' --proto-redir '=https' --tlsv1.2 \
    -sSfIL --max-redirs 10 --connect-timeout 15 --max-time 120 -o /dev/null \
    -w '%{url_effective}' https://github.com/SuppieRK/cmdshape/releases/latest)"
  case "$release_url" in
    https://github.com/SuppieRK/cmdshape/releases/tag/*) latest="${release_url##*/}" ;;
    *) echo 'unexpected latest release URL' >&2; exit 1 ;;
  esac
  install_case latest-curl latest "$latest" curl
  install_case pinned-wget 0.9.4 0.9.4 wget
  export CMDSHAPE_SMOKE_MODE=reject-curl
  install_case curl-403 0.9.4 0.9.4 fallback
fi
