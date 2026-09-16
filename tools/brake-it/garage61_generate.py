#!/usr/bin/env python3
"""Generate static Brake-It scenarios from Garage 61 lap telemetry."""

from __future__ import annotations

import argparse
import csv
import io
import json
import logging
import math
import os
import random
import statistics
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

try:
  import certifi
except ImportError:
  certifi = None

try:
  from rich.logging import RichHandler
except ImportError:
  RichHandler = None

GENERATOR_VERSION = "0.2.0"
API_BASE_URL = "https://garage61.net/api/v1"
APP_BASE_URL = "https://garage61.net/app"
ANALYZE_WEB_BASE_URL = f"{APP_BASE_URL}/analyze"
ANALYSIS_WEB_BASE_URL = f"{APP_BASE_URL}/analysis/laps"
DEFAULT_TELEMETRY_VIEW = "driving-style"
METHOD = "brake-event clustering by lap-distance percentage"
DEFAULT_REQUEST_INTERVAL_SECONDS = 1.0
REQUEST_INTERVAL_ENV = "GARAGE61_REQUEST_INTERVAL_SECONDS"
DEFAULT_RETRY_JITTER_SECONDS = 1.0
RETRY_JITTER_ENV = "GARAGE61_RETRY_JITTER_SECONDS"
LOGGER = logging.getLogger("brake_it.garage61")
SSL_CONTEXT = ssl.create_default_context(
  cafile=certifi.where() if certifi is not None else None,
)
_last_request_started_at: float | None = None
_rate_limit_not_before: float | None = None


@dataclass(frozen=True)
class FetchResponse:
  status: int
  body: str
  headers: dict[str, str]


@dataclass
class TelemetryRow:
  speed: float
  lap_pct: float
  brake: float
  throttle: float
  position_type: int


@dataclass
class LapEvent:
  lap: dict[str, Any]
  start_pct: float
  threshold_end_pct: float
  trail_end_pct: float
  acceleration_start_pct: float
  peak_brake_pct: float
  brake_rise_ms: float
  accelerator_fall_ms: float
  hold_ms: float
  trail_release_ms: float
  transition_ms: float
  transition_brake_pct: float
  accelerator_ramp_ms: float | None
  entry_speed: float
  min_speed: float


@dataclass
class Cluster:
  center: float
  events: list[LapEvent]


def configure_logging() -> None:
  """Configure colorful CLI logs while keeping logging calls standard."""
  app_logger = logging.getLogger("brake_it")
  if app_logger.handlers:
    return

  if RichHandler is not None:
    handler = RichHandler(
      show_path=False,
      rich_tracebacks=True,
      markup=False,
    )
    handler.setFormatter(logging.Formatter("%(message)s"))
  else:
    handler = logging.StreamHandler()
    handler.setFormatter(
      logging.Formatter(
        "%(asctime)s | %(levelname)-8s | %(message)s",
        datefmt="%H:%M:%S",
      ),
    )
  app_logger.addHandler(handler)
  app_logger.setLevel(logging.INFO)
  app_logger.propagate = False
  if RichHandler is None:
    app_logger.warning(
      "Rich is not installed; using plain logs. Install tools/brake-it/requirements.txt for color.",
    )


def request_log_message(method: str, path: str, status: int, elapsed: float) -> str:
  return f"Garage61 {method.upper()} {path} -> HTTP {status} | {elapsed:.1f}s"


def parse_args() -> argparse.Namespace:
  parser = argparse.ArgumentParser(
    description="Generate Brake-It scenario JSON from Garage 61 telemetry.",
  )
  parser.add_argument("--track-id", type=int, required=True)
  parser.add_argument("--car-id", type=int, required=True)
  parser.add_argument("--output", default="src/data/generatedScenarios.json")
  parser.add_argument("--name-prefix", default=None)
  parser.add_argument("--max-laps", type=int, default=100)
  parser.add_argument("--csv-limit", type=int, default=100)
  parser.add_argument("--min-coverage", type=float, default=0.35)
  parser.add_argument("--cluster-tolerance", type=float, default=0.018)
  parser.add_argument("--approach-ms", type=int, default=2200)
  parser.add_argument("--team", action="append", default=[])
  parser.add_argument("--driver", action="append", default=[])
  parser.add_argument("--min-rating", type=int)
  parser.add_argument("--max-rating", type=int)
  parser.add_argument("--age", type=int)
  parser.add_argument("--session-types", default=None)
  parser.add_argument("--fixed-setup", action="store_true")
  parser.add_argument("--open-setup", action="store_true")
  parser.add_argument("--allow-unclean", action="store_true")
  parser.add_argument(
    "--garage61-analysis-map",
    default=None,
    help=(
      "JSON file mapping Garage 61 lap IDs to existing lap-analysis IDs. "
      "Values may be strings or objects with analysisId/garage61AnalysisId/id."
    ),
  )
  parser.add_argument(
    "--garage61-telemetry-view",
    default=DEFAULT_TELEMETRY_VIEW,
    help="Garage 61 analysis view matrix parameter used for citation links.",
  )
  return parser.parse_args()


