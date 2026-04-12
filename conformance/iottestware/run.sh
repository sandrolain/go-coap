#!/usr/bin/env bash
# run.sh – convenience wrapper to build images and run the CoAP conformance suite.
#
# Usage (from the workspace root):
#   ./iottestware/run.sh [--no-cache] [--test TC_COAP_SERVER_001]
#
# Options:
#   --no-cache   pass --no-cache to docker build
#   --test TC    run only the named test case (e.g. TC_COAP_SERVER_001)
#                must be a fully-qualified TTCN-3 name or just the module
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

BUILD_ARGS=()
TC_NAME=""

for arg in "$@"; do
    case "${arg}" in
        --no-cache) BUILD_ARGS+=(--no-cache) ;;
        --test)     shift; TC_NAME="$1" ;;
        *)          ;;
    esac
done

echo "=== Building SUT image (go-coap-sut) ==="
docker build "${BUILD_ARGS[@]}" \
    -t go-coap-sut \
    -f "${SCRIPT_DIR}/Dockerfile.sut" \
    "${SCRIPT_DIR}/../.."

echo ""
echo "=== Building testware image (iottestware-coap) ==="
docker build "${BUILD_ARGS[@]}" \
    -t iottestware-coap \
    -f "${SCRIPT_DIR}/Dockerfile.testware" \
    "${SCRIPT_DIR}"

mkdir -p "${SCRIPT_DIR}/results"

echo ""
echo "=== Starting test campaign ==="

DOCKER_RUN_ARGS=(
    --rm
    -v "${SCRIPT_DIR}/results:/home/titan/logs"
    -e SUT_HOST=sut
    -e SUT_PORT=5683
)

if [ -n "${TC_NAME}" ]; then
    DOCKER_RUN_ARGS+=(-e "TC_NAME=${TC_NAME}")
    echo "Running single test: ${TC_NAME}"
fi

# Start SUT in the background on a shared network.
NETWORK="coap-test-net-$$"
docker network create "${NETWORK}" 2>/dev/null || true
trap 'docker network rm "${NETWORK}" 2>/dev/null || true' EXIT

SUT_ID=$(docker run -d \
    --network "${NETWORK}" \
    --name sut \
    go-coap-sut)
trap 'docker rm -f "${SUT_ID}" 2>/dev/null || true; docker network rm "${NETWORK}" 2>/dev/null || true' EXIT

echo "SUT container: ${SUT_ID}"

# Run the testware container (interactive, waits for completion).
docker run "${DOCKER_RUN_ARGS[@]}" \
    --network "${NETWORK}" \
    iottestware-coap

echo ""
echo "=== Results stored in ${SCRIPT_DIR}/results/ ==="
