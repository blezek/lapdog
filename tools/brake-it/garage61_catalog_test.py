import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import garage61_catalog as catalog


class Garage61CatalogTest(unittest.TestCase):
  def test_testing_pass_requests_only_circuito_de_navarra(self):
    self.assertEqual(
      ["Circuito de Navarra"],
      [target.label for target in catalog.FREE_ROAD_TRACK_TARGETS],
    )

  def test_resolves_only_requested_mx5(self):
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

    self.assertEqual([1], [car["id"] for car in catalog.resolve_cars(cars)])

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

  def test_testing_pass_selects_one_concrete_track_and_one_car(self):
    cars = [{"id": 1}, {"id": 2}]
    tracks = [{"id": 10}, {"id": 20}]

    combinations = catalog.single_car_track_combination(cars, tracks)

    self.assertEqual([(10, 1)], [(track["id"], car["id"]) for track, car in combinations])

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

  def test_minimized_scenario_keeps_sanitized_lap_links(self):
    scenario = {
      "id": "garage61-iracing-track-40-car-1-zone-1",
      "source": {
        "generatedAt": "2026-09-16T18:00:00Z",
        "generatorVersion": "0.2.0",
        "method": "aggregate",
        "model": {"sourceLapCount": 12},
        "sourceLaps": [{
          "lapId": "lap/one",
          "driver": {"name": "Private"},
          "apiLapUrl": "https://garage61.net/api/v1/laps/lap%2Fone",
          "lapTimeSec": 87.123,
          "contribution": {
            "zone": 1,
            "telemetryWindowStartLapPercent": 12.5,
            "telemetryWindowEndLapPercent": 16.75,
          },
        }],
      },
    }
    got = catalog.minimize_scenario(
      scenario,
      {"id": 1, "name": "Global Mazda MX-5 Cup"},
      {"id": 40, "name": "Road Atlanta", "variant": "Full Course"},
    )

    catalog.validate_private_data_absent(got)
    self.assertEqual(12, got["source"]["model"]["sourceLapCount"])
    self.assertEqual("lap/one", got["source"]["sourceLaps"][0]["lapId"])
    self.assertEqual(
      "https://garage61.net/app/analyze;t=lap%2Fone",
      got["source"]["sourceLaps"][0]["garage61Url"],
    )
    self.assertNotIn("driver", got["source"]["sourceLaps"][0])
    self.assertNotIn("apiLapUrl", got["source"]["sourceLaps"][0])

  def test_privacy_validator_rejects_driver_identity_and_raw_telemetry(self):
    with self.assertRaisesRegex(RuntimeError, "private lap-level field"):
      catalog.validate_private_data_absent({"source": {"driver": {"name": "Private"}}})
    with self.assertRaisesRegex(RuntimeError, "private lap-level field"):
      catalog.validate_private_data_absent({"source": {"samples": [0.1, 0.2]}})

  def test_privacy_validator_only_allows_garage61_app_links(self):
    catalog.validate_private_data_absent({
      "source": {
        "garage61Url": "https://garage61.net/app/analyze;t=lap-one",
      },
    })
    with self.assertRaisesRegex(RuntimeError, "invalid Garage61 citation URL"):
      catalog.validate_private_data_absent({
        "source": {"garage61Url": "https://example.com/app/analyze;t=lap-one"},
      })

  def test_completed_combination_is_checkpointed_before_the_next_one(self):
    first = {
      "id": "first-scenario",
      "carName": "Global Mazda MX-5 Cup",
      "trackName": "Circuito de Navarra Speed Circuit",
      "source": {"provider": "garage61"},
    }
    combinations = [
      (
        {"id": 10, "name": "Circuito de Navarra", "variant": "Speed Circuit"},
        {"id": 1, "name": "Global Mazda MX-5 Cup"},
      ),
      (
        {"id": 20, "name": "Second Track", "variant": "Road"},
        {"id": 1, "name": "Global Mazda MX-5 Cup"},
      ),
    ]
    with tempfile.TemporaryDirectory() as temp_dir:
      output = Path(temp_dir) / "catalog.json"
      with mock.patch.object(catalog, "fetch_laps", return_value=[{"id": "lap"}]):
        with mock.patch.object(
          catalog,
          "analyze_combination",
          side_effect=([first], RuntimeError("second combination failed")),
        ):
          with self.assertRaisesRegex(RuntimeError, "second combination failed"):
            catalog.generate_combinations(
              "dedicated-token",
              combinations,
              mock.Mock(),
              "2026-09-18T12:00:00Z",
              output,
            )

      checkpoint = json.loads(output.read_text(encoding="utf-8"))

    self.assertEqual(["first-scenario"], [item["id"] for item in checkpoint["scenarios"]])

  def test_existing_combination_is_not_downloaded_or_duplicated(self):
    existing = {
      "id": "existing-scenario",
      "carName": "Global Mazda MX-5 Cup",
      "trackName": "Circuito de Navarra Speed Circuit",
      "source": {
        "provider": "garage61",
        "sourceLaps": [{
          "lapId": "lap-one",
          "garage61Url": "https://garage61.net/app/analyze;t=lap-one",
        }],
      },
    }
    combination = [
      (
        {"id": 436, "name": "Circuito de Navarra", "variant": "Speed Circuit"},
        {"id": 8, "name": "Global Mazda MX-5 Cup"},
      ),
    ]
    with tempfile.TemporaryDirectory() as temp_dir:
      output = Path(temp_dir) / "catalog.json"
      catalog.write_catalog_checkpoint(
        output,
        "2026-09-18T12:00:00Z",
        [existing, existing],
      )
      loaded = catalog.load_existing_scenarios(output)
      with mock.patch.object(catalog, "fetch_laps") as fetch_laps:
        scenarios = catalog.generate_combinations(
          "dedicated-token",
          combination,
          mock.Mock(),
          "2026-09-18T13:00:00Z",
          output,
          loaded,
        )
      saved = json.loads(output.read_text(encoding="utf-8"))

    fetch_laps.assert_not_called()
    self.assertEqual(["existing-scenario"], [item["id"] for item in scenarios])
    self.assertEqual(["existing-scenario"], [item["id"] for item in saved["scenarios"]])

  def test_existing_combination_without_lap_links_is_regenerated(self):
    existing = {
      "id": "legacy-scenario",
      "carName": "Global Mazda MX-5 Cup",
      "trackName": "Circuito de Navarra Speed Circuit",
      "source": {"provider": "garage61"},
    }
    combination = [(
      {"id": 436, "name": "Circuito de Navarra", "variant": "Speed Circuit"},
      {"id": 8, "name": "Global Mazda MX-5 Cup"},
    )]
    replacement = {
      **existing,
      "id": "replacement-scenario",
      "source": {
        "provider": "garage61",
        "sourceLaps": [{
          "lapId": "lap-one",
          "garage61Url": "https://garage61.net/app/analyze;t=lap-one",
        }],
      },
    }
    with tempfile.TemporaryDirectory() as temp_dir:
      output = Path(temp_dir) / "catalog.json"
      with mock.patch.object(catalog, "fetch_laps", return_value=[{"id": "lap"}]) as fetch_laps:
        with mock.patch.object(catalog, "analyze_combination", return_value=[replacement]):
          scenarios = catalog.generate_combinations(
            "dedicated-token",
            combination,
            mock.Mock(),
            "2026-09-18T13:00:00Z",
            output,
            [existing],
          )

    fetch_laps.assert_called_once()
    self.assertEqual(["replacement-scenario"], [item["id"] for item in scenarios])

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
