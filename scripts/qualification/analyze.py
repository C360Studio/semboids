#!/usr/bin/env python3
"""Derive reproducible cross-checks from preserved raw run directories."""
import argparse
from datetime import datetime
import json
from pathlib import Path

import load


def stream(jsz, name):
    for account in jsz['account_details']:
        for item in account['stream_detail']:
            if item['name'] == name:
                return item
    raise ValueError(f'missing stream {name}')


def conservation(start, end, elapsed):
    """Conservative completion estimate; refuse ambiguous stream/consumer histories.

    NATS omits zero num_deleted in JSON. Other required fields must exist.
    Completion includes ACK/TERM disposition, not necessarily a successful commit;
    compare with committed-write metrics and rejection/error evidence separately.
    """
    reasons = []
    for label, item in [('start', start), ('end', end)]:
        state = item['state']
        if state.get('first_seq') != 1:
            reasons.append(label + ': first sequence is not 1')
        if state.get('messages') != state.get('last_seq', 0) - state.get('first_seq', 0) + 1:
            reasons.append(label + ': stream has missing retained messages')
        if state.get('num_deleted', 0) != 0 or state.get('deleted'):
            reasons.append(label + ': stream reports deletions')
    if start.get('created') != end.get('created') or start.get('config') != end.get('config'):
        reasons.append('stream identity/config changed')
    def selected(item):
        matches = [c for c in item.get('consumer_detail', []) if c.get('name') == 'graph-ingest-entity-wildcard']
        return matches[0] if len(matches) == 1 else None
    first, last = selected(start), selected(end)
    if first is None or last is None:
        reasons.append('exact consumer unavailable')
    else:
        for key in ('name', 'stream_name', 'created', 'config'):
            if key not in first or key not in last or first[key] != last[key]:
                reasons.append('consumer identity/config changed or missing: ' + key)
        for label, item, c in [('start', start, first), ('end', end, last)]:
            cfg = c.get('config', {})
            subjects = set(item.get('config', {}).get('subjects', []))
            filters = set(cfg.get('filter_subjects', []))
            if cfg.get('filter_subject'):
                filters.add(cfg['filter_subject'])
            if not subjects or (filters and filters != subjects and filters != {'>'}):
                reasons.append(label + ': consumer filter does not prove coverage of all stream subjects')
            if cfg.get('ack_policy') != 'explicit' or cfg.get('deliver_policy') != 'all':
                reasons.append(label + ': unsupported ACK/delivery policy')
            if cfg.get('max_deliver', 0) > 0:
                reasons.append(label + ': finite delivery limit can retire unprocessed work')
            delivered = c.get('delivered', {})
            if c.get('num_redelivered') != 0 or delivered.get('consumer_seq') != delivered.get('stream_seq'):
                reasons.append(label + ': zero-redelivery prerequisite not proven')
    accepted = end['state']['last_seq'] - start['state']['last_seq']
    if elapsed <= 0 or accepted < 0:
        reasons.append('elapsed time/accepted sequence delta invalid')
    completed = None
    if not reasons:
        outstanding = (last['num_pending'] + last['num_ack_pending'] -
                       first['num_pending'] - first['num_ack_pending'])
        completed = (accepted - outstanding) / elapsed
        if completed < 0:
            reasons.append('negative inferred completion')
            completed = None
    return {'accepted_stream_messages': accepted,
            'accepted_stream_per_s': accepted / elapsed if elapsed > 0 else None,
            'no_retention_loss': not any('sequence' in r or 'retained' in r or 'deletions' in r for r in reasons),
            'completed_consumer_work_per_s': completed, 'ineligible_reasons': reasons}


def analyze(directory):
    summary = json.loads((directory / 'summary.json').read_text())
    start_path = next(directory.glob('*-start.json'))
    end_path = next(directory.glob('*-end.json'))
    first, last = json.loads(start_path.read_text()), json.loads(end_path.read_text())
    js_start = json.loads(start_path.with_name(start_path.stem + '-jsz.json').read_text())
    js_end = json.loads(end_path.with_name(end_path.stem + '-jsz.json').read_text())
    ss, es = stream(js_start, 'ENTITY'), stream(js_end, 'ENTITY')
    elapsed = last['at'] - first['at']
    findings = conservation(ss, es, elapsed)
    findings.update(ack_pending_start=first['consumer']['ack_pending'],
                    ack_pending_end=last['consumer']['ack_pending'],
                    redelivered_start=first['consumer']['redelivered'],
                    redelivered_end=last['consumer']['redelivered'])
    events = {}
    for line in (directory / 'semboids.log').read_text().splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if 'time' not in event or event.get('level') not in ('WARN', 'ERROR'):
            continue
        at = datetime.fromisoformat(event['time']).timestamp()
        if first['at'] <= at <= last['at']:
            key = '|'.join(str(event.get(field, '')) for field in ['level', 'msg', 'reason'])
            events[key] = events.get(key, 0) + 1
    findings['measurement_warn_error_events'] = events
    prom_start = load.exposition(start_path.with_suffix('.prom').read_text())
    prom_end = load.exposition(end_path.with_suffix('.prom').read_text())
    findings['critical_counters_start_end'] = {
        name: {'start': load.total(prom_start, name), 'end': load.total(prom_end, name)}
        for name in [*load.COUNTERS.values(), 'semstreams_graph_ingest_poisoned_entities']}
    findings['websocket_error_series_end'] = prom_end.get('semstreams_websocket_errors_total')
    findings['physics_hashes'] = {str(row['tick']): row['boids_sha256']
                                for row in json.loads((directory / 'frames.json').read_text())
                                if 'boids_sha256' in row}
    ws_path = directory / 'websocket-frames.jsonl'
    if ws_path.exists():
        rows = [json.loads(row) for row in ws_path.read_text().splitlines()]
        measured = [row for row in rows if first['at'] <= row['at'] < last['at']]
        findings['websocket_max_gap_seconds'] = max((b['at']-a['at'] for a,b in zip(measured, measured[1:])), default=None)
        findings['websocket_max_age_ms'] = max((row['at']*1000-row['frame_timestamp_ms'] for row in measured), default=None)
    load.save(directory / 'cross-checks.json', findings)
    return dict(run=directory.name, summary=summary, cross_checks=findings)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directories', nargs='+', type=Path)
    args = parser.parse_args()
    print(json.dumps([analyze(directory) for directory in args.directories], indent=2))


if __name__ == '__main__':
    main()
