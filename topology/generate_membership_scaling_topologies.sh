#!/usr/bin/env bash
# Generates shared-CS membership-scaling topologies in steps of 5 memberships.
#
# Benchmark target:
#   - one root core AS in public ISD 1
#   - N child ASes in public ISD 1
#   - root additionally holds core membership in M private ISDs
#   - each child additionally holds non-core membership in the same M private ISDs
#   - shared-CS only: this topology models one shared control service instance per AS
#
# Defaults:
#   - N = 19, for a total of 20 ASes including the root
#   - private ISD range = 4096..65535
#
# The script generates one topology file per measurement point:
#   M = 0, 5, 10, 15, ... up to the requested maximum membership count.
# M=0 is the public-only baseline.
#
# Usage:
#   ./topology/generate_membership_scaling_topologies.sh <max_memberships> [num_children] [name_prefix]
#
# Examples:
#   ./topology/generate_membership_scaling_topologies.sh 0
#   ./topology/generate_membership_scaling_topologies.sh 35
#   ./topology/generate_membership_scaling_topologies.sh 100 19 bench_membership
#
# Output:
#   topology/<prefix>_m<M>.topo for each M in 0,5,10,...,max_memberships
#
# Suggested run loop:
#   ./topology/generate_membership_scaling_topologies.sh 100
#   for m in $(seq 0 5 100); do
#     ./scion.sh topology -c "topology/bench_membership_m${m}.topo"
#     ./scion.sh run
#     tools/measure_cpu_mem.sh 60 1 "measurements/bench_membership_m${m}/"
#     ./scion.sh stop
#   done

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: generate_membership_scaling_topologies.sh <max_memberships> [num_children] [name_prefix]

Generate shared-CS membership-scaling topologies for:
  M = 0, 5, 10, 15, ... up to max_memberships

Each topology has:
  - root AS: 1-ff00:0:100
  - child ASes: 1-ff00:0:201 .. 1-ff00:0:(200+N)
  - root is core in all generated private ISDs
  - children are non-core members in the same private ISDs

Arguments:
  max_memberships  Maximum number of private ISDs to add. Must be a multiple of 5.
  num_children     Number of children N. Default: 19
  name_prefix      Output file prefix. Default: bench_membership

Constraints:
  - 0 <= max_memberships <= 61440
  - max_memberships % 5 == 0
  - 1 <= N
USAGE
}

if [[ $# -lt 1 || $# -gt 3 ]]; then
  usage
  exit 1
fi

MAX_M="$1"
N="${2:-19}"
PREFIX="${3:-bench_membership}"

if ! [[ "$MAX_M" =~ ^[0-9]+$ ]]; then
  echo "error: max_memberships must be a non-negative integer" >&2
  exit 1
fi
if ! [[ "$N" =~ ^[0-9]+$ ]]; then
  echo "error: num_children must be a positive integer" >&2
  exit 1
fi
if (( MAX_M > 61440 )); then
  echo "error: max_memberships must be <= 61440 (private ISD range 4096..65535)" >&2
  exit 1
fi
if (( MAX_M % 5 != 0 )); then
  echo "error: max_memberships must be a multiple of 5" >&2
  exit 1
fi
if (( N < 1 )); then
  echo "error: num_children must be >= 1" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_IA="1-ff00:0:100"
PRIVATE_ISD_MIN=4096

generate_topology() {
  local M="$1"
  local FILE="${SCRIPT_DIR}/${PREFIX}_m${M}.topo"

  cat > "$FILE" <<EOF
--- # Auto-generated shared-CS membership-scaling topology
# Root AS: ${ROOT_IA}
# Child AS count: ${N}
# Private membership count: ${M}
ASes:
  "${ROOT_IA}":
    core: true
    voting: true
    authoritative: true
    issuing: true
EOF

  if (( M > 0 )); then
    printf '    private_isds:\n' >> "$FILE"
    for ((i=0; i<M; i++)); do
      private_isd=$((PRIVATE_ISD_MIN + i))
      cat >> "$FILE" <<EOF
      - isd: ${private_isd}
        core: true
        voting: true
        authoritative: true
        issuing: true
        cert_issuer: ${private_isd}-ff00:0:100
EOF
    done
  fi

  for ((i=1; i<=N; i++)); do
    child_as_suffix=$((200 + i))
    cat >> "$FILE" <<EOF
  "1-ff00:0:${child_as_suffix}":
    cert_issuer: ${ROOT_IA}
EOF
    if (( M > 0 )); then
      printf '    private_isds:\n' >> "$FILE"
      for ((j=0; j<M; j++)); do
        private_isd=$((PRIVATE_ISD_MIN + j))
        cat >> "$FILE" <<EOF
      - isd: ${private_isd}
        core: false
        voting: false
        authoritative: false
        issuing: false
        cert_issuer: ${private_isd}-ff00:0:100
EOF
      done
    fi
  done

  printf 'links:\n' >> "$FILE"
  for ((i=1; i<=N; i++)); do
    child_as_suffix=$((200 + i))
    printf '  - {a: "1-ff00:0:100#%d", b: "1-ff00:0:%d#1", linkAtoB: CHILD}\n' \
      "${i}" "${child_as_suffix}" >> "$FILE"
  done

  printf 'generated: %s\n' "$FILE"
}

for ((m=0; m<=MAX_M; m+=5)); do
  generate_topology "$m"
done

printf 'mode: shared-cs\n'
printf 'children: %d\n' "$N"
printf 'membership steps: 0, 5, 10, ... , %d\n' "$MAX_M"
if (( MAX_M == 0 )); then
  printf 'private ISD range used: none (public-only baseline only)\n'
else
  printf 'max private ISD range used: %d..%d\n' \
    "${PRIVATE_ISD_MIN}" "$((PRIVATE_ISD_MIN + MAX_M - 1))"
fi
