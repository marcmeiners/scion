#!/usr/bin/env bash
set -euo pipefail

# Measure SCION process CPU/memory usage repeatedly and write CSV output.
# Defaults:
#   - interval: 1s
#   - duration: 60s
#
# Usage:
#   tools/measure_cpu_mem.sh [duration_sec] [interval_sec] [out_dir] [pgrep_pattern]
#
# Example:
#   tools/measure_cpu_mem.sh
#   tools/measure_cpu_mem.sh 30 1 ./measurements 'control|daemon|router|dispatcher|sciond'

LC_ALL=C

DURATION="${1:-60}"
INTERVAL="${2:-1}"
OUT_DIR="${3:-measurements}"
PATTERN="${4:-control|daemon|router|dispatcher|sciond}"

if ! [[ "$DURATION" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
    echo "error: duration must be numeric (seconds)" >&2
    exit 1
fi
if ! [[ "$INTERVAL" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
    echo "error: interval must be numeric (seconds)" >&2
    exit 1
fi
if awk -v i="$INTERVAL" 'BEGIN{exit !(i <= 0)}'; then
    echo "error: interval must be > 0" >&2
    exit 1
fi
if awk -v d="$DURATION" 'BEGIN{exit !(d <= 0)}'; then
    echo "error: duration must be > 0" >&2
    exit 1
fi

if ! command -v pgrep >/dev/null 2>&1; then
    echo "error: pgrep is required" >&2
    exit 1
fi
if ! command -v ps >/dev/null 2>&1; then
    echo "error: ps is required" >&2
    exit 1
fi

SAMPLES="$(awk -v d="$DURATION" -v i="$INTERVAL" 'BEGIN { printf "%d", (d / i) + 0.5 }')"
if (( SAMPLES < 1 )); then
    SAMPLES=1
fi

mkdir -p "$OUT_DIR"
STAMP="$(date +%Y%m%d_%H%M%S)"
SUMMARY_CSV="${OUT_DIR}/cpu_mem_summary_${STAMP}.csv"
DETAIL_CSV="${OUT_DIR}/cpu_mem_detail_${STAMP}.csv"

echo "sample,timestamp,process_count,total_cpu_percent,total_rss_kib,total_rss_mib" > "$SUMMARY_CSV"
echo "sample,timestamp,pid,comm,cpu_percent,rss_kib" > "$DETAIL_CSV"

echo "Measuring for ${DURATION}s every ${INTERVAL}s (${SAMPLES} samples)"
echo "Pattern: ${PATTERN}"
echo "Summary CSV: ${SUMMARY_CSV}"
echo "Detail CSV:  ${DETAIL_CSV}"

for ((s = 1; s <= SAMPLES; s++)); do
    TS="$(date +%Y-%m-%dT%H:%M:%S.%3N%z)"
    PIDS="$(pgrep -f "$PATTERN" 2>/dev/null | paste -sd, - || true)"

    if [[ -z "$PIDS" ]]; then
        echo "${s},${TS},0,0,0,0.000" >> "$SUMMARY_CSV"
    else
        PS_OUT="$(ps -p "$PIDS" -o pid=,comm=,%cpu=,rss= --no-headers || true)"
        if [[ -z "$PS_OUT" ]]; then
            echo "${s},${TS},0,0,0,0.000" >> "$SUMMARY_CSV"
        else
            while read -r PID COMM CPU RSS; do
                [[ -z "${PID:-}" ]] && continue
                echo "${s},${TS},${PID},${COMM},${CPU},${RSS}" >> "$DETAIL_CSV"
            done <<< "$PS_OUT"

            read -r COUNT SUM_CPU SUM_RSS < <(
                awk '
                    NF >= 4 {
                        c += 1
                        cpu += $3
                        rss += $4
                    }
                    END {
                        printf "%d %.3f %.0f\n", c, cpu, rss
                    }
                ' <<< "$PS_OUT"
            )
            RSS_MIB="$(awk -v rss="$SUM_RSS" 'BEGIN { printf "%.3f", rss / 1024.0 }')"
            echo "${s},${TS},${COUNT},${SUM_CPU},${SUM_RSS},${RSS_MIB}" >> "$SUMMARY_CSV"
        fi
    fi

    if (( s < SAMPLES )); then
        sleep "$INTERVAL"
    fi
done

echo
echo "Done. Quick summary:"
awk -F, '
    NR == 1 { next }
    {
        n += 1
        avg_cpu += $4
        avg_mib += $6
        if ($4 > max_cpu) max_cpu = $4
        if ($6 > max_mib) max_mib = $6
    }
    END {
        if (n == 0) {
            print "No samples."
        } else {
            printf "  samples: %d\n", n
            printf "  avg total CPU%%: %.3f\n", avg_cpu / n
            printf "  peak total CPU%%: %.3f\n", max_cpu
            printf "  avg total RSS MiB: %.3f\n", avg_mib / n
            printf "  peak total RSS MiB: %.3f\n", max_mib
        }
    }
' "$SUMMARY_CSV"
