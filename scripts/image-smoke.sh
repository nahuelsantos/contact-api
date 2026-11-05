#!/usr/bin/env bash
# Boots a built image and checks what the deploy gate and visitors rely on: the HEALTHCHECK turns
# healthy, /healthz and /version.json answer, and the v2 form route answers 404 / 422 without
# ever sending mail. Everything runs on a private docker network, so it works wherever the docker
# daemon is (the CI runner's included).
#
#   scripts/image-smoke.sh <image>
set -euo pipefail

image="${1:?usage: image-smoke.sh <image>}"
NET="contact-api-smoke-$$"

cleanup() { docker rm -f "$NET-app" >/dev/null 2>&1 || true; docker network rm "$NET" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker network create "$NET" >/dev/null
docker run -d --name "$NET-app" --network "$NET" "$image" >/dev/null

health=starting
for _ in $(seq 1 30); do
  health="$(docker inspect --format '{{.State.Health.Status}}' "$NET-app")"
  [ "$health" = healthy ] && break
  [ "$health" = unhealthy ] && { docker logs "$NET-app"; echo "image-smoke: unhealthy" >&2; exit 1; }
  sleep 1
done
[ "$health" = healthy ] || { docker logs "$NET-app"; echo "image-smoke: not healthy after 30s ($health)" >&2; exit 1; }

get() { docker run --rm --network "$NET" alpine:3.21 wget -qO- "$@"; }
# Empty-JSON POST with the example site's origin. busybox wget exits non-zero on 4xx, so run it
# with -S (server response on stderr), ignore the exit code and read the status line and body.
post() {
  docker run --rm --network "$NET" alpine:3.21 wget -S -O- \
    --header 'Content-Type: application/json' --header 'Origin: https://example.com' \
    --post-data '{}' "http://$NET-app/api/v2/sites/$1/forms/contact" 2>&1 || true
}

[ "$(get "http://$NET-app/healthz")" = ok ] || { echo "image-smoke: /healthz did not say ok" >&2; exit 1; }
# Capture first, then match: with pipefail, `... | grep -q` fails at random when grep stops reading
# after its match and the docker run feeding it dies of SIGPIPE.
version="$(get "http://$NET-app/version.json")"
grep -q '"sha":' <<<"$version" || { echo "image-smoke: no /version.json" >&2; exit 1; }

unknown="$(post nope)"
grep -q 'HTTP/1.1 404' <<<"$unknown" || { echo "image-smoke: unknown site was not a 404" >&2; echo "$unknown" >&2; exit 1; }

invalid="$(post example)"
grep -q 'HTTP/1.1 422' <<<"$invalid" || { echo "image-smoke: empty form was not a 422" >&2; echo "$invalid" >&2; exit 1; }
grep -q '"required"' <<<"$invalid" || { echo "image-smoke: 422 body has no \"required\"" >&2; echo "$invalid" >&2; exit 1; }

echo "image-smoke: ok ($image)"
