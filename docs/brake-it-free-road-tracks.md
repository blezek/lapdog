# Free iRacing road tracks used by Brake-It

Checked against iRacing's live site on 2026-09-16.

iRacing's [track archive](https://www.iracing.com/tracks/) marks membership
content with `data-type="free"`, and its
[membership page](https://www.iracing.com/membership/#included) describes the
same set as included with membership. The archive does not offer a separate
road discipline filter, so this list narrows the 29 included entries to paved
road-racing venues. Charlotte is included because iRacing's archive links its
free entry to the [Charlotte Roval](https://www.iracing.com/tracks/charlotte-roval/)
page. Centripetal Circuit is a test facility rather than a race venue; paved
ovals, dirt venues, and rallycross-only entries are also excluded.

The resulting venues are:

- Circuito de Navarra
- Circuit de Ledenon
- Virginia International Raceway
- Motorsport Arena Oschersleben
- Rudskogen Motorsenter
- Winton Motor Raceway
- Lime Rock Park
- Tsukuba Circuit
- Charlotte Motor Speedway
- Snetterton Circuit
- Oran Park Raceway
- Oulton Park Circuit
- Okayama International Circuit
- Summit Point Motorsports Park

`tools/brake-it/garage61_catalog.py` holds this same reviewed allowlist. The
`brake-it` target reads Garage61's track catalog so it can resolve IDs, but it
only requests laps and telemetry for matching venues. At mixed venues it drops
layouts whose Garage61 variant contains `oval`, `dirt`, or `rallycross`; for
example, Charlotte's Roval remains eligible while its oval does not.

Treat the list as dated source data. Re-scrape iRacing's archive and review any
membership changes before editing the allowlist; do not infer that a newly
returned Garage61 track is free.
