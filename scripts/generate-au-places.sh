#!/usr/bin/env bash
# Generate the embedded GeoNames AU populated-places dataset used for
# offline suburb reverse-geocoding. Output is committed under
# backend/services/geodata/au_places_p.tsv.gz (~250 KB).
#
# Source: https://download.geonames.org/export/dump/AU.zip
# License: Creative Commons Attribution 4.0 (CC BY 4.0)
# See backend/services/geodata/ATTRIBUTION.md
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT/backend/services/geodata"
OUT_FILE="$OUT_DIR/au_places_p.tsv.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading GeoNames AU dump..."
curl -fsSL -o "$TMP/AU.zip" "https://download.geonames.org/export/dump/AU.zip"

python3 - <<PY
import gzip, zipfile, os
tmp = "$TMP"
out = "$OUT_FILE"
os.makedirs(os.path.dirname(out), exist_ok=True)
with zipfile.ZipFile(os.path.join(tmp, "AU.zip")) as z:
    z.extract("AU.txt", tmp)
rows = 0
with open(os.path.join(tmp, "AU.txt"), "r", encoding="utf-8") as src, \
     gzip.open(out, "wt", encoding="utf-8", compresslevel=9) as dst:
    for line in src:
        parts = line.rstrip("\n").split("\t")
        if len(parts) < 8:
            continue
        # feature class P = populated place (includes PPLX suburbs)
        if parts[6] != "P":
            continue
        name, lat, lon = parts[1], parts[4], parts[5]
        if not name or not lat or not lon:
            continue
        dst.write(f"{name}\t{lat}\t{lon}\n")
        rows += 1
print(f"Wrote {rows} places -> {out} ({os.path.getsize(out)} bytes)")
PY

echo "Done. Commit $OUT_FILE and refresh ATTRIBUTION.md date if needed."
