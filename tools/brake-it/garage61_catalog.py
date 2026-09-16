#!/usr/bin/env python3
"""Build a privacy-minimized Brake-It catalog from Garage 61 telemetry.

The access token is used only by this local developer tool. Raw telemetry and
lap-level provenance stay in memory; the JSON written to disk contains only
car/track labels and aggregate braking parameters.
"""

from __future__ import annotations

import argparse
import json
import logging
import math
import os
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import garage61_generate as generator


CATALOG_VERSION = 1
LAP_BLOCK_SIZE = 12
MIN_LAP_BLOCKS = 3
LOGGER = logging.getLogger("brake_it.catalog")


@dataclass(frozen=True)
class CarTarget:
  label: str
  terms: tuple[str, ...]
  names: tuple[str, ...]
  expected_id: int | None = None


@dataclass(frozen=True)
class TrackTarget:
  label: str
  names: tuple[str, ...]


CAR_TARGETS = (
  CarTarget("Mazda MX-5", ("mazda", "mx", "5"), ("Global Mazda MX-5 Cup", "Mazda MX-5 Cup")),
  CarTarget("BMW M2 Racing (G87)", ("bmw", "m2", "racing", "g87"), ("BMW M2 Racing (G87)",), 200),
  CarTarget("Porsche 911 Cup (992.2)", ("porsche", "911", "cup", "992", "2"), ("Porsche 911 Cup (992.2)",), 194),
  CarTarget("BMW M4 GT3", ("bmw", "m4", "gt3"), ("BMW M4 GT3",)),
)

# Scraped from iRacing's Included with Membership track list on 2026-09-16,
# then narrowed to paved road-racing venues. See docs/brake-it-free-road-tracks.md.
# Keep aliases exact: fuzzy matching could quietly turn a free venue into a paid
# or legacy one after either catalog changes.
FREE_ROAD_TRACK_TARGETS = (
  TrackTarget("Circuito de Navarra", ("Circuito de Navarra",)),
  TrackTarget("Circuit de Ledenon", ("Circuit de Ledenon", "Circuit de Lédenon")),
  TrackTarget("Virginia International Raceway", ("Virginia International Raceway",)),
  TrackTarget("Motorsport Arena Oschersleben", ("Motorsport Arena Oschersleben",)),
  TrackTarget("Rudskogen Motorsenter", ("Rudskogen Motorsenter",)),
  TrackTarget("Winton Motor Raceway", ("Winton Motor Raceway",)),
  TrackTarget("Lime Rock Park", ("Lime Rock Park",)),
  TrackTarget("Tsukuba Circuit", ("Tsukuba Circuit",)),
  TrackTarget("Charlotte Motor Speedway", ("Charlotte Motor Speedway",)),
  TrackTarget("Snetterton Circuit", ("Snetterton Circuit",)),
  TrackTarget("Oran Park Raceway", ("Oran Park Raceway",)),
  TrackTarget("Oulton Park Circuit", ("Oulton Park Circuit",)),
  TrackTarget("Okayama International Circuit", ("Okayama International Circuit",)),
  TrackTarget(
    "Summit Point Motorsports Park",
    ("Summit Point Motorsports Park", "Summit Point Raceway"),
  ),
)

NON_ROAD_VARIANT_TERMS = frozenset(("dirt", "oval", "rallycross"))


def parse_args() -> argparse.Namespace:
  parser = argparse.ArgumentParser(
    description="Generate a local SQLite-ready Brake-It catalog from Garage 61.",
  )
  parser.add_argument("--output", required=True)
  parser.add_argument("--csv-limit", type=int, default=12)
  parser.add_argument("--max-laps", type=int, default=100)
  parser.add_argument("--min-coverage", type=float, default=0.35)
  parser.add_argument("--cluster-tolerance", type=float, default=0.018)
  parser.add_argument("--approach-ms", type=int, default=2200)
  return parser.parse_args()


def items(response: Any, endpoint: str) -> list[dict[str, Any]]:
  if isinstance(response, list):
    return response
  if isinstance(response, dict) and isinstance(response.get("items"), list):
    return response["items"]
  raise RuntimeError(f"Garage 61 {endpoint} response did not contain an items array")