def now_iso() -> str:
  return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace(
    "+00:00",
    "Z",
  )


def request(
  token: str,
  path: str,
  params: dict[str, Any] | None = None,
  *,
  expect_text: bool = False,
  method: str = "GET",
  body: dict[str, Any] | None = None,
) -> Any:
  query = {}
  for key, value in (params or {}).items():
    if value is None or value == []:
      continue
    if isinstance(value, (list, tuple)):
      query[key] = ",".join(str(item) for item in value)
    else:
      query[key] = str(value)

  url = f"{API_BASE_URL}{path}"
  if query:
    url = f"{url}?{urllib.parse.urlencode(query)}"

  for _attempt in range(8):
    throttle_request()
    request_started_at = time.perf_counter()
    try:
      try:
        response = urllib_fetch(url, token, method=method, body=body)
      except urllib.error.HTTPError as error:
        response = FetchResponse(
          status=error.code,
          body=error.read().decode("utf-8", errors="replace"),
          headers=normalized_headers(error.headers),
        )
      except urllib.error.URLError as error:
        if "CERTIFICATE_VERIFY_FAILED" not in str(error):
          raise
        response = curl_fetch(url, token, method=method, body=body)
    except Exception as error:
      elapsed = time.perf_counter() - request_started_at
      LOGGER.error(
        "Garage61 %s %s failed | %.1fs | %s",
        method.upper(),
        path,
        elapsed,
        error,
      )
      raise

    elapsed = time.perf_counter() - request_started_at
    LOGGER.info(request_log_message(method, path, response.status, elapsed))

    retry_after = response_retry_seconds(response)
    if retry_after is not None:
      delay = defer_for_rate_limit(retry_after)
      outcome = "rate limited" if response.status == 429 else "capacity exhausted"
      LOGGER.warning(
        "Garage61 %s; delaying the next request %.1fs",
        outcome,
        delay,
      )

    if response.status == 429:
      continue
    if expect_text and response.status in {401, 403, 404}:
      return None
    if response.status >= 400:
      raise RuntimeError(
        f"Garage 61 API error {response.status}: {response.body[:500]}",
      )
    return response.body if expect_text else json.loads(response.body)

  raise RuntimeError(f"Garage 61 API rate limit did not clear for {url}")


def request_interval_seconds() -> float:
  return non_negative_env_float(
    REQUEST_INTERVAL_ENV,
    DEFAULT_REQUEST_INTERVAL_SECONDS,
  )


def retry_jitter_seconds() -> float:
  return non_negative_env_float(RETRY_JITTER_ENV, DEFAULT_RETRY_JITTER_SECONDS)


def non_negative_env_float(name: str, default: float) -> float:
  raw = os.environ.get(name, str(default))
  try:
    value = float(raw)
  except ValueError as error:
    raise RuntimeError(f"{name} must be a number") from error
  if not math.isfinite(value) or value < 0:
    raise RuntimeError(f"{name} must be a finite non-negative number")
  return value


def throttle_request() -> None:
  """Wait for both the local interval and any server-requested delay."""
  global _last_request_started_at, _rate_limit_not_before

  interval = request_interval_seconds()
  now = time.monotonic()
  not_before = _rate_limit_not_before or 0.0
  if _last_request_started_at is not None:
    not_before = max(not_before, _last_request_started_at + interval)
  wait_seconds = not_before - now
  if wait_seconds > 0:
    time.sleep(wait_seconds)
    now += wait_seconds
  _last_request_started_at = now
  if _rate_limit_not_before is not None and now >= _rate_limit_not_before:
    _rate_limit_not_before = None


def defer_for_rate_limit(retry_after_seconds: float) -> float:
  """Prevent the next request until Retry-After plus random jitter has passed."""
  global _rate_limit_not_before

  jitter = random.uniform(0.0, retry_jitter_seconds())
  delay = retry_after_seconds + jitter
  not_before = time.monotonic() + delay
  _rate_limit_not_before = max(_rate_limit_not_before or 0.0, not_before)
  return delay


