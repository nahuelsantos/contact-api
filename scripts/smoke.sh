#!/usr/bin/env bash
# Post-deploy smoke: submits the `smoke` site's contact form on a live deployment. This sends one
# real mail, so check the smoke inbox afterwards.
#
#   scripts/smoke.sh <base-url>     e.g. scripts/smoke.sh https://nahuelsantos.com
set -euo pipefail

base="${1:?usage: smoke.sh <base-url>}"
base="${base%/}"

body="$(printf '{"name":"smoke","email":"smoke@example.com","subject":"smoke %s","message":"post-deploy smoke"}' "$(date -u +%FT%TZ)")"
status="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 30 \
  -X POST "$base/api/v2/sites/smoke/forms/contact" \
  -H 'Content-Type: application/json' -H 'Origin: https://nahuelsantos.com' \
  --data "$body")"

[ "$status" = 200 ] || { echo "smoke: expected 200, got $status" >&2; exit 1; }
echo "smoke: ok (200); check the smoke inbox"