def normalized(value: str) -> tuple[str, ...]:
  return tuple(re.findall(r"[a-z0-9]+", value.casefold()))


def resolve_cars(cars: list[dict[str, Any]]) -> list[dict[str, Any]]:
  iracing = [car for car in cars if str(car.get("platform", "")).casefold() == "iracing"]
  selected: dict[int, dict[str, Any]] = {}
  missing: list[str] = []
  for target in CAR_TARGETS:
    exact_names = {normalized(name) for name in target.names}
    matches = [
      car
      for car in iracing
      if normalized(str(car.get("name", ""))) in exact_names
      and (target.expected_id is None or int(car.get("id", 0)) == target.expected_id)
    ]
    if len(matches) > 1:
      choices = ", ".join(f"{car['name']} (id {car['id']})" for car in matches)
      raise RuntimeError(f"Garage 61 car name {target.label!r} is ambiguous: {choices}")
    if not matches:
      matches = [
        car
        for car in iracing
        if all(term in normalized(str(car.get("name", ""))) for term in target.terms)
        and (target.expected_id is None or int(car.get("id", 0)) == target.expected_id)
      ]
      if len(matches) > 1:
        choices = ", ".join(f"{car['name']} (id {car['id']})" for car in matches)
        raise RuntimeError(f"Garage 61 car name {target.label!r} is ambiguous: {choices}")
    if not matches:
      missing.append(target.label)
      continue
    LOGGER.info(
      "Car | %s | %s",
      target.label,
      ", ".join(f"{car['name']} (id {car['id']})" for car in matches),
    )
    for car in matches:
      selected[int(car["id"])] = car
  if missing:
    available = ", ".join(sorted(str(car.get("name")) for car in iracing))
    raise RuntimeError(
      "Garage 61 did not contain the requested car family/families: "
      + ", ".join(missing)
      + f". Available iRacing cars: {available}",
    )
  return sorted(selected.values(), key=lambda car: (str(car["name"]), int(car["id"])))


def resolve_tracks(
  tracks: list[dict[str, Any]],
  targets: tuple[TrackTarget, ...] = FREE_ROAD_TRACK_TARGETS,
) -> list[dict[str, Any]]:
  """Select only current included road venues and their road layouts.

  Garage61 exposes every layout as a separate track record. Venue names must
  match the reviewed iRacing list exactly, and plainly non-road variants at a
  mixed venue (for example Charlotte's oval) are excluded.
  """
  iracing = [
    track for track in tracks
    if str(track.get("platform", "")).casefold() == "iracing"
  ]
  selected: dict[int, dict[str, Any]] = {}
  missing: list[str] = []
  for target in targets:
    exact_names = {normalized(name) for name in target.names}
    matches = [
      track
      for track in iracing
      if normalized(str(track.get("name", ""))) in exact_names
      and NON_ROAD_VARIANT_TERMS.isdisjoint(normalized(str(track.get("variant", ""))))
    ]
    if not matches:
      missing.append(target.label)
      continue
    LOGGER.info(
      "Track catalog | %s | %s",
      target.label,
      ", ".join(
        f"{track_label(track)} (id {track['id']})"
        for track in sorted(matches, key=track_sort_key)
      ),
    )
    for track in matches:
      selected[int(track["id"])] = track
  if missing:
    available = ", ".join(sorted({str(track.get("name")) for track in iracing}))
    raise RuntimeError(
      "Garage 61 did not contain the included road venue(s): "
      + ", ".join(missing)
      + f". Available iRacing tracks: {available}",
    )
  return sorted(selected.values(), key=track_sort_key)


def track_sort_key(track: dict[str, Any]) -> tuple[str, str, int]:
  return (
    str(track.get("name", "")),
    str(track.get("variant", "")),
    int(track["id"]),
  )