def urllib_fetch(
  url: str,
  token: str,
  *,
  method: str = "GET",
  body: dict[str, Any] | None = None,
) -> FetchResponse:
  headers = {"Authorization": f"Bearer {token}"}
  data = None
  if body is not None:
    headers["Content-Type"] = "application/json"
    data = json.dumps(body).encode("utf-8")
  req = urllib.request.Request(url, data=data, headers=headers, method=method)
  with urllib.request.urlopen(req, timeout=60, context=SSL_CONTEXT) as response:
    return FetchResponse(
      status=response.status,
      body=response.read().decode("utf-8"),
      headers=normalized_headers(response.headers),
    )


def curl_fetch(
  url: str,
  token: str,
  *,
  method: str = "GET",
  body: dict[str, Any] | None = None,
) -> FetchResponse:
  with tempfile.TemporaryDirectory() as temp_dir:
    header_path = Path(temp_dir) / "headers"
    command = [
      "curl",
      "-L",
      "-sS",
      "--dump-header",
      str(header_path),
      "-w",
      "\nHTTP_STATUS:%{http_code}",
      "-H",
      f"Authorization: Bearer {token}",
    ]
    if method != "GET":
      command.extend(["-X", method])
    if body is not None:
      command.extend(
        [
          "-H",
          "Content-Type: application/json",
          "--data-binary",
          json.dumps(body),
        ],
      )
    command.append(url)
    result = subprocess.run(
      command,
      check=False,
      capture_output=True,
      text=True,
    )
    if result.returncode != 0:
      raise RuntimeError(f"curl failed: {result.stderr.strip()}")
    header_text = header_path.read_text(encoding="iso-8859-1")
  response_body, _, status_text = result.stdout.rpartition("\nHTTP_STATUS:")
  try:
    return FetchResponse(
      status=int(status_text.strip()),
      body=response_body,
      headers=parse_curl_headers(header_text),
    )
  except ValueError as error:
    raise RuntimeError("curl response did not include an HTTP status") from error


def normalized_headers(headers: Any) -> dict[str, str]:
  if headers is None:
    return {}
  return {str(key).casefold(): str(value).strip() for key, value in headers.items()}


def parse_curl_headers(text: str) -> dict[str, str]:
  """Return headers from curl's final response, ignoring redirect blocks."""
  headers: dict[str, str] = {}
  for line in text.replace("\r\n", "\n").split("\n"):
    if line.startswith("HTTP/"):
      headers = {}
      continue
    if ":" not in line:
      continue
    key, value = line.split(":", 1)
    headers[key.casefold()] = value.strip()
  return headers


def response_retry_seconds(response: FetchResponse) -> float | None:
  candidates: list[float] = []
  header_value = response.headers.get("retry-after")
  if header_value is not None:
    try:
      parsed = float(header_value)
      if math.isfinite(parsed) and parsed >= 0:
        candidates.append(parsed)
    except ValueError:
      pass
  if response.status == 429:
    body_value = retry_seconds_from_body(response.body)
    if body_value is not None:
      candidates.append(body_value)
    if not candidates:
      candidates.append(10.0)
  return max(candidates) if candidates else None


def retry_seconds_from_body(body: str) -> float | None:
  try:
    data = json.loads(body)
    value = data.get("details", {}).get("retryAfterSeconds")
    seconds = float(value)
    return seconds if math.isfinite(seconds) and seconds >= 0 else None
  except (AttributeError, TypeError, ValueError, json.JSONDecodeError):
    return None


def load_csv_rows(csv_text: str) -> list[TelemetryRow]:
  rows: list[TelemetryRow] = []
  reader = csv.DictReader(io.StringIO(csv_text))
  required = {"Speed", "LapDistPct", "Brake", "Throttle", "PositionType"}
  missing = required - set(reader.fieldnames or [])
  if missing:
    raise ValueError(f"Telemetry CSV missing columns: {', '.join(sorted(missing))}")

  for row in reader:
    try:
      rows.append(
        TelemetryRow(
          speed=float(row["Speed"]),
          lap_pct=float(row["LapDistPct"]),
          brake=float(row["Brake"]),
          throttle=float(row["Throttle"]),
          position_type=int(float(row["PositionType"])),
        ),
      )
    except (TypeError, ValueError):
      continue

  return [row for row in rows if math.isfinite(row.lap_pct)]


