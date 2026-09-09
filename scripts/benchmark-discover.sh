#!/usr/bin/env bash
set -euo pipefail

mode=""
event_name=""
base_sha=""
head_sha=""
output_file=""
summary_file=""
declare -a changed_override=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      mode="${2:-}"
      shift 2
      ;;
    --event-name)
      event_name="${2:-}"
      shift 2
      ;;
    --base-sha)
      base_sha="${2:-}"
      shift 2
      ;;
    --head-sha)
      head_sha="${2:-}"
      shift 2
      ;;
    --output-file)
      output_file="${2:-}"
      shift 2
      ;;
    --summary-file)
      summary_file="${2:-}"
      shift 2
      ;;
    --changed-file)
      changed_override+=("${2:-}")
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$mode" || -z "$output_file" ]]; then
  echo "usage: benchmark-discover.sh --mode <main|pr> --output-file <path> [--event-name <name>] [--base-sha <sha>] [--head-sha <sha>] [--summary-file <path>] [--changed-file <path>]" >&2
  exit 1
fi

all_tools=()
tool_known() {
  local expected="$1"
  local existing
  for existing in "${all_tools[@]-}"; do
    [[ "$existing" == "$expected" ]] && return 0
  done
  return 1
}

add_known_tool() {
  local tool="$1"
  [[ -z "$tool" ]] && return 0
  tool_known "$tool" || all_tools+=("$tool")
}

for tool_dir in testdata/benchmarks/*; do
  if [[ -d "$tool_dir" ]]; then
    add_known_tool "$(basename "$tool_dir")"
  fi
done
if [[ -n "$base_sha" ]] && git cat-file -e "${base_sha}^{commit}" 2>/dev/null; then
  while IFS= read -r path; do
    tool="${path#testdata/benchmarks/}"
    tool="${tool%%/*}"
    add_known_tool "$tool"
  done < <(git ls-tree -r --name-only "$base_sha" -- testdata/benchmarks)
fi
if [[ -z "${all_tools[*]-}" ]]; then
  echo "No benchmark tools found under benchmark roots" >&2
  exit 1
fi

tool_matrix_entry() {
  local tool="$1"
  jq -nc \
    --arg tool "$tool" \
    '{
      tool: $tool
    }'
  return 0
}

build_matrix_json() {
  local tool
  for tool in "$@"; do
    tool_matrix_entry "$tool"
  done | jq -s -c '.'
  return 0
}

if [[ "$mode" != "main" && "$mode" != "pr" ]]; then
  echo "unsupported mode: $mode" >&2
  exit 1
fi

selected_tools=()
run_all=0
changed=()
run_validate=false
run_install_smoke=false
run_benchmarks=false
run_fixture_verify=false
change_class="none"

selected_contains() {
  local expected="$1"
  local existing
  if [[ -z "${selected_tools[*]-}" ]]; then
    return 1
  fi
  for existing in "${selected_tools[@]-}"; do
    if [[ "$existing" == "$expected" ]]; then
      return 0
    fi
  done
  return 1
}

add_selected_tool() {
  local tool="$1"
  if ! selected_contains "$tool"; then
    selected_tools+=("$tool")
  fi
  return 0
}

add_prefix() {
  local prefix="$1"
  local t
  for t in "${all_tools[@]-}"; do
    if [[ "$t" == "$prefix" || "$t" == "$prefix"-* ]]; then
      add_selected_tool "$t"
    fi
  done
  return 0
}

add_exact_if_exists() {
  local exact="$1"
  local t
  for t in "${all_tools[@]-}"; do
    if [[ "$t" == "$exact" ]]; then
      add_selected_tool "$t"
      return
    fi
  done
}

load_changed_files() {
  if [[ -n "${changed_override[*]-}" ]]; then
    changed=("${changed_override[@]-}")
    return 0
  fi

  if [[ -n "$base_sha" && -n "$head_sha" ]]; then
    changed=()
    while IFS= read -r path; do
      [[ -z "$path" ]] && continue
      changed+=("$path")
    done < <(git diff --name-only "$base_sha" "$head_sha")
    return 0
  fi

  if [[ "$event_name" == "pull_request" ]]; then
    echo "pull_request mode requires --base-sha and --head-sha" >&2
    exit 1
  fi

  changed=()
  return 0
}

mark_full_ci() {
  run_validate=true
  run_all=1
  return 0
}

load_changed_files

if [[ -z "${changed[*]-}" ]]; then
  mark_full_ci
fi

changed_count=0
for path in "${changed[@]-}"; do
  [[ -z "$path" ]] && continue
  changed_count=$((changed_count + 1))
  case "$path" in
    testdata/benchmarks/*)
      tool="${path#testdata/benchmarks/}"
      tool="${tool%%/*}"
      add_selected_tool "$tool"
      ;;
    filters/.mappings.yaml)
	  run_validate=true
      run_all=1
      ;;
    filters/*)
	  run_validate=true
      tool="${path#filters/}"
      tool="${tool%%/*}"
      tool="${tool%%.*}"
      add_exact_if_exists "$tool"
      ;;
    openspec/specs/*/spec.md)
      cap="${path#openspec/specs/}"
      cap="${cap%%/*}"
      add_exact_if_exists "$cap"
      ;;
    cmd/*|internal/*|go.mod|go.sum)
      mark_full_ci
      ;;
    README.md|site/index.html)
      run_install_smoke=true
      ;;
    docs/*|site/*|assets/*|LICENSE|LICENSE.*|CODE_OF_CONDUCT.md|CONTRIBUTING.md|SECURITY.md)
      ;;
    *)
	  mark_full_ci
      ;;
  esac
done

if [[ "$run_all" -eq 1 ]]; then
  benchmark_matrix=$(build_matrix_json "${all_tools[@]-}")
  has_tools=true
elif [[ -n "${selected_tools[*]-}" ]]; then
  sorted_tools=()
  while IFS= read -r tool; do
    sorted_tools+=("$tool")
  done < <(printf "%s\n" "${selected_tools[@]-}" | sort)
  selected_tools=("${sorted_tools[@]}")
  benchmark_matrix=$(build_matrix_json "${selected_tools[@]-}")
  has_tools=true
else
  benchmark_matrix="[]"
  has_tools=false
fi

if [[ "$run_validate" == "true" ]]; then
  run_install_smoke=true
  change_class="full_ci"
elif [[ "$has_tools" == "true" ]]; then
  change_class="benchmark_only"
fi

if [[ "$has_tools" == "true" ]]; then
  run_benchmarks=true
  run_fixture_verify=true
fi

{
  echo "benchmark_matrix=${benchmark_matrix}"
  echo "has_tools=${has_tools}"
  echo "run_validate=${run_validate}"
  echo "run_install_smoke=${run_install_smoke}"
  echo "run_benchmarks=${run_benchmarks}"
	 echo "run_fixture_verify=${run_fixture_verify}"
  echo "change_class=${change_class}"
} >> "$output_file"

if [[ -n "$summary_file" ]]; then
  {
    echo "## Validation Plan"
    echo ""
    echo "- Changed files analyzed: ${changed_count}"
    echo "- Change class: \`${change_class}\`"
    echo "- Run validate: \`${run_validate}\`"
    echo "- Run installer smoke: \`${run_install_smoke}\`"
    echo "- Run benchmarks: \`${run_benchmarks}\`"
	 echo "- Run fixture verification: \`${run_fixture_verify}\`"
    echo "- Selected tools: \`$(jq -c 'map(.tool)' <<< "${benchmark_matrix}")\`"
  } >> "$summary_file"
fi
