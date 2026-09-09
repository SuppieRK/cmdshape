#!/usr/bin/env sh
set -eu

# Installs cmdshape from GitHub Releases.
#
# Usage:
#   Download this script completely, then run: sh install.sh
#   VERSION=0.1.0 sh install.sh
#   See README.md for curl and wget bootstrap commands.
#
# Env:
#   VERSION               Release tag in X.Y.Z form (default: latest)
#   CMDSHAPE_INSTALL_DIR  Explicit destination directory (default: automatic selection)
# Installer behavior:
#   - Repository is fixed to SuppieRK/cmdshape.
#   - Install directory is selected automatically:
#       1) /usr/local/bin (if writable)
#       2) $HOME/.local/bin
#       3) ./bin

REPO="SuppieRK/cmdshape"
VERSION="${VERSION:-latest}"
CMDSHAPE_REQUESTED_INSTALL_DIR="${CMDSHAPE_INSTALL_DIR:-}"
REQUESTED_INSTALL_DIR="$CMDSHAPE_REQUESTED_INSTALL_DIR"
BIN_NAME="cmdshape"
PROFILE_NOTE="# added by cmdshape installer"
MAX_RELEASE_BYTES=1048576
MAX_CHECKSUM_BYTES=1048576
MAX_ARCHIVE_BYTES=134217728
MAX_BINARY_BYTES=67108864
# Each client handles one response at a time. In particular, wget's
# --https-only does not prevent HTTP redirects for non-recursive downloads.
download_with() (
  client="$1"
  canonical="$2"
  destination="$3"
  limit="$4"
  stage="$5"
  mode="$6"
  request_url="$canonical"
  redirects=0
  retries=0
  headers="$TMP_DIR/response-headers"
  diagnostic="$TMP_DIR/response-error"

  while :; do
    case "$request_url" in
      https://*) ;;
      *) echo "$stage: refused non-HTTPS redirect from $canonical" >&2; exit 65 ;;
    esac
    : > "$destination"
    : > "$headers"
    : > "$diagnostic"
    status=0
    # POSIX sh specifies 512-byte blocks. This also bounds wget responses
    # without Content-Length; the byte check below is authoritative.
    if (
      ulimit -f "$(( (limit + 511) / 512 ))" || exit 65
      if [ "$client" = curl ]; then
        curl --disable --proto '=https' --proto-redir '=https' --tlsv1.2 \
          --connect-timeout 15 --max-time 120 --max-filesize "$limit" \
          -sSf --dump-header "$headers" -o "$destination" "$request_url"
      else
        wget --no-config --secure-protocol=TLSv1_2 --max-redirect=0 \
          --timeout=30 --tries=1 --server-response --no-verbose \
          -O "$destination" "$request_url" 2> "$headers"
      fi
    ) 2> "$diagnostic"; then
      status=0
    else
      status=$?
    fi
    size="$(wc -c < "$destination" | tr -d ' ')"
    if [ "$size" -gt "$limit" ] || [ "$status" -eq 63 ] || [ "$status" -ge 128 ]; then
      echo "$stage: download exceeds ${limit} bytes: $canonical" >&2
      exit 65
    fi
    [ "$status" -ne 65 ] || exit 65
    http_status="$(awk '$1 ~ /^HTTP\// {code=$2; sub(/\r$/, "", code)} END {print code}' "$headers")"
    case "$http_status" in
      301|302|303|307|308)
        redirect="$(awk '
          $1 ~ /^HTTP\// {location=""; count=0}
          tolower($1) == "location:" {sub(/\r$/, ""); sub(/^[ \t]*[^:]+:[ \t]*/, ""); location=$0; count++}
          END {if (count == 1) print location}
        ' "$headers")"
        redirects=$((redirects + 1))
        if [ "$redirects" -gt 10 ] || [ -z "$redirect" ]; then
          echo "$stage: invalid or excessive redirects from $canonical" >&2
          exit 65
        fi
        case "$redirect" in
          https://*) ;;
          /*) case "$redirect" in //*) echo "$stage: unsafe redirect from $canonical" >&2; exit 65 ;; esac
              authority="${request_url#https://}"; authority="${authority%%/*}"
              redirect="https://$authority$redirect" ;;
          *) echo "$stage: refused non-HTTPS or ambiguous redirect from $canonical" >&2; exit 65 ;;
        esac
        if [ "$mode" = resolve ]; then
          case "$redirect" in
            "https://github.com/$REPO/releases/tag/"*)
              validate_release_version "${redirect#"https://github.com/$REPO/releases/tag/"}" || {
                echo "$stage: release version must be exact semantic version (X.Y.Z)" >&2; exit 65;
              }
              exit 0 ;;
            *) echo "$stage: unexpected release redirect from $canonical" >&2; exit 65 ;;
          esac
        fi
        request_url="$redirect"
        continue
        ;;
      200)
        if [ "$status" -eq 0 ] && [ "$mode" = file ]; then return 0; fi
        ;;
    esac
    echo "$stage: $client failed (HTTP ${http_status:-unknown}, exit $status): $canonical" >&2
    cat "$diagnostic" >&2
    if [ "$client" = wget ]; then
      # Suppress signed redirect URLs in wget's progress output.
      sed '/https\?:\/\//d' "$headers" >&2
    fi
    transient=false
    case "$http_status" in
      408|429|500|502|503|504) transient=true ;;
      ''|200)
        case "$client:$status" in
          curl:5|curl:6|curl:7|curl:18|curl:28|curl:52|curl:55|curl:56|wget:4) transient=true ;;
        esac ;;
    esac
    if [ "$transient" = true ] && [ "$retries" -lt 2 ]; then
      retries=$((retries + 1))
      sleep "$retries"
      request_url="$canonical"
      redirects=0
      continue
    fi
    exit 1
  done
)

download_bounded() {
  for download_client in curl wget; do
    command -v "$download_client" >/dev/null 2>&1 || continue
    if download_with "$download_client" "$1" "$2" "$3" "$4" "${5:-file}"; then
      return 0
    else
      download_status=$?
    fi
    [ "$download_status" -ne 65 ] || exit 1
    echo "$4: trying another available downloader" >&2
  done
  echo "$4: download failed; curl or wget must be installed and able to reach $1" >&2
  exit 1
}

sha256_file() {
  file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return 0
  fi
  echo "missing required command: sha256sum or shasum" >&2
  exit 1
}

verify_download_checksum() {
  checksums_file="$1"
  asset_name="$2"
  asset_path="$3"
  expected=""
	matches=0

  while IFS= read -r line || [ -n "$line" ]; do
    [ -n "$line" ] || continue
    hash="$(printf '%s' "$line" | awk '{print $1}')"
		name="$(printf '%s' "$line" | awk '{print $2}')"
		name="${name#\*}"
    name="${name#./}"
    if [ "$name" = "$asset_name" ]; then
			field_count="$(printf '%s' "$line" | awk '{print NF}')"
			if [ "$field_count" -ne 2 ]; then
				echo "malformed checksum entry for asset: $asset_name" >&2
				exit 1
			fi
			if [ "${#hash}" -ne 64 ]; then
				echo "checksum for asset must contain exactly 64 hex digits: $asset_name" >&2
				exit 1
			fi
			case "$hash" in
				*[!0-9a-fA-F]*) echo "checksum for asset is not hexadecimal: $asset_name" >&2; exit 1 ;;
			esac
			matches=$((matches + 1))
			if [ "$matches" -gt 1 ]; then
				echo "duplicate checksum for asset: $asset_name" >&2
				exit 1
			fi
      expected="$hash"
    fi
  done < "$checksums_file"

  if [ -z "$expected" ]; then
    echo "checksum for asset not found: $asset_name" >&2
    exit 1
  fi

  actual="$(sha256_file "$asset_path")"
	normalized_expected="$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')"
	if [ "$actual" != "$normalized_expected" ]; then
    echo "checksum mismatch for $asset_name" >&2
    exit 1
  fi
}

inspect_archive() {
  archive="$1"
  expected="$2"
  count=0
  entries="$(unzip -Z1 "$archive")" || {
    echo "failed to inspect archive entries" >&2
    exit 1
  }
  while IFS= read -r entry; do
    [ -n "$entry" ] || continue
    case "$entry" in
      /*|*\\*|../*|*/../*|*/..)
        echo "unsafe archive entry: $entry" >&2
        exit 1
        ;;
    esac
    if [ "$entry" != "$expected" ]; then
      echo "unexpected archive entry: $entry" >&2
      exit 1
    fi
    count=$((count + 1))
  done <<EOF