def moving_average(values: list[float], radius: int = 3) -> list[float]:
  averaged: list[float] = []
  for index in range(len(values)):
    start = max(0, index - radius)
    end = min(len(values), index + radius + 1)
    averaged.append(sum(values[start:end]) / (end - start))
  return averaged


def median(values: list[float]) -> float | None:
  finite = sorted(value for value in values if math.isfinite(value))
  return statistics.median(finite) if finite else None


def percentile(values: list[float], percentile_value: float) -> float | None:
  finite = sorted(value for value in values if math.isfinite(value))
  if not finite:
    return None
  if len(finite) == 1:
    return finite[0]

  rank = (len(finite) - 1) * clamp(percentile_value, 0, 1)
  lower_index = math.floor(rank)
  upper_index = math.ceil(rank)
  if lower_index == upper_index:
    return finite[lower_index]

  fraction = rank - lower_index
  return finite[lower_index] + (
    finite[upper_index] - finite[lower_index]
  ) * fraction


def mad(values: list[float], center: float | None) -> float | None:
  if center is None:
    return None
  return median([abs(value - center) for value in values])


def clamp(value: float, low: float, high: float) -> float:
  return min(high, max(low, value))


def garage61_zoom_param(start_pct: float, end_pct: float) -> str:
  start = round(clamp(start_pct, 0, 1) * 10000)
  end = round(clamp(end_pct, 0, 1) * 10000)
  if end <= start:
    end = min(10000, start + 1)
  return f"{start}-{end}"


def garage61_analysis_url(
  analysis_id: str,
  start_pct: float,
  end_pct: float,
  telemetry_view: str,
) -> str:
  encoded_view = urllib.parse.quote(telemetry_view, safe="")
  encoded_id = urllib.parse.quote(analysis_id, safe="")
  zoom = garage61_zoom_param(start_pct, end_pct)
  return f"{ANALYSIS_WEB_BASE_URL}/{encoded_id};v={encoded_view};z={zoom}"


def garage61_analyze_lap_url(lap_id: str) -> str:
  encoded_id = urllib.parse.quote(lap_id, safe="")
  return f"{ANALYZE_WEB_BASE_URL};t={encoded_id}"


def load_analysis_map(path: str | None) -> dict[str, str]:
  if not path:
    return {}

  data = json.loads(Path(path).read_text(encoding="utf-8"))
  mapping: dict[str, str] = {}

  def analysis_id(value: Any) -> str | None:
    if isinstance(value, str):
      return value
    if isinstance(value, dict):
      for key in ("analysisId", "garage61AnalysisId", "id"):
        found = value.get(key)
        if isinstance(found, str) and found:
          return found
    return None

  if isinstance(data, dict):
    records = data.get("laps")
    if isinstance(records, list):
      for item in records:
        if not isinstance(item, dict):
          continue
        lap_id = item.get("lapId") or item.get("lap") or item.get("id")
        found = item.get("analysisId") or item.get("garage61AnalysisId")
        if isinstance(lap_id, str) and isinstance(found, str) and found:
          mapping[lap_id] = found
      return mapping

    for lap_id, value in data.items():
      found = analysis_id(value)
      if found:
        mapping[lap_id] = found

  return mapping


def first_at_or_after(
  values: list[float],
  start: int,
  end: int,
  predicate: Callable[[float, int], bool],
) -> int | None:
  for index in range(max(0, start), min(len(values), end)):
    if predicate(values[index], index):
      return index
  return None


def circular_distance(a: float, b: float) -> float:
  direct = abs(a - b)
  return min(direct, 1 - direct)


