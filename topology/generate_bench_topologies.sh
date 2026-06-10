#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
Usage: $0 <num_children> [name_prefix]

Generates two topology files in topology/:
  1) <prefix>_public_<N>.topo
     - root public core (ISD 1)
     - N second-level public cores, each in its own public ISD (outside 4096..65535)

  2) <prefix>_private_<N>.topo
     - root public core (ISD 1)
     - N second-level public children (ISD 1), each core of its own private ISD
       in range 4096..65535

Constraints:
  - 1 <= N <= 61440  (private ISD range 4096..65535 has 61440 values)
USAGE
}

if [[ $# -lt 1 || $# -gt 2 ]]; then
  usage
  exit 1
fi

N="$1"
PREFIX="${2:-bench_tree}"

if ! [[ "$N" =~ ^[0-9]+$ ]]; then
  echo "error: num_children must be a positive integer" >&2
  exit 1
fi
if (( N < 1 )); then
  echo "error: num_children must be >= 1" >&2
  exit 1
fi
if (( N > 61440 )); then
  echo "error: num_children must be <= 61440 (private ISD range 4096..65535)" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PUBLIC_FILE="${SCRIPT_DIR}/${PREFIX}_public_${N}.topo"
PRIVATE_FILE="${SCRIPT_DIR}/${PREFIX}_private_${N}.topo"

ROOT_IA="1-ff00:0:100"

# -----------------------------
# Public version
# -----------------------------
cat > "$PUBLIC_FILE" <<EOF_PUBLIC
--- # Auto-generated benchmark topology (public)
ASes:
  "$ROOT_IA":
    core: true
    voting: true
    authoritative: true
    issuing: true
EOF_PUBLIC

for ((i=1; i<=N; i++)); do
  child_isd=$((100 + i))         # keep public ISDs outside private range 4096..65535
  child_as_suffix=$((200 + i))   # globally unique AS suffixes
  cat >> "$PUBLIC_FILE" <<EOF_PUBLIC_AS
  "${child_isd}-ff00:0:${child_as_suffix}":
    core: true
    voting: true
    authoritative: true
    issuing: true
EOF_PUBLIC_AS
done

cat >> "$PUBLIC_FILE" <<'EOF_PUBLIC_LINKS'
links:
EOF_PUBLIC_LINKS

for ((i=1; i<=N; i++)); do
  child_isd=$((100 + i))
  child_as_suffix=$((200 + i))
  cat >> "$PUBLIC_FILE" <<EOF_PUBLIC_LINK
  - {a: "1-ff00:0:100#${i}", b: "${child_isd}-ff00:0:${child_as_suffix}#1", linkAtoB: CORE}
EOF_PUBLIC_LINK
done

# -----------------------------
# Private version
# -----------------------------
cat > "$PRIVATE_FILE" <<EOF_PRIVATE
--- # Auto-generated benchmark topology (private)
ASes:
  "$ROOT_IA":
    core: true
    voting: true
    authoritative: true
    issuing: true
EOF_PRIVATE

for ((i=1; i<=N; i++)); do
  child_as_suffix=$((200 + i))
  private_isd=$((4095 + i))      # private range 4096..65535
  cat >> "$PRIVATE_FILE" <<EOF_PRIVATE_AS
  "1-ff00:0:${child_as_suffix}":
    cert_issuer: $ROOT_IA
    private_isds:
      - isd: ${private_isd}
        core: true
        voting: true
        authoritative: true
        issuing: true
        cert_issuer: ${private_isd}-ff00:0:${child_as_suffix}
EOF_PRIVATE_AS
done

cat >> "$PRIVATE_FILE" <<'EOF_PRIVATE_LINKS'
links:
EOF_PRIVATE_LINKS

for ((i=1; i<=N; i++)); do
  child_as_suffix=$((200 + i))
  cat >> "$PRIVATE_FILE" <<EOF_PRIVATE_LINK
  - {a: "1-ff00:0:100#${i}", b: "1-ff00:0:${child_as_suffix}#1", linkAtoB: CHILD}
EOF_PRIVATE_LINK
done

printf 'generated: %s\n' "$PUBLIC_FILE"
printf 'generated: %s\n' "$PRIVATE_FILE"
printf 'children: %d\n' "$N"
printf 'private ISD range used: 4096..%d\n' $((4095 + N))
