#!/bin/bash
# run_eval.sh - Kicks off a SWE-bench evaluation run for Nebula

set -e

# Default parameters
DATASET="SWE-bench_Lite"
SPLIT="test"
MAX_INSTANCES=5
CONCURRENCY=1
OUTPUT_DIR="output/swebench_eval_$(date +%Y%m%d_%H%M%S)"
TIMEOUT="15m"

echo "==========================================================="
echo "🚀 Starting Nebula SWE-bench Evaluation"
echo "==========================================================="
echo "Dataset:     $DATASET"
echo "Split:       $SPLIT"
echo "Instances:   $MAX_INSTANCES (Max)"
echo "Concurrency: $CONCURRENCY"
echo "Output Dir:  $OUTPUT_DIR"
echo "Timeout:     $TIMEOUT per instance"
echo "==========================================================="
echo ""

# Ensure output directory exists
mkdir -p "$OUTPUT_DIR"

# Run the evaluation using the compiled binary (or via go run)
if [ -f "nebula-bin" ]; then
    ./nebula-bin swebench \
        --dataset "$DATASET" \
        --split "$SPLIT" \
        --max-instances "$MAX_INSTANCES" \
        --concurrency "$CONCURRENCY" \
        --output-dir "$OUTPUT_DIR" \
        --timeout "$TIMEOUT"
else
    go run cmd/nebula/main.go swebench \
        --dataset "$DATASET" \
        --split "$SPLIT" \
        --max-instances "$MAX_INSTANCES" \
        --concurrency "$CONCURRENCY" \
        --output-dir "$OUTPUT_DIR" \
        --timeout "$TIMEOUT"
fi

echo ""
echo "==========================================================="
echo "✅ Evaluation Complete!"
echo "Check '$OUTPUT_DIR' for full logs and trajectory JSONL files."
echo "==========================================================="