def detect_events(lap: dict[str, Any], rows: list[TelemetryRow]) -> list[LapEvent]:
  lap_time = float(lap["lapTime"])
  dt = lap_time / max(1, len(rows) - 1)
  brake = moving_average([row.brake for row in rows])
  throttle = moving_average([row.throttle for row in rows])
  brake_threshold = 0.04
  merge_gap = max(3, round(0.16 / dt))
  min_samples = max(6, round(0.28 / dt))
  windows: list[tuple[int, int]] = []
  start: int | None = None
  last_active: int | None = None

  for index, value in enumerate(brake):
    active = value > brake_threshold and rows[index].position_type == 3
    if active:
      if start is None:
        start = index
      last_active = index
    elif start is not None and last_active is not None and index - last_active > merge_gap:
      if last_active - start >= min_samples:
        windows.append((start, last_active))
      start = None
      last_active = None

  if start is not None and last_active is not None and last_active - start >= min_samples:
    windows.append((start, last_active))

  events: list[LapEvent] = []
  for start_index, end_index in windows:
    peak_index = max(range(start_index, end_index + 1), key=lambda index: brake[index])
    peak = brake[peak_index]
    if peak < 0.18:
      continue

    rise_end = first_at_or_after(
      brake,
      start_index,
      end_index + 1,
      lambda value, _index: value >= peak * 0.9,
    )
    if rise_end is None:
      rise_end = peak_index

    threshold_end = first_at_or_after(
      brake,
      rise_end,
      end_index + 1,
      lambda value, index: index > peak_index and value < max(0.12, peak * 0.82),
    )
    if threshold_end is None:
      threshold_end = peak_index

    lookback = max(0, start_index - round(1.5 / dt))
    fall_start = start_index
    fall_scan_end = min(len(throttle) - 1, start_index + round(0.25 / dt))
    for index in range(lookback + 1, fall_scan_end + 1):
      if throttle[index - 1] >= 0.92 and throttle[index] < 0.92:
        fall_start = index - 1
        break

    fall_end = first_at_or_after(
      throttle,
      fall_start,
      min(len(throttle), start_index + round(1.5 / dt)),
      lambda value, _index: value <= 0.12,
    )
    if fall_end is None:
      fall_end = start_index

    acceleration_start = first_at_or_after(
      throttle,
      end_index,
      len(throttle),
      lambda value, _index: value >= 0.12,
    )
    if acceleration_start is None:
      acceleration_start = end_index

    full_throttle = first_at_or_after(
      throttle,
      acceleration_start,
      len(throttle),
      lambda value, _index: value >= 0.95,
    )

    transition_brake = 0.0
    if acceleration_start > end_index:
      transition_brake = median(
        [value * 100 for value in brake[end_index:acceleration_start]],
      ) or 0.0

    events.append(
      LapEvent(
        lap=lap,
        start_pct=rows[start_index].lap_pct,
        threshold_end_pct=rows[threshold_end].lap_pct,
        trail_end_pct=rows[end_index].lap_pct,
        acceleration_start_pct=rows[acceleration_start].lap_pct,
        peak_brake_pct=peak * 100,
        brake_rise_ms=max(0, (rise_end - start_index) * dt * 1000),
        accelerator_fall_ms=max(0, (fall_end - fall_start) * dt * 1000),
        hold_ms=max(0, (threshold_end - max(rise_end, fall_end)) * dt * 1000),
        trail_release_ms=max(0, (end_index - threshold_end) * dt * 1000),
        transition_ms=max(0, (acceleration_start - end_index) * dt * 1000),
        transition_brake_pct=transition_brake,
        accelerator_ramp_ms=None
        if full_throttle is None
        else max(0, (full_throttle - acceleration_start) * dt * 1000),
        entry_speed=rows[start_index].speed,
        min_speed=min(row.speed for row in rows[start_index : end_index + 1]),
      ),
    )

  return events


def cluster_events(events: list[LapEvent], tolerance: float) -> list[Cluster]:
  clusters: list[Cluster] = []
  for event in sorted(events, key=lambda item: item.start_pct):
    match = next(
      (
        cluster
        for cluster in clusters
        if circular_distance(cluster.center, event.start_pct) <= tolerance
      ),
      None,
    )
    if match is None:
      clusters.append(Cluster(center=event.start_pct, events=[event]))
    else:
      match.events.append(event)
      match.center = median([item.start_pct for item in match.events]) or match.center
  return clusters


def driver_name(driver: dict[str, Any] | None) -> str | None:
  if not driver:
    return None
  name = " ".join(
    part for part in [driver.get("firstName"), driver.get("lastName")] if part
  ).strip()
  return name or driver.get("slug")


def entity(info: dict[str, Any]) -> dict[str, Any]:
  return {
    "id": info.get("id"),
    "name": info.get("name"),
    **({"variant": info["variant"]} if info.get("variant") else {}),
    **({"platform": info["platform"]} if info.get("platform") else {}),
    **({"platformId": info["platform_id"]} if info.get("platform_id") else {}),
  }


def source_car_entity(
  args: argparse.Namespace,
  token: str,
  fallback_lap: dict[str, Any],
) -> dict[str, Any]:
  if args.car_id >= 0:
    car = entity(fallback_lap["car"])
    car["kind"] = "car"
    return car

  group_id = abs(args.car_id)
  car_groups = request(token, "/car-groups")
  groups = car_groups if isinstance(car_groups, list) else car_groups.get("items", [])
  for group in groups:
    if group.get("id") == group_id:
      return {
        "id": group["id"],
        "name": group["name"],
        **({"platform": group["platform"]} if group.get("platform") else {}),
        "kind": "carGroup",
      }

  car = entity(fallback_lap["car"])
  car["id"] = args.car_id
  car["name"] = f"Car group {group_id}"
  car["kind"] = "carGroup"
  return car


