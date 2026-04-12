#!/usr/bin/env bash
# entrypoint.sh – IoT-Testware CoAP container entrypoint.
#
# Substitutes SUT_HOST and SUT_PORT placeholders in CoAP_SUT.cfg,
# waits until the SUT is reachable, then runs the test campaign.
#
# Environment variables:
#   SUT_HOST   – hostname / IP of the System Under Test  (default: sut)
#   SUT_PORT   – UDP port of the SUT                     (default: 5683)
set -euo pipefail

: "${SUT_HOST:=sut}"
: "${SUT_PORT:=5683}"

CFG=/home/titan/playground/coap/CoAP_SUT.cfg

echo "[entrypoint] SUT -> ${SUT_HOST}:${SUT_PORT}"

# Replace placeholders with actual values from environment.
sed -i \
    -e "s/SUT_HOST_PLACEHOLDER/${SUT_HOST}/g" \
    -e "s/SUT_PORT_PLACEHOLDER/${SUT_PORT}/g" \
    "${CFG}"

# ── wait for SUT to be ready ─────────────────────────────────────────────────
# nc -zu sends a zero-byte UDP datagram; we just need the DNS/connect to work.
MAX_WAIT=60
WAITED=0
echo "[entrypoint] Waiting for SUT ${SUT_HOST}:${SUT_PORT} ..."
until nc -zu "${SUT_HOST}" "${SUT_PORT}" 2>/dev/null; do
    if [ "${WAITED}" -ge "${MAX_WAIT}" ]; then
        echo "[entrypoint] WARNING: SUT did not respond after ${MAX_WAIT}s, continuing anyway..."
        break
    fi
    sleep 1
    WAITED=$((WAITED + 1))
done
echo "[entrypoint] SUT ready (waited ${WAITED}s)."

# Ensure log directory exists (mapped as Docker volume ./results:/home/titan/logs)
mkdir -p /home/titan/logs

# ── run the test campaign ─────────────────────────────────────────────────────
cd /home/titan/playground/coap

echo "[entrypoint] Starting: ttcn3_start iottestware.coap CoAP_SUT.cfg"
ttcn3_start iottestware.coap CoAP_SUT.cfg
EXIT_CODE=$?

echo "[entrypoint] Test campaign finished (exit code: ${EXIT_CODE})."

# ── generate human-readable report ───────────────────────────────────────────
if command -v python3 >/dev/null 2>&1; then
    echo "[entrypoint] Generating conformance report..."
    python3 /home/titan/parse_log.py /home/titan/logs/ /home/titan/logs/ \
        && echo "[entrypoint] Report written to /home/titan/logs/" \
        || echo "[entrypoint] WARNING: report generation failed (non-fatal)."
else
    echo "[entrypoint] python3 not available – skipping report generation."
fi

exit "${EXIT_CODE}"