$entries
EOF
  if [ "$count" -ne 1 ]; then
    echo "archive must contain exactly one $expected binary" >&2
    exit 1
  fi
  entry_type="$(unzip -Z -l "$archive" "$expected" | awk '$1 ~ /^[-dl]/ {print substr($1,1,1); exit}')"
  if [ "$entry_type" != "-" ]; then
    echo "archive entry is not a regular binary: $expected" >&2
    exit 1
  fi
  entry_size="$(unzip -Z -l "$archive" "$expected" | awk '$1 ~ /^-/ {print $4; exit}')"
  case "$entry_size" in
    ""|*[!0-9]*) echo "failed to inspect archive binary size" >&2; exit 1 ;;
  esac
  if [ "${#entry_size}" -gt "${#MAX_BINARY_BYTES}" ] || \
    { [ "${#entry_size}" -eq "${#MAX_BINARY_BYTES}" ] && [ "$entry_size" -gt "$MAX_BINARY_BYTES" ]; }; then
    echo "archive binary exceeds ${MAX_BINARY_BYTES} bytes: $expected" >&2
    exit 1
  fi
}

validate_staged_binary() {
  candidate="$1"
  expected="$2"
	version_output="$TMP_DIR/staged-version.txt"
  "$candidate" --version > "$version_output" 2>/dev/null || {
    echo "staged binary failed --version" >&2
    exit 1
  }
	actual_with_marker="$(cat "$version_output"; printf x)"
	actual="${actual_with_marker%x}"
	line_feed='
'
	if [ "$actual" != "$expected$line_feed" ]; then
    echo "staged binary version mismatch: expected $expected, got $actual" >&2
    exit 1
  fi
}

