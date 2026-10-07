"""Regenerate the synthetic parity fixture using the original Python model.

Run from the repository root. No Garage61 calls or real driver data are used.
"""
import argparse
import csv
import io
import json
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[3] / 'tools' / 'brake-it'))
import garage61_catalog as catalog
import garage61_generate as generator

car = {'id': 101, 'name': 'Fixture car', 'platform': 'iracing'}
track = {'id': 202, 'name': 'Fixture circuit', 'variant': 'Road', 'platform': 'iracing'}
laps = []
csvs = {}
for lap_index in range(6):
    lap = {'id': f'synthetic-{lap_index}', 'lapTime': 80 + lap_index / 10, 'canViewTelemetry': True, 'car': car, 'track': track}
    laps.append(lap)
    stream = io.StringIO()
    writer = csv.writer(stream)
    writer.writerow(['Speed', 'LapDistPct', 'Brake', 'Throttle', 'PositionType'])
    for i in range(1601):
        brake, throttle = 0, 1
        for start, peak, transition in [(200 + lap_index, .65 + .03 * lap_index, 12), (950 - lap_index, .9 - .02 * lap_index, 0)]:
            t = i - start
            if -8 <= t < 0: throttle = -t / 8
            if 0 <= t < 8: brake, throttle = peak * t / 8, 0
            if 8 <= t < 30: brake, throttle = peak, 0
            if 30 <= t < 70: brake, throttle = peak * (70 - t) / 40, 0
            if 70 <= t < 70 + transition: throttle = 0
            if 70 + transition <= t < 100 + transition: throttle = (t - 70 - transition) / 30
        writer.writerow([50 - brake * 20, i / 1600, brake, throttle, 3])
    csvs[lap['id']] = stream.getvalue()
original = generator.request
generator.request = lambda token, path, **kwargs: csvs[path.split('/')[2]]
try:
    scenarios = catalog.analyze_combination('', track, car, laps, argparse.Namespace(csv_limit=12, max_laps=100, min_coverage=.35, cluster_tolerance=.018, approach_ms=2200), '2026-10-06T12:00:00Z')
finally:
    generator.request = original
output = {'car': car, 'track': track, 'laps': laps, 'csv': csvs, 'scenarios': scenarios}
Path(__file__).with_name('python_parity.json').write_text(json.dumps(output, indent=2) + '\n')
