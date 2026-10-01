#!/usr/bin/env python3
"""Summarize the selected eight steady-state runs without hiding pair variance."""
import argparse
import json
from pathlib import Path


def aggregate(root):
    runs, pairs = [], []
    hashes = {}
    for profile in ('stable', 'churn'):
        for ordinal in (1, 2):
            pair = []
            for pin in ('baseline', 'target'):
                name = f'{pin}-{profile}-{ordinal}'
                directory = root / name
                read = lambda file: json.loads((directory / file).read_text())
                summary, checks, manifest = read('summary.json'), read('cross-checks.json'), read('manifest.json')
                ready, activation = read('admitted-readiness.json'), read('load-activation.json')
                if ready['failures'] or activation['http_status'] != 200 or activation['reply']['hz'] != manifest['hz']:
                    raise ValueError('run admission failed: ' + name)
                rows = read('frames.json')
                measured_start = read(next(directory.glob('*-start.json')).name)['at']
                measured_end = read(next(directory.glob('*-end.json')).name)['at']
                populations = [r['population'] for r in rows if measured_start <= r['at'] < measured_end]
                record = dict(run=name, binary_sha256=manifest['binary_sha256'],
                              elapsed_seconds=summary['elapsed_seconds'],
                              activation=activation, admitted_at=ready['at'],
                              offered_per_s=checks['offered_load']['inferred_offered_entities_per_s'],
                              accepted_per_s=checks['accepted_stream_per_s'],
                              committed_writes_per_s=summary['rates_per_s']['entity_updates'],
                              outstanding_start=summary['backlog_start']['outstanding'],
                              outstanding_end=summary['backlog_end']['outstanding'],
                              ack_pending_start=summary['backlog_start']['ack_pending'],
                              ack_pending_end=summary['backlog_end']['ack_pending'],
                              population_start=summary['population_start'], population_end=summary['population_end'],
                              population_min=min(populations), population_max=max(populations),
                              physics_fps=summary['physics_egress_fps'],
                              frame_max_gap_ms=1000*summary['max_frame_gap_seconds'],
                              missing_nats_ticks=summary['missing_egress_ticks'],
                              snapshot_drops=summary['deltas']['snapshot_offer_drops'],
                              spawned=summary['deltas']['spawn_creates'], culled=summary['deltas']['cull_observations'],
                              e2e_p50=summary['e2e_p50'], e2e_p99=summary['e2e_p99'],
                              ws_missing_ticks=checks['websocket_ticks_missing_against_nats_window'],
                              ws_max_gap_seconds=checks['websocket_max_gap_seconds'],
                              ws_max_age_ms=checks['websocket_max_age_ms'],
                              ws_reconnect_seconds=checks['websocket_reconnect_seconds'],
                              warn_error_events=checks['measurement_warn_error_events'],
                              load_start=manifest['load_start'], load_end=summary['load_end'],
                              shutdown=read('shutdown.json'))
                runs.append(record)
                pair.append(record)
                if profile == 'stable':
                    hashes[name] = checks['physics_hashes']
            baseline, target = pair
            pairs.append(dict(profile=profile, ordinal=ordinal,
                              target_over_baseline_commit_rate=target['committed_writes_per_s']/baseline['committed_writes_per_s'],
                              target_over_baseline_offered_rate=target['offered_per_s']/baseline['offered_per_s'],
                              interpretation='descriptive only; two repetitions, differing observed demand in churn'))
    common = sorted(set.intersection(*(set(values) for values in hashes.values())), key=int)
    mismatches = [tick for tick in common if len({values[tick] for values in hashes.values()}) != 1]
    return dict(selected_runs=[run['run'] for run in runs], runs=runs, pairs=pairs,
                stable_seeded_hash_comparison=dict(common_ticks=common, mismatches=mismatches,
                                                  per_run_hashes=hashes),
                qualification='fresh-store post-readiness steady-state only; does not qualify 30Hz cold startup')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    args.out.write_text(json.dumps(aggregate(args.directory), indent=2) + '\n')
