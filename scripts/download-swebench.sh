#!/usr/bin/env bash
# Download SWE-bench dataset from Hugging Face
# Usage: ./download-swebench.sh [SWE-bench|SWE-bench_Verified|SWE-bench_Lite] [output_dir]

set -euo pipefail

DATASET="${1:-SWE-bench_Verified}"
OUT_DIR="${2:-./data/swebench}"

echo "Downloading $DATASET to $OUT_DIR..."

mkdir -p "$OUT_DIR"

# Use huggingface-cli if available, otherwise use wget/curl
if command -v huggingface-cli &> /dev/null; then
    echo "Using huggingface-cli..."
    hf download "SWE-bench/$DATASET" --repo-type dataset --local-dir "$OUT_DIR"
else
    echo "huggingface-cli not found. Install with: pip install huggingface_hub"
    echo "Or download manually from: https://huggingface.co/datasets/SWE-bench/$DATASET"
    exit 1
fi

# Verify the split files exist
for split in test dev train; do
    if [[ -f "$OUT_DIR/$split.jsonl" ]]; then
        count=$(wc -l < "$OUT_DIR/$split.jsonl")
        echo "  $split.jsonl: $count instances"
    fi
done

echo "Done! Use with: nebula swebench --local-path $OUT_DIR"