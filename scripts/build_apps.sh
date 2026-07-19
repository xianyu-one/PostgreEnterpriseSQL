#!/usr/bin/env bash
# This script scans the apps directory for Go tools, compiles them,
# and puts them into either bin/ or sbin/ based on the presence of
# the is_a_root_tool marker file.

set -euo pipefail

APPS_DIR="${1:-./apps}"
OUTPUT_DIR="${2:-./output}"

echo "Scanning ${APPS_DIR} for Go tools..."

# Ensure output directories exist
mkdir -p "${OUTPUT_DIR}/bin"
mkdir -p "${OUTPUT_DIR}/sbin"

# Resolve absolute path for output directory to avoid path issues when cd-ing
ABS_OUTPUT_DIR=$(cd "${OUTPUT_DIR}" && pwd)

# Scan apps directory
for item in "${APPS_DIR}"/*; do
    if [ -d "${item}" ]; then
        # Skip directories that do not contain Go source files or go.mod
        if ! ls "${item}"/*.go >/dev/null 2>&1 && [ ! -f "${item}/go.mod" ]; then
            echo "Skipping non-Go directory: $(basename "${item}")"
            continue
        fi

        app_name=$(basename "${item}")
        echo "Found Go tool: ${app_name}"

        # Determine target directory
        if [ -f "${item}/is_a_root_tool" ]; then
            target_bin_dir="${ABS_OUTPUT_DIR}/sbin"
            echo "  [Root Tool] Will be installed to sbin/"
        else
            target_bin_dir="${ABS_OUTPUT_DIR}/bin"
            echo "  [User Tool] Will be installed to bin/"
        fi

        # Enter the package directory and build the binary
        pushd "${item}" > /dev/null
        echo "  Compiling ${app_name}..."
        
        # Build with optimizations disabled for size (ldflags -s -w) and static linking (CGO_ENABLED=0)
        if [ -f "go.mod" ]; then
            CGO_ENABLED=0 go build -ldflags="-s -w" -o "${target_bin_dir}/${app_name}" .
        else
            # Fallback for projects without a go.mod file
            if [ -f "main.go" ]; then
                CGO_ENABLED=0 GO111MODULE=auto go build -ldflags="-s -w" -o "${target_bin_dir}/${app_name}" main.go
            else
                CGO_ENABLED=0 GO111MODULE=auto go build -ldflags="-s -w" -o "${target_bin_dir}/${app_name}" *.go
            fi
        fi

        popd > /dev/null
        echo "  Successfully compiled and installed ${app_name}."
    fi
done

echo "Go tools build completed."