def citation_for(
  event: LapEvent,
  zone: int,
  center: float,
  analysis_id: str | None,
  telemetry_view: str,
) -> dict[str, Any]:
  lap = event.lap
  driver = lap.get("driver") or {}
  name = driver_name(driver)
  window_start = event.start_pct
  window_end = max(
    event.threshold_end_pct,
    event.trail_end_pct,
    event.acceleration_start_pct,
  )
  telemetry_url = (
    garage61_analysis_url(analysis_id, window_start, window_end, telemetry_view)
    if analysis_id
    else None
  )
  analysis_url = (
    garage61_analysis_url(analysis_id, 0, 1, telemetry_view) if analysis_id else None
  )
  analyze_url = garage61_analyze_lap_url(lap["id"])
  return {
    "lapId": lap["id"],
    **({"garage61AnalysisId": analysis_id} if analysis_id else {}),
    "garage61Url": telemetry_url or analyze_url,
    **({"garage61TelemetryUrl": telemetry_url} if telemetry_url else {}),
    **({"garage61AnalysisUrl": analysis_url} if analysis_url else {}),
    "garage61AnalyzeUrl": analyze_url,
    "apiLapUrl": f"{API_BASE_URL}/laps/{lap['id']}",
    "apiCsvUrl": f"{API_BASE_URL}/laps/{lap['id']}/csv",
    "driver": {
      **({"id": driver["id"]} if driver.get("id") else {}),
      **({"slug": driver["slug"]} if driver.get("slug") else {}),
      **({"name": name} if name else {}),
    },
    "car": {**entity(lap["car"]), "kind": "car"},
    "lapTimeSec": round(float(lap["lapTime"]), 6),
    **({"driverRating": lap["driverRating"]} if lap.get("driverRating") is not None else {}),
    **({"startTime": lap["startTime"]} if lap.get("startTime") else {}),
    **({"eventId": lap["event"]} if lap.get("event") else {}),
    "contribution": {
      "zone": zone,
      "weight": 1.0,
      "eventStartLapPercent": round(event.start_pct * 100, 4),
      "eventThresholdEndLapPercent": round(event.threshold_end_pct * 100, 4),
      "eventTrailEndLapPercent": round(event.trail_end_pct * 100, 4),
      "eventAccelerationStartLapPercent": round(event.acceleration_start_pct * 100, 4),
      "telemetryWindowStartLapPercent": round(window_start * 100, 4),
      "telemetryWindowEndLapPercent": round(window_end * 100, 4),
      "peakBrakePercent": round(event.peak_brake_pct, 3),
      "clusterDistanceLapPercent": round(circular_distance(center, event.start_pct) * 100, 4),
    },
  }


def best_event_per_lap(cluster: Cluster) -> list[LapEvent]:
  by_lap: dict[str, LapEvent] = {}
  for event in cluster.events:
    lap_id = event.lap["id"]
    current = by_lap.get(lap_id)
    if current is None or circular_distance(cluster.center, event.start_pct) < circular_distance(
      cluster.center,
      current.start_pct,
    ):
      by_lap[lap_id] = event
  return sorted(by_lap.values(), key=lambda event: float(event.lap["lapTime"]))


def scenario_id_for(track: dict[str, Any], car: dict[str, Any], zone: int) -> str:
  car_id = car["id"]
  car_slug = (
    f"car-group-{car_id}" if car.get("kind") == "carGroup" else f"car-{car_id}"
  )
  return (
    f"garage61-{track.get('platform', 'sim')}-track-{track['id']}"
    f"-{car_slug}-zone-{zone}"
  )


