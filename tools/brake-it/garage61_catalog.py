#!/usr/bin/env python3
"""Build a privacy-screened Brake-It catalog from Garage 61 telemetry.

The access token is used only by this local developer tool. Raw telemetry and
driver identity stay in memory. The JSON written to disk contains aggregate
braking parameters and Garage61 links for the laps that informed each scenario.
"""

from __future__ import annotations

import argparse
import json
import logging
import math
import os
import re
import urllib.parse
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
  variants: tuple[str, ...] | None = None


CAR_TARGETS = (
  CarTarget("Mazda MX-5", ("mazda", "mx", "5"), ("Global Mazda MX-5 Cup", "Mazda MX-5 Cup")),
  CarTarget("BMW M4 GT3", ("bmw", "m4", "gt3"), ("BMW M4 GT3",)),
)

# Active extraction venues. Circuito de Navarra comes from the reviewed iRacing
# Included with Membership list; Road Atlanta and Spa are explicitly requested
# paid venues. Keep aliases exact so catalog changes cannot silently select
# another venue or layout with a similar name.
TRACK_TARGETS = (
  TrackTarget("Circuito de Navarra", ("Circuito de Navarra",), ("Speed Circuit",)),
  TrackTarget("Road Atlanta", ("Road Atlanta",), ("Full Course",)),
  TrackTarget(
    "Circuit de Spa-Francorchamps",
    ("Circuit de Spa-Francorchamps", "Spa"),
    ("Grand Prix Pits",),
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
  targets: tuple[TrackTarget, ...] = TRACK_TARGETS,
) -> list[dict[str, Any]]:
  """Select only configured road venues and layouts.

  Garage61 exposes every layout as a separate track record. Venue and optional
  layout names must match the configured allowlist exactly, and plainly
  non-road variants at a mixed venue (for example Charlotte's oval) are
  excluded.
  """
  iracing = [
    track for track in tracks
    if str(track.get("platform", "")).casefold() == "iracing"
  ]
  selected: dict[int, dict[str, Any]] = {}
  missing: list[str] = []
  for target in targets:
    exact_names = {normalized(name) for name in target.names}
    exact_variants = (
      {normalized(variant) for variant in target.variants}
      if target.variants is not None
      else None
    )
    matches = [
      track
      for track in iracing
      if normalized(str(track.get("name", ""))) in exact_names
      and (
        exact_variants is None
        or normalized(str(track.get("variant", ""))) in exact_variants
      )
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
    available = ", ".join(sorted(track_label(track) for track in iracing))
    raise RuntimeError(
      "Garage 61 did not contain the requested road venue/layout(s): "
      + ", ".join(missing)
      + f". Available iRacing tracks: {available}",
    )
  return sorted(selected.values(), key=track_sort_key)


def car_track_combinations(
  cars: list[dict[str, Any]],
  tracks: list[dict[str, Any]],
) -> list[tuple[dict[str, Any], dict[str, Any]]]:
  """Build every configured pair; generate_combinations processes them serially."""
  return [(track, car) for track in tracks for car in cars]


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
    "sourceLaps": [sanitize_lap_citation(lap) for lap in source.get("sourceLaps", [])],
  }
  return scenario


def sanitize_lap_citation(lap: dict[str, Any]) -> dict[str, Any]:
  """Keep useful Garage61 provenance without copying driver or API details."""
  lap_id = str(lap.get("lapId", "")).strip()
  if not lap_id:
    raise RuntimeError("Garage61 source lap has no lapId")
  encoded_id = urllib.parse.quote(lap_id, safe="")
  lap_url = f"https://garage61.net/app/analyze;t={encoded_id}"
  contribution = lap.get("contribution")
  if not isinstance(contribution, dict):
    raise RuntimeError(f"Garage61 source lap {lap_id} has no contribution")
  return {
    "lapId": lap_id,
    "garage61Url": lap_url,
    "garage61AnalyzeUrl": lap_url,
    "lapTimeSec": float(lap["lapTimeSec"]),
    "contribution": {
      key: value
      for key, value in contribution.items()
      if key in {
        "zone",
        "weight",
        "eventStartLapPercent",
        "eventThresholdEndLapPercent",
        "eventTrailEndLapPercent",
        "eventAccelerationStartLapPercent",
        "telemetryWindowStartLapPercent",
        "telemetryWindowEndLapPercent",
        "peakBrakePercent",
        "clusterDistanceLapPercent",
      }
      and isinstance(value, (int, float))
    },
  }


def valid_citation_url(value: str) -> bool:
  parsed = urllib.parse.urlparse(value)
  return (
    parsed.scheme == "https"
    and parsed.netloc == "garage61.net"
    and (
      parsed.path.startswith("/app/analyze")
      or parsed.path.startswith("/app/analysis/laps/")
    )
  )


def validate_private_data_absent(value: Any, path: str = "catalog") -> None:
  banned_keys = {
    "driver",
    "drivers",
    "driverid",
    "driverslug",
    "laps",
    "samples",
    "telemetry",
    "rows",
    "csv",
    "raw",
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
      if folded in {
        "garage61url",
        "garage61telemetryurl",
        "garage61analysisurl",
        "garage61analyzeurl",
      }:
        if not isinstance(child, str) or not valid_citation_url(child):
          raise RuntimeError(f"invalid Garage61 citation URL at {path}.{key}")
        continue
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


def unique_scenarios(scenarios: list[dict[str, Any]]) -> list[dict[str, Any]]:
  """Return one scenario per stable ID, preserving the first completed copy."""
  unique: dict[str, dict[str, Any]] = {}
  for scenario in scenarios:
    scenario_id = str(scenario.get("id", "")).strip()
    if not scenario_id:
      raise RuntimeError("Brake-It catalog scenario has no id")
    unique.setdefault(scenario_id, scenario)
  return list(unique.values())


def load_existing_scenarios(output: Path) -> list[dict[str, Any]]:
  """Load a prior valid checkpoint so completed combinations can be skipped."""
  if not output.exists():
    return []
  try:
    catalog = json.loads(output.read_text(encoding="utf-8"))
  except (OSError, json.JSONDecodeError) as error:
    raise RuntimeError(f"Existing Brake-It catalog {output} is not valid JSON") from error
  if catalog.get("catalogVersion") != CATALOG_VERSION:
    raise RuntimeError(
      f"Existing Brake-It catalog version is {catalog.get('catalogVersion')}, "
      f"want {CATALOG_VERSION}",
    )
  if catalog.get("sourceProvider") != "garage61":
    raise RuntimeError("Existing Brake-It catalog is not a Garage61 catalog")
  scenarios = catalog.get("scenarios")
  if not isinstance(scenarios, list):
    raise RuntimeError("Existing Brake-It catalog has no scenarios array")
  validate_private_data_absent(catalog)
  loaded = unique_scenarios(scenarios)
  LOGGER.info("Resume | %d existing scenarios | %s", len(loaded), output)
  return loaded


def combination_exists(
  scenarios: list[dict[str, Any]],
  track: dict[str, Any],
  car: dict[str, Any],
) -> bool:
  car_name = str(car["name"])
  track_name = track_label(track)
  matches = [
    scenario
    for scenario in scenarios
    if scenario.get("carName") == car_name and scenario.get("trackName") == track_name
  ]
  if not matches:
    return False
  for scenario in matches:
    source = scenario.get("source")
    if not isinstance(source, dict):
      return False
    citations = source.get("sourceLaps")
    if not isinstance(citations, list) or not citations:
      return False
  return True


def write_catalog_checkpoint(
  output: Path,
  generated_at: str,
  scenarios: list[dict[str, Any]],
) -> None:
  """Atomically persist every scenario completed so far.

  Raw telemetry and driver identity remain in memory. The checkpoint contains
  only screened scenarios that have already passed the privacy validator.
  """
  ordered = sorted(
    unique_scenarios(scenarios),
    key=lambda scenario: (scenario["carName"], scenario["trackName"], scenario["id"]),
  )
  catalog = {
    "catalogVersion": CATALOG_VERSION,
    "generatedAt": generated_at,
    "sourceProvider": "garage61",
    "carsRequested": [target.label for target in CAR_TARGETS],
    "scenarios": ordered,
  }
  validate_private_data_absent(catalog)
  output.parent.mkdir(parents=True, exist_ok=True)
  temporary = output.with_name(f".{output.name}.tmp")
  try:
    with temporary.open("w", encoding="utf-8") as checkpoint:
      json.dump(catalog, checkpoint, indent=2)
      checkpoint.write("\n")
      checkpoint.flush()
      os.fsync(checkpoint.fileno())
    temporary.replace(output)
  finally:
    temporary.unlink(missing_ok=True)
  LOGGER.info("Checkpoint | %d scenarios | %s", len(ordered), output)


def generate_combinations(
  token: str,
  combinations: list[tuple[dict[str, Any], dict[str, Any]]],
  args: argparse.Namespace,
  generated_at: str,
  output: Path,
  existing_scenarios: list[dict[str, Any]] | None = None,
) -> list[dict[str, Any]]:
  """Process one pair at a time and checkpoint it before starting the next."""
  scenarios = unique_scenarios(list(existing_scenarios or []))
  for index, (track, car) in enumerate(combinations, start=1):
    LOGGER.info(
      "Combination %d/%d | %s | %s",
      index,
      len(combinations),
      track_label(track),
      car["name"],
    )
    if combination_exists(scenarios, track, car):
      LOGGER.info("Existing scenarios found; skipping Garage61 lap downloads")
      continue
    laps = fetch_laps(token, int(track["id"]), int(car["id"]))
    if not laps:
      continue
    generated = analyze_combination(
      token,
      track,
      car,
      laps,
      args,
      generated_at,
    )
    if not generated:
      continue
    car_name = str(car["name"])
    track_name = track_label(track)
    scenarios = [
      scenario
      for scenario in scenarios
      if scenario.get("carName") != car_name or scenario.get("trackName") != track_name
    ]
    scenarios = unique_scenarios([*scenarios, *generated])
    write_catalog_checkpoint(output, generated_at, scenarios)
  return scenarios


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
  combinations = car_track_combinations(cars, tracks)
  output = Path(args.output)
  existing_scenarios = load_existing_scenarios(output)
  scenarios = generate_combinations(
    token,
    combinations,
    args,
    generated_at,
    output,
    existing_scenarios,
  )

  if not scenarios:
    raise RuntimeError("No aggregate scenarios could be generated from visible telemetry")
  LOGGER.info(
    "Complete | %d combination | %d scenarios | %s",
    len(combinations),
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