need_cmd() {
  cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || {
    echo "missing required command: $cmd" >&2
    exit 1
  }
  return 0
}

need_cmd uname
need_cmd unzip
if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
  echo "missing required command: curl or wget" >&2
  exit 1
fi

parse_release_version() {
	parsed_version="$1"
	[ "$(printf '%s' "$parsed_version" | tr -d '\r\n')" = "$parsed_version" ] || return 1
	parsed_remainder="${parsed_version#*.}"
  [ "$parsed_remainder" != "$parsed_version" ] || return 1
  parsed_major="${parsed_version%%.*}"

  parsed_patch="${parsed_remainder#*.}"
  [ "$parsed_patch" != "$parsed_remainder" ] || return 1
  parsed_minor="${parsed_remainder%%.*}"

	for component in "$parsed_major" "$parsed_minor" "$parsed_patch"; do
		case "$component" in
			""|*[!0-9]*) return 1 ;;
			0|[1-9]|[1-9][0-9]*) ;;
			*) return 1 ;;
		esac
		component_length="${#component}"
		[ "$component_length" -lt 20 ] && continue
		[ "$component_length" -eq 20 ] || return 1
		awk -v value="$component" 'BEGIN { exit !("x" value <= "x18446744073709551615") }' || return 1
	done
	return 0
}

validate_release_version() {
  parse_release_version "$1" || return 1
  printf '%s' "$parsed_version"
}