def make_scenario(
  cluster: Cluster,
  zone: int,
  lap_count: int,
  query: dict[str, Any],
  generated_at: str,
  approach_ms: int,
  name_prefix: str,
  analysis_map: dict[str, str],
  telemetry_view: str,
  source_car: dict[str, Any] | None = None,
) -> dict[str, Any]:
  contributors = best_event_per_lap(cluster)

  def values(attr: str) -> list[float]:
    return [
      float(getattr(event, attr))
      for event in contributors
      if getattr(event, attr) is not None
    ]

  peak_brake_values = values("peak_brake_pct")
  target_brake = median(peak_brake_values) or 75
  target_brake_p10 = percentile(peak_brake_values, 0.1) or target_brake
  target_brake_p90 = percentile(peak_brake_values, 0.9) or target_brake
  target_variation = max(
    abs(target_brake - target_brake_p10),
    abs(target_brake_p90 - target_brake),
  )
  target_mad = mad(peak_brake_values, target_brake) or 4
  transition_ms = median(values("transition_ms")) or 0
  transition_brake = median(values("transition_brake_pct")) or 0
  accelerator_ramp = median(values("accelerator_ramp_ms")) or 1700
  transition_mode = "low-brake" if transition_ms >= 250 and transition_brake >= 2 else "coast"
  transition_enabled = transition_ms >= 250
  track = entity(contributors[0].lap["track"])
  track["kind"] = "track"
  car = source_car or {**entity(contributors[0].lap["car"]), "kind": "car"}
  scenario_id = scenario_id_for(track, car, zone)
  source_laps = [
    citation_for(
      event,
      zone,
      cluster.center,
      analysis_map.get(event.lap["id"]),
      telemetry_view,
    )
    for event in contributors
  ]
  start_pct = median(values("start_pct")) or cluster.center
  threshold_pct = median(values("threshold_end_pct")) or start_pct
  trail_pct = median(values("trail_end_pct")) or threshold_pct
  accel_pct = median(values("acceleration_start_pct")) or trail_pct

  return {
    "id": scenario_id,
    "name": f"{name_prefix} braking zone {zone}",
    "description": (
      f"Generated from {len(source_laps)} Garage 61 laps. "
      f"Start {start_pct * 100:.2f}% lap, trail ends {trail_pct * 100:.2f}% lap."
    ),
    "approachMs": approach_ms,
    "acceleratorFallTargetMs": round(
      clamp(median(values("accelerator_fall_ms")) or 280, 80, 1600),
    ),
    "brakeRiseTargetMs": round(clamp(median(values("brake_rise_ms")) or 420, 120, 2200)),
    "targetBrakePercent": round(clamp(target_brake, 20, 100)),
    "brakeTolerancePercent": round(clamp(target_variation, 4, 35)),
    "brakeHoldMs": round(clamp(median(values("hold_ms")) or 500, 250, 3500)),
    "trailBrakeReleaseMs": round(
      clamp(median(values("trail_release_ms")) or 1200, 350, 4500),
    ),
    "transitionEnabled": transition_enabled,
    "transitionMode": transition_mode,
    "transitionDurationMs": round(clamp(transition_ms or 600, 150, 2500)),
    "transitionBrakePercent": round(clamp(transition_brake or 5, 1, 25)),
    "acceleratorRampMs": round(clamp(accelerator_ramp, 400, 4500)),
    "createdAt": generated_at,
    "updatedAt": generated_at,
    "source": {
      "provider": "garage61",
      "generatedAt": generated_at,
      "generatorVersion": GENERATOR_VERSION,
      "method": METHOD,
      "track": track,
      "car": car,
      "query": query,
      "model": {
        "zone": zone,
        "observations": len(cluster.events),
        "sourceLapCount": len(source_laps),
        "lapCoveragePercent": round((len(source_laps) / lap_count) * 100, 2),
        "garage61TelemetryView": telemetry_view,
        "clusterCenterLapPercent": round(cluster.center * 100, 4),
        "startLapPercent": round(start_pct * 100, 4),
        "thresholdEndLapPercent": round(threshold_pct * 100, 4),
        "trailEndLapPercent": round(trail_pct * 100, 4),
        "accelerationStartLapPercent": round(accel_pct * 100, 4),
        "targetBrakeMedianPercent": round(target_brake, 3),
        "targetBrakeP10Percent": round(target_brake_p10, 3),
        "targetBrakeP90Percent": round(target_brake_p90, 3),
        "targetBrakeVariationPercent": round(target_variation, 3),
        "targetBrakeMadPercent": round(target_mad, 3),
        "brakeToleranceBasis": "Garage 61 peak-brake p10-p90 envelope",
        "entrySpeedMedian": round(median(values("entry_speed")) or 0, 3),
        "minSpeedMedian": round(median(values("min_speed")) or 0, 3),
      },
      "sourceLaps": source_laps,
    },
  }


