#!/usr/bin/env bash
# E2E scenario from docs/PLAN-sites-releases-environments.md §8.
#
# Requires a running docker compose stack (see compose.yaml) with the
# bootstrap admin token reachable from localhost and two release archives
# with index.html at the archive root:
#   $E2E_TMP/onix-site-1.0.0.zip and $E2E_TMP/onix-site-1.1.0.zip
# (default $E2E_TMP is /tmp).
#
# The script is not idempotent in one DB lifetime: reset the schema
# before a second run.
#!/usr/bin/env bash
# E2E: import -> activate dev -> static+API checks -> promote prod ->
# content/form isolation -> import v1.1.0 -> activate prod -> rollback.
set -euo pipefail

API=http://localhost:8080
DEV=http://localhost:8081
PROD=http://localhost:8082
TOKEN=dev-admin-token-change-me-please
TMP=${E2E_TMP:-/tmp}
ZIP1=$TMP/onix-site-1.0.0.zip
ZIP2=$TMP/onix-site-1.1.0.zip

for zip in "$ZIP1" "$ZIP2"; do
  if [ ! -f "$zip" ]; then
    echo "error: $zip not found — build the test site first and place two" \
      "release archives (index.html at the archive root) at:" >&2
    echo "  ${TMP}/onix-site-1.0.0.zip, ${TMP}/onix-site-1.1.0.zip" >&2
    exit 2
  fi
done

auth() { printf 'Authorization: Bearer %s' "$TOKEN"; }

step() { echo; echo "=== $1"; }

step "1. Import release 1.0.0"
REL1=$(curl -sf -X POST "$API/api/v1/admin/releases" -H "$(auth)" \
  -F "version=1.0.0" -F "tag=dist-v1.0.0" -F "artifact=@$ZIP1;type=application/zip")
REL1_ID=$(echo "$REL1" | jq -r .id)
echo "$REL1" | jq '{id, version, tag, artifact_sha256, artifact_size}'

step "2. Feature pages: schema v1 -> publish dev -> promote prod"
F=$(curl -sf -X POST "$API/api/v1/admin/features" -H "$(auth)" \
  -H 'Content-Type: application/json' \
  -d '{"key":"pages","name":"Страницы","type":"collection"}')
FID=$(echo "$F" | jq -r .id)
REV=$(echo "$F" | jq -r .revision)
DRAFT=$(curl -sf -X PUT "$API/api/v1/admin/features/$FID/draft" -H "$(auth)" \
  -H 'Content-Type: application/json' \
  -d "{\"revision\":$REV,\"schema\":{\"type\":\"object\",\"properties\":{\"title\":{\"type\":\"string\",\"minLength\":1}},\"required\":[\"title\"],\"additionalProperties\":false}}")
REV=$(echo "$DRAFT" | jq -r .revision)
PUB=$(curl -sf -X POST "$API/api/v1/admin/features/$FID/publish" -H "$(auth)" \
  -H 'Content-Type: application/json' -d "{\"revision\":$REV}")
REV=$(echo "$PUB" | jq -r .revision)
PR=$(curl -sf -X POST "$API/api/v1/admin/features/$FID/promote" -H "$(auth)" \
  -H 'Content-Type: application/json' -d "{\"revision\":$REV}")
echo "  feature: $FID  schema prod v$(echo "$PR" | jq -r .prod_version.version)"

step "3. Entry v1: publish dev + promote prod"
E=$(curl -sf -X POST "$API/api/v1/admin/features/$FID/entries" -H "$(auth)" \
  -H 'Content-Type: application/json' \
  -d '{"data":{"title":"Страница v1"}}')
EID=$(echo "$E" | jq -r .id)
EREV=$(echo "$E" | jq -r .revision)
EMBER_=$((0))
EREV=$(curl -sf -X POST "$API/api/v1/admin/entries/$EID/publish" -H "$(auth)" \
  -H 'Content-Type: application/json' -d "{\"revision\":$EREV}" | jq -r .revision)
EREV=$(curl -sf -X POST "$API/api/v1/admin/entries/$EID/promote" -H "$(auth)" \
  -H 'Content-Type: application/json' -d "{\"revision\":$EREV}" | jq -r .revision)
echo "  entry: $EID published in both environments"

step "4. Activate 1.0.0 in dev and check static + API"
curl -sf -X POST "$API/api/v1/admin/releases/$REL1_ID/activate" -H "$(auth)" \
  -H 'Content-Type: application/json' -d '{"environment":"dev"}' > /dev/null
sleep 1
BODY=$(curl -sf "$DEV/")
echo "$BODY" | grep -q "Geist\|<html" && echo "  dev static: OK ($(printf %s "$BODY" | wc -c | tr -d ' ') bytes)"
curl -sf "$DEV/api/v1/content/pages" | jq -c '{environment, entries: (.data.entries | length), title: .data.entries[0].data.title}'
CORS=$(curl -s -o /dev/null -w '%{http_code}' -X OPTIONS "$DEV/api/v1/content/pages" \
  -H 'Origin: http://localhost:8081' -H 'Access-Control-Request-Method: GET')
echo "  dev CORS preflight: $CORS"

step "5. Promote 1.0.0 to prod"
curl -sf -X POST "$API/api/v1/admin/releases/$REL1_ID/activate" -H "$(auth)" \
  -H 'Content-Type: application/json' -d '{"environment":"prod"}' > /dev/null
sleep 1
PBODY=$(curl -sf "$PROD/")
echo "  prod static: OK ($(printf %s "$PBODY" | wc -c | tr -d ' ') bytes)"
curl -sf "$PROD/api/v1/content/pages" | jq -c '{environment, title: .data.entries[0].data.title}'

