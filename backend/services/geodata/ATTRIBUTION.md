# GeoNames Australia places (offline reverse geocode)

Teslaxy embeds a compact extract of **GeoNames** populated places for Australia
(`feature class P`, including suburb codes such as `PPLX`) so Recent clips can
resolve latitude/longitude to a suburb name **without leaving the LAN**.

| Item | Detail |
|------|--------|
| Source dump | https://download.geonames.org/export/dump/AU.zip |
| Filter | Tab fields where `feature class == P`; columns kept: `name`, `latitude`, `longitude` |
| Embedded file | `au_places_p.tsv.gz` |
| Regenerator | `scripts/generate-au-places.sh` |
| License | [Creative Commons Attribution 4.0 International (CC BY 4.0)](https://creativecommons.org/licenses/by/4.0/) |

GeoNames data is provided “as is” without warranty. This product includes
GeoNames data; when redistributing Teslaxy or this extract, retain this
attribution.

Generated for Teslaxy on 2026-11-01 from the public GeoNames AU country dump.