def generation_query(args: argparse.Namespace) -> dict[str, Any]:
  query: dict[str, Any] = {
    "tracks": [args.track_id],
    "cars": [args.car_id],
    "group": "driver",
    "limit": min(args.max_laps, 100),
    "lapTypes": [1],
  }
  if args.team:
    query["teams"] = args.team
  if args.driver:
    query["drivers"] = args.driver
  if args.min_rating is not None:
    query["minRating"] = args.min_rating
  if args.max_rating is not None:
    query["maxRating"] = args.max_rating
  if args.age is not None:
    query["age"] = args.age
  if args.session_types:
    query["sessionTypes"] = [int(item) for item in args.session_types.split(",") if item]
  if args.fixed_setup:
    query["sessionSetupTypes"] = [2]
  if args.open_setup:
    query["sessionSetupTypes"] = [1]
  if args.allow_unclean:
    query["unclean"] = True
  return query


def write_pack(output_path: Path, scenarios: list[dict[str, Any]], generated_at: str) -> None:
  output_path.parent.mkdir(parents=True, exist_ok=True)
  pack = {
    "id": "garage61-generated-scenarios",
    "name": "Garage 61 generated scenarios",
    "generatedAt": generated_at,
    "generatorVersion": GENERATOR_VERSION,
    "sourceProvider": "garage61",
    "scenarios": scenarios,
  }
  output_path.write_text(json.dumps(pack, indent=2) + "\n", encoding="utf-8")


def main() -> int:
  configure_logging()
  args = parse_args()
  token = os.environ.get("GARAGE61_TOKEN")
  if not token:
    LOGGER.error("GARAGE61_TOKEN is required in the environment")
    return 2

  query = generation_query(args)
  analysis_map = load_analysis_map(args.garage61_analysis_map)
  generated_at = now_iso()
  laps_response = request(token, "/laps", query)
  laps = sorted(
    laps_response.get("items", []),
    key=lambda lap: float(lap.get("lapTime") or math.inf),
  )
  all_visible_laps = [lap for lap in laps if lap.get("canViewTelemetry")]
  visible_laps = all_visible_laps[: min(args.csv_limit, 100)]

  if not visible_laps:
    LOGGER.error("No telemetry-visible laps matched the query")
    return 1

  events: list[LapEvent] = []
  analyzed_laps = 0
  for index, lap in enumerate(visible_laps, start=1):
    LOGGER.info(
      "Telemetry %d/%d | lap %s",
      index,
      len(visible_laps),
      lap["id"],
    )
    csv_text = request(token, f"/laps/{lap['id']}/csv", expect_text=True)
    if csv_text is None:
      LOGGER.warning("Skipping lap %s: telemetry CSV not visible", lap["id"])
      continue
    rows = load_csv_rows(csv_text)
    if len(rows) < 100:
      LOGGER.warning("Skipping lap %s: not enough telemetry rows", lap["id"])
      continue
    lap_events = detect_events(lap, rows)
    if lap_events:
      analyzed_laps += 1
      events.extend(lap_events)
    time.sleep(0.2)

  if not events:
    LOGGER.error("No braking events were detected")
    return 1

  min_source_laps = max(3, round(analyzed_laps * args.min_coverage))
  clusters = [
    cluster
    for cluster in cluster_events(events, args.cluster_tolerance)
    if len({event.lap["id"] for event in cluster.events}) >= min_source_laps
  ]
  clusters.sort(key=lambda cluster: cluster.center)

  if not clusters:
    LOGGER.error("No stable braking-zone clusters survived coverage filtering")
    return 1

  first_lap = visible_laps[0]
  default_prefix = (
    f"{first_lap['track']['name']} {first_lap['track'].get('variant', '').strip()} "
    f"{first_lap['car']['name']}"
  ).replace("  ", " ").strip()
  name_prefix = args.name_prefix or default_prefix
  source_car = source_car_entity(args, token, first_lap)
  scenarios: list[dict[str, Any]] = []
  for zone, cluster in enumerate(clusters, start=1):
    scenarios.append(
      make_scenario(
        cluster,
        zone,
        analyzed_laps,
        query,
        generated_at,
        args.approach_ms,
        name_prefix,
        analysis_map,
        args.garage61_telemetry_view,
        source_car,
      ),
    )

  write_pack(Path(args.output), scenarios, generated_at)
  LOGGER.info(
    "Complete | %d scenarios | %d events | %d/%d laps analyzed | %s",
    len(scenarios),
    len(events),
    analyzed_laps,
    len(visible_laps),
    args.output,
  )
  return 0


if __name__ == "__main__":
  raise SystemExit(main())
