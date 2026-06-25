#!/usr/bin/env bash
# Generates AS-scaling topologies in steps of 5 child ASes.
#
# Topology:
#   - one root core AS in public ISD 1 (1-ff00:0:100)
#   - N child ASes in public ISD 1 (1-ff00:0:201 .. 1-ff00:0:(200+N))
#   - no private ISDs — purely public, single-ISD
#   - shared-CS only
#
# The script generates one topology file per measurement point:
#   N = 0, 5, 10, ... up to the requested maximum child count.
# N=0 is the root-only baseline.
#
# Usage:
#   ./topology/generate_as_scaling_topologies.sh <max_children> [name_prefix]
#
# Examples:
#   ./topology/generate_as_scaling_topologies.sh 0
#   ./topology/generate_as_scaling_topologies.sh 50
#   ./topology/generate_as_scaling_topologies.sh 100 bench_as
#
# Output:
#   topology/<prefix>_n<N>.topo for each N in 0,5,10,...,max_children
#
# Suggested run loop:
#   ./topology/generate_as_scaling_topologies.sh 100
#   for n in $(seq 0 5 100); do
#     ./scion.sh topology -c "topology/bench_as_n${n}.topo"
#     ./scion.sh run
#     tools/measure_cpu_mem.sh 60 1 "measurements/bench_as_n${n}/"
#     ./scion.sh stop
#   done

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: generate_as_scaling_topologies.sh <max_children> [name_prefix]

Generate AS-scaling topologies for:
  N = 0, 5, 10, 15, ... up to max_children

Each topology has:
  - root AS: 1-ff00:0:100 (core, voting, authoritative, issuing)
  - child ASes: 1-ff00:0:201 .. 1-ff00:0:(200+N)
  - all ASes in ISD 1, no private ISDs

Arguments:
  max_children  Maximum number of child ASes. Must be a multiple of 5.
  name_prefix   Output file prefix. Default: bench_as

Constraints:
  - max_children >= 0
  - max_children % 5 == 0
USAGE
}

if [[ $# -lt 1 || $# -gt 2 ]]; then
  usage
  exit 1
fi

MAX_N="$1"
PREFIX="${2:-bench_as}"

if ! [[ "$MAX_N" =~ ^[0-9]+$ ]]; then
  echo "error: max_children must be a non-negative integer" >&2
  exit 1
fi
if (( MAX_N % 5 != 0 )); then
  echo "error: max_children must be a multiple of 5" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_IA="1-ff00:0:100"

generate_topology() {
  local N="$1"
  local FILE="${SCRIPT_DIR}/${PREFIX}_n${N}.topo"

  cat > "$FILE" <<EOF
--- # Auto-generated AS-scaling topology
# Root AS: ${ROOT_IA}
# Child AS count: ${N}
ASes:
  "${ROOT_IA}":
    core: true
    voting: true
    authoritative: true
    issuing: true
EOF

  for ((i=1; i<=N; i++)); do
    child_as_suffix=$((200 + i))
    printf '  "1-ff00:0:%d":\n' "${child_as_suffix}" >> "$FILE"
    printf '    cert_issuer: %s\n' "${ROOT_IA}" >> "$FILE"
  done

  printf 'links:\n' >> "$FILE"
  for ((i=1; i<=N; i++)); do
    child_as_suffix=$((200 + i))
    printf '  - {a: "%s#%d", b: "1-ff00:0:%d#1", linkAtoB: CHILD}\n' \
      "${ROOT_IA}" "${i}" "${child_as_suffix}" >> "$FILE"
  done

  printf 'generated: %s\n' "$FILE"
}

for ((n=0; n<=MAX_N; n+=5)); do
  generate_topology "$n"
done

printf 'mode: shared-cs\n'
printf 'isd: 1 (public only)\n'
printf 'AS steps: 0, 5, 10, ... , %d\n' "$MAX_N"
