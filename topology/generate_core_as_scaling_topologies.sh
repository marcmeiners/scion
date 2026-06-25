#!/usr/bin/env bash
# Generates all-core AS-scaling topologies in steps of 5 outer core ASes.
#
# Topology:
#   - one center core AS in public ISD 1 (1-ff00:0:100)
#   - N outer core ASes in public ISD 1 (1-ff00:0:201 .. 1-ff00:0:(200+N))
#   - all ASes are core, voting, authoritative, issuing
#   - star topology: center linked to each outer AS via CORE links
#   - no private ISDs — purely public, single-ISD
#   - shared-CS only
#
# The script generates one topology file per measurement point:
#   N = 0, 5, 10, ... up to the requested maximum outer AS count.
# N=0 is the single-core-AS baseline.
#
# Usage:
#   ./topology/generate_core_as_scaling_topologies.sh <max_outer> [name_prefix]
#
# Examples:
#   ./topology/generate_core_as_scaling_topologies.sh 0
#   ./topology/generate_core_as_scaling_topologies.sh 50
#   ./topology/generate_core_as_scaling_topologies.sh 100 bench_core_as
#
# Output:
#   topology/<prefix>_n<N>.topo for each N in 0,5,10,...,max_outer
#
# Suggested run loop:
#   ./topology/generate_core_as_scaling_topologies.sh 100
#   for n in $(seq 0 5 100); do
#     ./scion.sh topology -c "topology/bench_core_as_n${n}.topo"
#     ./scion.sh run
#     tools/measure_cpu_mem.sh 60 1 "measurements/bench_core_as_n${n}/"
#     ./scion.sh stop
#   done

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: generate_core_as_scaling_topologies.sh <max_outer> [name_prefix]

Generate all-core AS-scaling topologies for:
  N = 0, 5, 10, 15, ... up to max_outer

Each topology has:
  - center AS: 1-ff00:0:100 (core, voting, authoritative, issuing)
  - outer ASes: 1-ff00:0:201 .. 1-ff00:0:(200+N) (all core)
  - all in ISD 1, no private ISDs
  - links: CORE between center and each outer AS

Arguments:
  max_outer    Maximum number of outer core ASes. Must be a multiple of 5.
  name_prefix  Output file prefix. Default: bench_core_as

Constraints:
  - max_outer >= 0
  - max_outer % 5 == 0
USAGE
}

if [[ $# -lt 1 || $# -gt 2 ]]; then
  usage
  exit 1
fi

MAX_N="$1"
PREFIX="${2:-bench_core_as}"

if ! [[ "$MAX_N" =~ ^[0-9]+$ ]]; then
  echo "error: max_outer must be a non-negative integer" >&2
  exit 1
fi
if (( MAX_N % 5 != 0 )); then
  echo "error: max_outer must be a multiple of 5" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CENTER_IA="1-ff00:0:100"

generate_topology() {
  local N="$1"
  local FILE="${SCRIPT_DIR}/${PREFIX}_n${N}.topo"

  cat > "$FILE" <<EOF
--- # Auto-generated all-core AS-scaling topology
# Center AS: ${CENTER_IA}
# Outer core AS count: ${N}
ASes:
  "${CENTER_IA}":
    core: true
    voting: true
    authoritative: true
    issuing: true
EOF

  for ((i=1; i<=N; i++)); do
    outer_as_suffix=$((200 + i))
    cat >> "$FILE" <<EOF
  "1-ff00:0:${outer_as_suffix}":
    core: true
    voting: true
    authoritative: true
    issuing: true
EOF
  done

  printf 'links:\n' >> "$FILE"
  for ((i=1; i<=N; i++)); do
    outer_as_suffix=$((200 + i))
    printf '  - {a: "%s#%d", b: "1-ff00:0:%d#1", linkAtoB: CORE}\n' \
      "${CENTER_IA}" "${i}" "${outer_as_suffix}" >> "$FILE"
  done

  printf 'generated: %s\n' "$FILE"
}

for ((n=0; n<=MAX_N; n+=5)); do
  generate_topology "$n"
done

printf 'mode: shared-cs\n'
printf 'isd: 1 (public only, all-core)\n'
printf 'AS steps: 0, 5, 10, ... , %d\n' "$MAX_N"