validate_install_dir_path() {
  install_dir="$1"
  case "$install_dir" in
    /*) ;;
    *) echo "install directory must resolve to an absolute path" >&2; exit 1 ;;
  esac
  if [ "$(printf '%s' "$install_dir" | tr -d '\r\n')" != "$install_dir" ]; then
    echo "install directory must not contain CR or LF" >&2
    exit 1
  fi
  case "$install_dir" in
    *:*) echo "install directory cannot be represented safely in PATH" >&2; exit 1 ;;
  esac
}

validate_install_dir() {
  install_dir="$1"
  validate_install_dir_path "$install_dir"
  [ -d "$install_dir" ] && [ -w "$install_dir" ] || {
    echo "install directory is not writable: $install_dir" >&2
    exit 1
  }
}

normalize_requested_install_dir() {
	requested="$1"
	if [ "$(printf '%s' "$requested" | tr -d '\r\n')" != "$requested" ]; then
		echo "CMDSHAPE_INSTALL_DIR must not contain CR or LF" >&2
		exit 1
	fi
	case "$OS:$requested" in
		windows:[A-Za-z]:[\\/]*)
			command -v cygpath >/dev/null 2>&1 || {
				echo "cygpath is required for a native Windows CMDSHAPE_INSTALL_DIR" >&2
				exit 1
			}
			cygpath -u -- "$requested"
			;;
		*) printf '%s\n' "$requested" ;;
	esac
}

choose_install_dir() {
	if [ -n "$REQUESTED_INSTALL_DIR" ]; then
		normalized_install_dir="$(normalize_requested_install_dir "$REQUESTED_INSTALL_DIR")"
		case "$normalized_install_dir" in
			/*) ;;
			*) echo "CMDSHAPE_INSTALL_DIR must be an absolute path" >&2; exit 1 ;;
		esac
		validate_install_dir_path "$normalized_install_dir"
		mkdir -p "$normalized_install_dir" 2>/dev/null || {
			echo "install directory is not writable: $normalized_install_dir" >&2
			exit 1
		}
		[ -w "$normalized_install_dir" ] || {
			echo "install directory is not writable: $normalized_install_dir" >&2
			exit 1
		}
		resolved_install_dir="$(cd "$normalized_install_dir" && pwd -P)"
		validate_install_dir "$resolved_install_dir"
		printf '%s\n' "$resolved_install_dir"
		return 0
  fi
  if [ -w "/usr/local/bin" ]; then
	(cd "/usr/local/bin" && pwd -P)
    return 0
  fi
  if [ -d "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
	resolved_install_dir="$(cd "$HOME/.local/bin" && pwd -P)"
	validate_install_dir "$resolved_install_dir"
	printf '%s\n' "$resolved_install_dir"
    return 0
  fi
  if [ -d "./bin" ] || mkdir -p "./bin" 2>/dev/null; then
    # Keep fallback deterministic and local to current directory.
	(cd "./bin" && pwd -P)
    return 0
  fi
  echo "failed to determine writable install directory" >&2
  exit 1
}

path_starts_with_dir() {
  dir="$1"
	case "$PATH" in
		"$dir"|"$dir":*) return 0 ;;
    *) return 1 ;;
  esac
}

append_path_export_once() {
	conf_file="$1"
	install_dir="$2"
	quoted_install_dir="'$(printf '%s' "$install_dir" | sed "s/'/'\\\\''/g")'"
	path_entry="export PATH=${quoted_install_dir}:\"\$PATH\""
	if [ -f "$conf_file" ] && grep -Fqx -e "$path_entry" "$conf_file"; then
		echo "PATH already configured in $conf_file"
    return 0
  fi
  if [ ! -f "$conf_file" ]; then
    : > "$conf_file"
  fi
	{
		echo ""
		echo "$PROFILE_NOTE"
		printf '%s\n' "$path_entry"
  } >> "$conf_file"
  echo "Added $install_dir to $conf_file"
  return 0
}

update_path_if_needed() {
  install_dir="$1"
  shell_name=""
  conf_file=""
	if path_starts_with_dir "$install_dir"; then
    return 0
  fi

  shell_name="${SHELL:-}"
  case "$shell_name" in
    */zsh)
      append_path_export_once "$HOME/.zshrc" "$install_dir"
      echo "Run: source \"$HOME/.zshrc\""
      ;;
    */bash)
      if [ "$(uname -s)" = "Darwin" ]; then
        conf_file="$HOME/.bash_profile"
      else
        conf_file="$HOME/.bashrc"
      fi
      append_path_export_once "$conf_file" "$install_dir"
      echo "Run: source \"$conf_file\""
      ;;
    */fish)
		if command -v fish >/dev/null 2>&1; then
			fish -c 'fish_add_path --move --prepend -- $argv[1]' "$install_dir" >/dev/null 2>&1 || true
        echo "Added $install_dir to fish PATH configuration"
      fi
      ;;
    *)
      append_path_export_once "$HOME/.profile" "$install_dir"
      echo "Run: source \"$HOME/.profile\""
      ;;
  esac
  return 0
}

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "unsupported arch: $ARCH" >&2
    exit 1
    ;;
