#!/bin/sh
# CI-only transport fixture. The production installer has no fixture override.
set -eu
output=""
headers=""
url=""
client="${0##*/}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o|-O|--output|--output-document) output="$2"; shift 2 ;;
    --dump-header) headers="$2"; shift 2 ;;
    https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
[ "$output" != - ] || output=/dev/stdout

if [ "${CMDSHAPE_SMOKE_MODE:-}" = reject-curl ]; then
  printf 'HTTP/2 403\r\n\r\n' > "$headers"
  echo 'curl: (22) simulated HTTP 403' >&2
  exit 22
fi

prefix="https://github.com/SuppieRK/cmdshape/releases/download/${CMDSHAPE_SMOKE_TAG}/"
case "$url" in
  "$prefix"*) asset="${url#"$prefix"}" ;;
  *) echo "unexpected fixture URL: $url" >&2; exit 99 ;;
esac
case "$asset" in
  cmdshape_checksums.txt|"${CMDSHAPE_SMOKE_ASSET}") ;;
  *) echo "unexpected fixture asset: $asset" >&2; exit 99 ;;
esac
if [ "$client" = curl ]; then
  printf 'HTTP/2 200\r\n\r\n' > "$headers"
else
  printf '  HTTP/1.1 200 OK\r\n\r\n' >&2
fi
cp "$CMDSHAPE_SMOKE_ASSETS/$asset" "$output"
