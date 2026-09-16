import unittest
from unittest import mock

import garage61_catalog as catalog


class Garage61CatalogTest(unittest.TestCase):
  def test_free_road_track_list_matches_reviewed_iracing_membership_page(self):
    self.assertEqual(
      [
        "Circuito de Navarra",
        "Circuit de Ledenon",
        "Virginia International Raceway",
        "Motorsport Arena Oschersleben",
        "Rudskogen Motorsenter",
        "Winton Motor Raceway",
        "Lime Rock Park",
        "Tsukuba Circuit",
        "Charlotte Motor Speedway",
        "Snetterton Circuit",
        "Oran Park Raceway",
        "Oulton Park Circuit",
        "Okayama International Circuit",
        "Summit Point Motorsports Park",
      ],
      [target.label for target in catalog.FREE_ROAD_TRACK_TARGETS],
    )

  def test_resolves_requested_iracing_car_families(self):
    cars = [
      {"id": 1, "name": "Global Mazda MX-5 Cup", "platform": "iracing"},
      {"id": 200, "name": "BMW M2 Racing (G87)", "platform": "iracing"},
      {"id": 2, "name": "BMW M2 Racing (G87)", "platform": "iracing"},
      {"id": 194, "name": "Porsche 911 Cup (992.2)", "platform": "iracing"},
      {"id": 3, "name": "Porsche 911 Cup (992.2)", "platform": "iracing"},
      {"id": 4, "name": "BMW M4 GT3", "platform": "iracing"},
      {"id": 5, "name": "BMW M4 GT3 Evo", "platform": "iracing"},
      {"id": 6, "name": "Global Mazda MX-5 Cup", "platform": "acc"},
    ]

    self.assertEqual([200, 4, 1, 194], [car["id"] for car in catalog.resolve_cars(cars)])

  def test_resolves_only_road_layouts_at_requested_free_venues(self):
    targets = (
      catalog.TrackTarget("Charlotte Motor Speedway", ("Charlotte Motor Speedway",)),
      catalog.TrackTarget(
        "Summit Point Motorsports Park",
        ("Summit Point Motorsports Park", "Summit Point Raceway"),
      ),
    )
    tracks = [
      {"id": 10, "name": "Charlotte Motor Speedway", "variant": "Roval", "platform": "iracing"},
      {"id": 11, "name": "Charlotte Motor Speedway", "variant": "Oval - 2018", "platform": "iracing"},
      {"id": 12, "name": "Charlotte Motor Speedway", "variant": "Dirt Road", "platform": "iracing"},
      {"id": 13, "name": "Summit Point Raceway", "variant": "Jefferson Circuit", "platform": "iracing"},
      {"id": 14, "name": "Summit Point Raceway", "variant": "Oval", "platform": "iracing"},
      {"id": 15, "name": "Road America", "variant": "Full Course", "platform": "iracing"},
      {"id": 16, "name": "Charlotte Motor Speedway", "variant": "Roval", "platform": "acc"},
    ]

    self.assertEqual([10, 13], [track["id"] for track in catalog.resolve_tracks(tracks, targets)])

  def test_track_resolution_fails_if_a_reviewed_venue_disappears(self):
    targets = (catalog.TrackTarget("Lime Rock Park", ("Lime Rock Park",)),)

    with self.assertRaisesRegex(RuntimeError, "included road venue.*Lime Rock Park"):
      catalog.resolve_tracks(
        [{"id": 15, "name": "Road America", "variant": "Full Course", "platform": "iracing"}],
        targets,
      )

  def test_fetch_laps_reads_three_ordered_blocks_and_returns_fastest_visible_twelve(self):
    pages = [
      {"items": self.laps(100, 12, visible=False), "total": 100},
      {"items": self.laps(112, 12, visible=True), "total": 100},
      {"items": self.laps(124, 12, visible=True), "total": 100},
    ]
    with mock.patch.object(catalog.generator, "request", side_effect=pages) as request:
      laps = catalog.fetch_laps("dedicated-token", 42, 7)

    self.assertEqual([f"lap-{value}" for value in range(112, 124)], [lap["id"] for lap in laps])
    self.assertEqual(3, request.call_count)
    for offset, call in zip((0, 12, 24), request.call_args_list):
      self.assertEqual(
        {
          "tracks": [42],
          "cars": [7],
          "group": "driver-car",
          "limit": 12,
          "offset": offset,
          "lapTypes": [1],
        },
        call.args[2],
      )
      self.assertNotIn("seeTelemetry", call.args[2])

  def test_fetch_laps_continues_after_three_blocks_until_twelve_are_visible(self):
    pages = [
      {"items": self.laps(100, 12, visible_count=3), "total": 60},
      {"items": self.laps(112, 12, visible_count=3), "total": 60},
      {"items": self.laps(124, 12, visible_count=3), "total": 60},
      {"items": self.laps(136, 12, visible_count=3), "total": 60},
    ]
    with mock.patch.object(catalog.generator, "request", side_effect=pages) as request:
      laps = catalog.fetch_laps("dedicated-token", 42, 7)

    self.assertEqual(12, len(laps))
    self.assertEqual(4, request.call_count)

  def test_fetch_laps_stops_when_results_are_exhausted(self):
    page = self.laps(100, 7, visible=True)
    with mock.patch.object(
      catalog.generator,
      "request",
      return_value={"items": page, "total": len(page)},
    ) as request:
      laps = catalog.fetch_laps("dedicated-token", 42, 7)

    self.assertEqual(page, laps)
    request.assert_called_once()

  def test_fetch_laps_rejects_an_ordering_violation_between_blocks(self):
    pages = [
      {"items": self.laps(100, 12, visible=True), "total": 36},
      {"items": self.laps(110, 12, visible=True), "total": 36},
    ]
    with mock.patch.object(catalog.generator, "request", side_effect=pages):
      with self.assertRaisesRegex(RuntimeError, "not ordered fastest-first.*track 42, car 7"):
        catalog.fetch_laps("dedicated-token", 42, 7)

  def test_fetch_laps_rejects_a_missing_lap_time(self):
    with mock.patch.object(
      catalog.generator,
      "request",
      return_value={"items": [{"id": "missing", "canViewTelemetry": True}], "total": 1},
    ):
      with self.assertRaisesRegex(RuntimeError, "no numeric lapTime"):
        catalog.fetch_laps("dedicated-token", 42, 7)

  def test_minimized_scenario_keeps_aggregates_but_not_laps(self):
    scenario = {
      "id": "garage61-iracing-track-40-car-1-zone-1",
      "source": {
        "generatedAt": "2026-09-16T18:00:00Z",
        "generatorVersion": "0.2.0",
        "method": "aggregate",
        "model": {"sourceLapCount": 12},
        "sourceLaps": [{"lapId": "private", "driver": {"name": "Private"}}],
      },
    }
    got = catalog.minimize_scenario(
      scenario,
      {"id": 1, "name": "Global Mazda MX-5 Cup"},
      {"id": 40, "name": "Road Atlanta", "variant": "Full Course"},
    )

    catalog.validate_private_data_absent(got)
    self.assertEqual(12, got["source"]["model"]["sourceLapCount"])
    self.assertNotIn("sourceLaps", got["source"])

  def test_privacy_validator_rejects_lap_identity(self):
    with self.assertRaisesRegex(RuntimeError, "private lap-level field"):
      catalog.validate_private_data_absent({"source": {"lapId": "private"}})
    with self.assertRaisesRegex(RuntimeError, "private lap-level field"):
      catalog.validate_private_data_absent({"source": {"samples": [0.1, 0.2]}})

  @staticmethod
  def laps(start: int, count: int, *, visible=None, visible_count: int = 0):
    return [
      {
        "id": f"lap-{value}",
        "lapTime": float(value),
        "canViewTelemetry": visible if visible is not None else index < visible_count,
      }
      for index, value in enumerate(range(start, start + count))
    ]


if __name__ == "__main__":
  unittest.main()