step "6. Content isolation: dev-only change"
NEWREV=$(curl -sf "$API/api/v1/admin/entries/$EID" -H "$(auth)" | jq -r .revision)
DJ=$(curl -sf -X PATCH "$API/api/v1/admin/entries/$EID/draft" -H "$(auth)" \
  -H 'Content-Type: application/json' -d "{\"revision\":$NEWREV,\"data\":{\"title\":\"dev версия\"}}")
DREV=$(echo "$DJ" | jq -r .revision)
curl -sf -X POST "$API/api/v1/admin/entries/$EID/publish" -H "$(auth)" \
  -H 'Content-Type: application/json' -d "{\"revision\":$DREV}" > /dev/null
echo -n "  dev sees: ";  curl -sf "$DEV/api/v1/content/pages"  | jq -c '{title: .data.entries[0].data.title}'
echo -n "  prod sees: "; curl -sf "$PROD/api/v1/content/pages" | jq -c '{title: .data.entries[0].data.title}'

step "7. Forms per environment"
FF=$(curl -sf -X POST "$API/api/v1/admin/features" -H "$(auth)" \
  -H 'Content-Type: application/json' \
  -d '{"key":"feedback","name":"Обратная связь","type":"form"}')
FFID=$(echo "$FF" | jq -r .id)
FREV=$(echo "$FF" | jq -r .revision)
FREV2=$(curl -sf -X PUT "$API/api/v1/admin/features/$FFID/draft" -H "$(auth)" \
  -H 'Content-Type: application/json' \
  -d "{\"revision\":$FREV,\"schema\":{\"type\":\"object\",\"properties\":{\"message\":{\"type\":\"string\",\"minLength\":1}},\"required\":[\"message\"],\"additionalProperties\":false}}" | jq -r .revision)
PUBRESP=$(curl -sf -X POST "$API/api/v1/admin/features/$FFID/publish" -H "$(auth)" \
  -H 'Content-Type: application/json' -d "{\"revision\":$FREV2}")
FREV2=$(echo "$PUBRESP" | jq -r .revision)
curl -sf -X POST "$API/api/v1/admin/features/$FFID/promote" -H "$(auth)" \
  -H 'Content-Type: application/json' -d "{\"revision\":$FREV2}" > /dev/null
SDEV=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$DEV/api/v1/forms/feedback/submissions" \
  -H 'Content-Type: application/json' -d '{"message":"заявка из dev"}')
SPROD=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$PROD/api/v1/forms/feedback/submissions" \
  -H 'Content-Type: application/json' -d '{"message":"заявка из prod"}')
echo "  dev form submit: $SDEV, prod form submit: $SPROD"
curl -sf "$API/api/v1/admin/forms/feedback/submissions?environment=prod" -H "$(auth)" | jq -c '{prod_count: (.items|length), env: .items[0].environment}'

step "8. Import and activate 1.1.0 in prod, then rollback to 1.0.0"
REL2=$(curl -sf -X POST "$API/api/v1/admin/releases" -H "$(auth)" \
  -F "version=1.1.0" -F "tag=dist-v1.1.0" -F "artifact=@$ZIP2;type=application/zip")
REL2_ID=$(echo "$REL2" | jq -r .id)
echo "$REL2" | jq -c '{version, tag, artifact_sha256: .artifact_sha256[0:16]}'
curl -sf -X POST "$API/api/v1/admin/releases/$REL2_ID/activate" -H "$(auth)" \
  -H 'Content-Type: application/json' -d '{"environment":"prod"}' > /dev/null
sleep 1
echo -n "  prod after 1.1.0: "; curl -sf "$PROD/" | grep -o "v1\.1\.0" | head -1 || echo "(нет метки в html)"
curl -sf -X POST "$API/api/v1/admin/releases/$REL1_ID/rollback" -H "$(auth)" \
  -H 'Content-Type: application/json' -d '{"environment":"prod"}' > /dev/null
sleep 1
echo -n "  prod after rollback: "; curl -sf "$PROD/" | head -c 60 >/dev/null && echo "OK (отдаёт стат)"
curl -sf "$PROD/api/v1/content/pages" | jq -c '{title: .data.entries[0].data.title}'

step "9. Environment settings via PATCH (CORS)"
curl -sf "$API/api/v1/admin/site/environments" -H "$(auth)" > "$TMP/envs.json"
DREV2=$(jq -r '.items[] | select(.key=="dev") | .revision' "$TMP/envs.json")
PTS=$(curl -sf -X PATCH "$API/api/v1/admin/site/environments/dev" -H "$(auth)" \
  -H 'Content-Type: application/json' \
  -d "{\"cors_origins\":[\"https://onix.example\"],\"revision\":$DREV2}")
echo "$PTS" | jq -c '{key, cors_origins, revision}'
sleep 1
CORS2=$(curl -s -o /dev/null -w '%{http_code}' -X OPTIONS "$DEV/api/v1/content/pages" \
  -H 'Origin: https://onix.example' -H 'Access-Control-Request-Method: GET')
echo "  preflight with new origin: $CORS2"
CORS3=$(curl -s -o /dev/null -w '%{http_code}' -X OPTIONS "$DEV/api/v1/content/pages" \
  -H 'Origin: http://localhost:8081' -H 'Access-Control-Request-Method: GET')
echo "  preflight with old bootstrap origin: $CORS3"

step "10. Audit: release events"
curl -sf "$API/api/v1/admin/publication-events?limit=10" -H "$(auth)" | jq -c '.items | map({kind, action, environment}) | .[0:5]'

echo
echo "=== E2E COMPLETE ==="