esac

case "$OS" in
  linux) OS="linux" ;;
  darwin) OS="darwin" ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *)
    echo "unsupported os: $OS" >&2
    exit 1
    ;;
esac

INSTALL_DIR="$(choose_install_dir)"
validate_install_dir "$INSTALL_DIR"
STAGED_DST="$(mktemp "$INSTALL_DIR/.${BIN_NAME}.new.XXXXXX")" || {
	echo "install directory is not writable: $INSTALL_DIR" >&2
	exit 1
}
TMP_DIR="$(mktemp -d)"
trap 'if [ -n "$STAGED_DST" ]; then rm -f "$STAGED_DST"; fi; if [ -n "$TMP_DIR" ]; then rm -rf "$TMP_DIR"; fi' EXIT INT TERM

if [ "$VERSION" = "latest" ]; then
  LATEST_URL="https://github.com/$REPO/releases/latest"
  VERSION="$(download_bounded "$LATEST_URL" "$TMP_DIR/latest" "$MAX_RELEASE_BYTES" 'version lookup' resolve)"
fi

RESOLVED_VERSION="$(validate_release_version "$VERSION")" || {
  echo "release version must be exact semantic version (X.Y.Z): ${VERSION}" >&2
  exit 1
}
VERSION="$RESOLVED_VERSION"

ASSET="${BIN_NAME}_${VERSION}_${OS}_${ARCH}.zip"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
CHECKSUMS_ASSET="cmdshape_checksums.txt"
CHECKSUMS_URL="https://github.com/$REPO/releases/download/$VERSION/$CHECKSUMS_ASSET"

echo "Downloading $URL"
download_bounded "$URL" "$TMP_DIR/$ASSET" "$MAX_ARCHIVE_BYTES" archive
download_bounded "$CHECKSUMS_URL" "$TMP_DIR/$CHECKSUMS_ASSET" "$MAX_CHECKSUM_BYTES" checksums
verify_download_checksum "$TMP_DIR/$CHECKSUMS_ASSET" "$ASSET" "$TMP_DIR/$ASSET"

if [ "$OS" = "windows" ]; then
	ARCHIVE_BINARY="${BIN_NAME}.exe"
	DST="$INSTALL_DIR/${BIN_NAME}.exe"
else
	ARCHIVE_BINARY="$BIN_NAME"
	DST="$INSTALL_DIR/$BIN_NAME"
fi
inspect_archive "$TMP_DIR/$ASSET" "$ARCHIVE_BINARY"
unzip -p "$TMP_DIR/$ASSET" "$ARCHIVE_BINARY" > "$STAGED_DST"
staged_size="$(wc -c < "$STAGED_DST" | tr -d ' ')"
if [ "$staged_size" -gt "$MAX_BINARY_BYTES" ]; then
	echo "staged binary exceeds ${MAX_BINARY_BYTES} bytes" >&2
	exit 1
fi

if [ "$OS" != "windows" ]; then
	chmod 0755 "$STAGED_DST"
fi
validate_staged_binary "$STAGED_DST" "$VERSION"
mv -f "$STAGED_DST" "$DST"
STAGED_DST=""
update_path_if_needed "$INSTALL_DIR"
echo "Installed binary $BIN_NAME $VERSION to $DST"