def fetch_laps(
  token: str,
  track_id: int,
  car_id: int,
) -> list[dict[str, Any]]:
  visible: list[dict[str, Any]] = []
  offset = 0
  blocks = 0
  previous_lap_time: float | None = None
  while True:
    response = generator.request(
      token,
      "/laps",
      {
        "tracks": [track_id],
        "cars": [car_id],
        "group": "driver-car",
        "limit": LAP_BLOCK_SIZE,
        "offset": offset,
        "lapTypes": [1],
      },
    )
    page = items(response, "/laps")
    blocks += 1
    for lap in page:
      try:
        lap_time = float(lap["lapTime"])
      except (KeyError, TypeError, ValueError) as error:
        raise RuntimeError(
          f"Garage 61 lap ordering cannot be verified for track {track_id}, car {car_id}: "
          "a lap has no numeric lapTime",
        ) from error
      if not math.isfinite(lap_time):
        raise RuntimeError(
          f"Garage 61 lap ordering cannot be verified for track {track_id}, car {car_id}: "
          f"lapTime is {lap_time}",
        )
      if previous_lap_time is not None and lap_time < previous_lap_time:
        raise RuntimeError(
          f"Garage 61 laps are not ordered fastest-first for track {track_id}, car {car_id}: "
          f"{lap_time:.6f}s followed {previous_lap_time:.6f}s",
        )
      previous_lap_time = lap_time
      if lap.get("canViewTelemetry") and len(visible) < LAP_BLOCK_SIZE:
        visible.append(lap)

    fetched = offset + len(page)
    total = int(response.get("total", fetched)) if isinstance(response, dict) else fetched
    exhausted = not page or fetched >= total
    if exhausted or (blocks >= MIN_LAP_BLOCKS and len(visible) >= LAP_BLOCK_SIZE):
      return visible
    offset += len(page)


def source_entity(record: dict[str, Any], kind: str) -> dict[str, Any]:
  value = generator.entity(record)
  value["kind"] = kind
  return value


def analyze_combination(
  token: str,
  track: dict[str, Any],
  car: dict[str, Any],
  laps: list[dict[str, Any]],
  args: argparse.Namespace,
  generated_at: str,
) -> list[dict[str, Any]]:
  visible = sorted(
    (lap for lap in laps if lap.get("canViewTelemetry")),
    key=lambda lap: float(lap.get("lapTime") or math.inf),
  )[: min(args.csv_limit, args.max_laps, 100)]
  if not visible:
    return []

  events: list[generator.LapEvent] = []
  analyzed_laps = 0
  for index, lap in enumerate(visible, start=1):
    LOGGER.info(
      "Telemetry %d/%d | %s | %s",
      index,
      len(visible),
      car["name"],
      track_label(track),
    )
    csv_text = generator.request(token, f"/laps/{lap['id']}/csv", expect_text=True)
    if csv_text is None:
      continue
    rows = generator.load_csv_rows(csv_text)
    if len(rows) < 100:
      continue
    lap_events = generator.detect_events(lap, rows)
    if lap_events:
      analyzed_laps += 1
      events.extend(lap_events)
  if not events:
    return []
  min_source_laps = max(3, round(analyzed_laps * args.min_coverage))
  clusters = [
    cluster
    for cluster in generator.cluster_events(events, args.cluster_tolerance)
    if len({event.lap["id"] for event in cluster.events}) >= min_source_laps
  ]
  clusters.sort(key=lambda cluster: cluster.center)
  if not clusters:
    return []

  query = {
    "tracks": [int(track["id"])],
    "cars": [int(car["id"])],
    "group": "driver-car",
    "limit": args.max_laps,
    "lapTypes": [1],
  }
  prefix = f"{track_label(track)} {car['name']}"
  output = []
  for zone, cluster in enumerate(clusters, start=1):
    scenario = generator.make_scenario(
      cluster,
      zone,
      analyzed_laps,
      query,
      generated_at,
      args.approach_ms,
      prefix,
      {},
      generator.DEFAULT_TELEMETRY_VIEW,
      source_entity(car, "car"),
    )
    output.append(minimize_scenario(scenario, car, track))
  return output


def track_label(track: dict[str, Any]) -> str:
  return " ".join(
    value.strip()
    for value in (str(track.get("name", "")), str(track.get("variant", "")))
    if value.strip()
  )


