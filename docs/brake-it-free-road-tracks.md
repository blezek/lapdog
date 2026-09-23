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

The active configuration in `tools/brake-it/garage61_catalog.py` includes
Circuito de Navarra's Speed Circuit from this list plus Road Atlanta's Full
Course and Circuit de Spa-Francorchamps's Grand Prix Pits, two explicitly
requested paid venues. The full list above remains the reviewed included-content
expansion set. The `brake-it` target reads Garage61's track catalog so it can
resolve IDs, and it drops layouts whose variant contains `oval`, `dirt`, or
`rallycross`.

Treat the list as dated source data. Re-scrape iRacing's archive and review any
membership changes before editing the allowlist; do not infer that a newly
returned Garage61 track is free.