def minimize_scenario(
  scenario: dict[str, Any],
  car: dict[str, Any],
  track: dict[str, Any],
) -> dict[str, Any]:
  source = scenario.pop("source")
  scenario["carName"] = str(car["name"])
  scenario["trackName"] = track_label(track)
  scenario["source"] = {
    "provider": "garage61",
    "generatedAt": source["generatedAt"],
    "generatorVersion": source["generatorVersion"],
    "method": source["method"],
    "car": {"name": str(car["name"])},
    "track": {
      "name": str(track["name"]),
      **({"variant": str(track["variant"])} if track.get("variant") else {}),
    },
    "model": source["model"],
  }
  return scenario


def validate_private_data_absent(value: Any, path: str = "catalog") -> None:
  banned_keys = {
    "driver",
    "drivers",
    "driverid",
    "driverslug",
    "lapid",
    "laps",
    "samples",
    "telemetry",
    "rows",
    "csv",
    "raw",
    "sourcelaps",
    "garage61url",
    "garage61telemetryurl",
    "garage61analysisurl",
    "garage61analyzeurl",
    "apilapurl",
    "apicsvurl",
    "authorization",
    "token",
  }
  if isinstance(value, dict):
    for key, child in value.items():
      folded = re.sub(r"[^a-z0-9]", "", str(key).casefold())
      if folded in banned_keys:
        raise RuntimeError(f"private lap-level field {path}.{key} reached catalog output")
      validate_private_data_absent(child, f"{path}.{key}")
  elif isinstance(value, list):
    for index, child in enumerate(value):
      validate_private_data_absent(child, f"{path}[{index}]")
  elif isinstance(value, str):
    folded = value.casefold()
    if (
      "authorization: bearer" in folded
      or "/api/v1/laps/" in folded
      or "https://" in folded
      or "http://" in folded
    ):
      raise RuntimeError(f"private lap-level value reached {path}")


def main() -> int:
  generator.configure_logging()
  args = parse_args()
  if args.csv_limit < 3 or args.csv_limit > LAP_BLOCK_SIZE:
    raise RuntimeError(f"--csv-limit must be between 3 and {LAP_BLOCK_SIZE}")
  if args.max_laps < args.csv_limit or args.max_laps > 1000:
    raise RuntimeError("--max-laps must be between --csv-limit and 1000")
  token = os.environ.get("GARAGE61_TOKEN")
  if not token:
    LOGGER.error("GARAGE61_TOKEN is required in the environment")
    return 2

  cars = resolve_cars(items(generator.request(token, "/cars"), "/cars"))
  tracks = resolve_tracks(items(generator.request(token, "/tracks"), "/tracks"))
  generated_at = generator.now_iso()
  scenarios: list[dict[str, Any]] = []

  for index, track in enumerate(tracks, start=1):
    LOGGER.info("Track %d/%d | %s", index, len(tracks), track_label(track))
    for car in cars:
      laps = fetch_laps(token, int(track["id"]), int(car["id"]))
      if not laps:
        continue
      scenarios.extend(
        analyze_combination(
          token,
          track,
          car,
          laps,
          args,
          generated_at,
        ),
      )

  if not scenarios:
    raise RuntimeError("No aggregate scenarios could be generated from visible telemetry")
  scenarios.sort(key=lambda scenario: (scenario["carName"], scenario["trackName"], scenario["id"]))
  catalog = {
    "catalogVersion": CATALOG_VERSION,
    "generatedAt": generated_at,
    "sourceProvider": "garage61",
    "carsRequested": [target.label for target in CAR_TARGETS],
    "scenarios": scenarios,
  }
  validate_private_data_absent(catalog)
  output = Path(args.output)
  output.parent.mkdir(parents=True, exist_ok=True)
  output.write_text(json.dumps(catalog, indent=2) + "\n", encoding="utf-8")
  LOGGER.info(
    "Complete | %d tracks | %d scenarios | %s",
    len(tracks),
    len(scenarios),
    output,
  )
  return 0


if __name__ == "__main__":
  try:
    raise SystemExit(main())
  except (RuntimeError, ValueError) as error:
    LOGGER.error("Import failed: %s", error)
    raise SystemExit(1)
