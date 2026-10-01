#!/usr/bin/env python3
"""One isolated SemBoids qualification run. Python stdlib + Node 22 WebSocket observer; records raw evidence.

Run identical arguments for baseline/target, changing only --binary/--label.
Metric absence is None, never zero. No existing Docker containers are touched.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import threading
import time
import urllib.request

IMAGE = 'nats@sha256:b270f5e2428354c0335612694d7dd2fb588148e567a5757fdff325ef9c9332e6'
COUNTERS = {
    'acked_entities': 'boids_graph_entities_published_total',
    'completed_snapshot_attempts': 'boids_graph_snapshots_published_total',
    'snapshot_offer_drops': 'boids_graph_snapshots_dropped_total',
    'entity_updates': 'semstreams_datamanager_entities_updated_total',
    'neighbor_clear_failures': 'boids_graph_neighbor_clear_failures_total',
    'reclaim_failures': 'boids_lifecycle_reclaim_failures_total',
    'spawn_creates': 'boids_lifecycle_spawns_total',
    'cull_observations': 'boids_lifecycle_culls_total',
    'mutation_rejections': 'semstreams_graph_ingest_mutation_rejections_total',
    'predicate_rejections': 'semstreams_graph_ingest_predicate_contract_rejections_total',
    'entity_state_rejections': 'semstreams_graph_ingest_entity_state_contract_rejections_total',
    'index_events': 'semstreams_graph_index_events_processed_total',
    'websocket_received': 'semstreams_websocket_messages_received_total',
    'websocket_sent': 'semstreams_websocket_messages_sent_total',
    'websocket_errors': 'semstreams_websocket_errors_total',
}


def fetch(url, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method='GET' if body is None else 'PUT',
                                 headers={'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=8) as response:
        return response.read().decode()


def exposition(raw):
    result = {}
    for line in raw.splitlines():
        if not line or line.startswith('#'):
            continue
        match = re.match(r'^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{.*\})?\s+([^\s]+)', line)
        if match:
            labels = dict(re.findall(r'(\w+)="([^"\\]*(?:\\.[^"\\]*)*)"', match[2] or ''))
            result.setdefault(match[1], []).append((labels, float(match[3])))
    return result


def total(metrics, name):
    series = metrics.get(name)
    return sum(value for _, value in series) if series else None


def delta(start, end, name):
    a, b = total(start, name), total(end, name)
    return b - a if a is not None and b is not None else None


def quantile(start, end, name, q):
    if name + '_bucket' not in start or name + '_bucket' not in end:
        return None
    buckets = {}
    for sign, metrics in [(-1, start), (1, end)]:
        for labels, value in metrics[name + '_bucket']:
            upper = float(labels['le'])
            buckets[upper] = buckets.get(upper, 0) + sign * value
    count = buckets.get(math.inf, 0)
    if count <= 0:
        return None
    previous, cumulative = 0, 0
    for upper, current in sorted(buckets.items()):
        if current >= q * count:
            if math.isinf(upper):
                return {'seconds': previous, 'lower_bound': True, 'observations': count}
            fraction = (q * count - cumulative) / (current - cumulative) if current > cumulative else 0
            return {'seconds': previous + fraction * (upper - previous),
                    'lower_bound': False, 'observations': count}
        previous, cumulative = upper, current
    return None


def consumer(jsz):
    for account in jsz.get('account_details', []):
        for stream in account.get('stream_detail', []):
            if stream.get('name') == 'ENTITY':
                for item in stream.get('consumer_detail', []):
                    if item.get('name') == 'graph-ingest-entity-wildcard':
                        return {'pending': item['num_pending'], 'ack_pending': item['num_ack_pending'],
                                'outstanding': item['num_pending'] + item['num_ack_pending'],
                                'redelivered': item['num_redelivered'],
                                'ack_floor': item['ack_floor'], 'delivered': item['delivered']}
    raise RuntimeError('authoritative ENTITY graph-ingest consumer missing from NATS /jsz')


class Frames:
    def __init__(self, port):
        self.sock = socket.create_connection(('127.0.0.1', port), timeout=10)
        self.sock.settimeout(None)
        self.sock.sendall(b'CONNECT {"verbose":false,"name":"semboids-qualification"}\r\nSUB boids.frames 1\r\nPING\r\n')
        self.rows = []
        self.error = None
        self.thread = threading.Thread(target=self.read, daemon=True)
        self.thread.start()

    def read(self):
        try:
            with self.sock.makefile('rb') as reader:
                while line := reader.readline():
                    if line.startswith(b'PING'):
                        self.sock.sendall(b'PONG\r\n')
                    elif line.startswith(b'MSG '):
                        size = int(line.split()[-1])
                        payload = json.loads(reader.read(size))
                        reader.read(2)
                        self.rows.append({'at': time.time(), 'tick': payload['tick'],
                                          'population': len(payload['boids']),
                                          'frame_timestamp_ms': payload['t'],
                                          **({'boids_sha256': hashlib.sha256(json.dumps(payload['boids'], separators=(',', ':')).encode()).hexdigest()}
                                             if payload['tick'] % 300 == 0 else {})})
                    elif line.startswith(b'-ERR'):
                        raise RuntimeError(line.decode())
        except Exception as error:
            self.error = str(error)

    def close(self):
        self.sock.shutdown(socket.SHUT_RDWR)
        self.sock.close()
        self.thread.join(timeout=3)


def wait_ready(url, process=None, seconds=60):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if process is not None and process.poll() is not None:
            raise RuntimeError(f'process exited before ready: {process.returncode}')
        try:
            return fetch(url)
        except Exception:
            time.sleep(0.2)
    raise RuntimeError(f'timed out waiting for {url}')


def save(path, value):
    path.write_text(json.dumps(value, indent=2, allow_nan=False) + '\n')


def cleanup_resources(out, container, process, frames, ws_process, slow):
    """Every owned resource gets a cleanup attempt despite observer failures."""
    errors = []
    def attempt(label, action):
        try:
            action()
        except Exception as error:
            errors.append(label + ': ' + str(error))
    def stop_ws():
        if ws_process.poll() is None:
            ws_process.terminate()
            try:
                ws_process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                ws_process.kill()
                ws_process.wait(timeout=5)
    def stop_app():
        if process.poll() is None:
            stopped = time.monotonic()
            process.send_signal(signal.SIGTERM)
            forced = False
            try:
                process.wait(timeout=25)
            except subprocess.TimeoutExpired:
                forced = True
                process.kill()
                process.wait(timeout=5)
            save(out / 'shutdown.json', {'returncode': process.returncode,
                 'seconds': time.monotonic() - stopped, 'forced_kill': forced})
    def logs():
        with (out / 'nats.log').open('w') as log:
            subprocess.run(['docker', 'logs', container], stdout=log, stderr=subprocess.STDOUT, check=False, timeout=10)
    if slow:
        attempt('slow-websocket-close', slow.close)
    if ws_process:
        attempt('websocket-stop', stop_ws)
    if frames:
        attempt('frames-save', lambda: save(out / 'frames.json', frames.rows))
        attempt('frames-close', frames.close)
    if process:
        attempt('application-stop', stop_app)
    attempt('broker-logs', logs)
    attempt('broker-stop', lambda: subprocess.run(['docker', 'stop', '--time', '5', container],
                                                 check=False, stdout=subprocess.PIPE, timeout=15))
    if errors:
        save(out / 'cleanup-errors.json', errors)
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--label', required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--profile', choices=['stable', 'churn'], default='stable')
    parser.add_argument('--slow-client', action='store_true', help='separate transport probe: connect one non-reading client')
    parser.add_argument('--hz', type=float, default=30)
    parser.add_argument('--churn-hz', type=float, default=0)
    parser.add_argument('--boids', type=int, default=200)
    parser.add_argument('--seed', type=int, default=1)
    parser.add_argument('--warmup', type=float, default=20)
    parser.add_argument('--window', type=float, default=45)
    parser.add_argument('--interval', type=float, default=5)
    parser.add_argument('--nats-port', type=int, default=44222)
    parser.add_argument('--monitor-port', type=int, default=48222)
    parser.add_argument('--api-port', type=int, default=38080)
    parser.add_argument('--ws-port', type=int, default=38081)
    parser.add_argument('--metrics-port', type=int, default=39090)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=False)
    cfg = json.loads((args.source / 'configs/flock.json').read_text())
    cfg['nats']['urls'] = [f'nats://127.0.0.1:{args.nats_port}']
    cfg['services']['service-manager']['config']['http_port'] = args.api_port
    cfg['services']['metrics']['config']['port'] = args.metrics_port
    sim = cfg['components']['sim']['config']
    sim.update(boids=args.boids, seed=args.seed, tick_hz=30, graph_hz=args.hz)
    if args.profile == 'stable':
        sim['zones'] = []
    rule_cfg = cfg['components']['rule-processor']['config']
    rule_cfg['rules_files'] = [str((args.source / name).resolve()) for name in rule_cfg['rules_files']]
    cfg['components']['frames-websocket']['config']['ports']['outputs'][0]['config']['port'] = args.ws_port
    save(args.out / 'config.json', cfg)
    manifest = {name: str(value) if isinstance(value, Path) else value for name, value in vars(args).items()}
    manifest.update(image=IMAGE, binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),
                    started_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                    platform=os.uname()._asdict() if hasattr(os.uname(), '_asdict') else list(os.uname()),
                    logical_cpu_count=os.cpu_count(), load_start=os.getloadavg())
    manifest['go_version_m'] = subprocess.check_output(['go', 'version', '-m', str(args.binary)], text=True)
    save(args.out / 'manifest.json', manifest)
    container = 'semboids-qual-' + str(os.getpid())
    process, frames, ws_process, slow = None, None, None, None
    api = f'http://127.0.0.1:{args.api_port}/boids'
    metrics_url = f'http://127.0.0.1:{args.metrics_port}/metrics'
    monitor = f'http://127.0.0.1:{args.monitor_port}'
    snapshots = []
    def capture(phase):
        at = time.time()
        raw = fetch(metrics_url)
        jsz = json.loads(fetch(monitor + '/jsz?accounts=true&streams=true&consumers=true&config=true'))
        index = len(snapshots)
        (args.out / f'{index:03d}-{phase}.prom').write_text(raw)
        save(args.out / f'{index:03d}-{phase}-jsz.json', jsz)
        snapshot = {'at': at, 'phase': phase, 'load': os.getloadavg(), 'consumer': consumer(jsz),
                    'frame_count': len(frames.rows), 'last_frame': frames.rows[-1] if frames.rows else None}
        save(args.out / f'{index:03d}-{phase}.json', snapshot)
        snapshots.append((snapshot, exposition(raw)))
        print(json.dumps(snapshot), flush=True)
    try:
        subprocess.run(['docker', 'run', '--rm', '-d', '--name', container,
                        '-p', f'127.0.0.1:{args.nats_port}:4222', '-p', f'127.0.0.1:{args.monitor_port}:8222',
                        IMAGE, '-js', '-m', '8222'], check=True, stdout=subprocess.PIPE)
        wait_ready(monitor + '/healthz')
        with (args.out / 'semboids.log').open('w') as log:
            env = dict(os.environ, SEMBOIDS_NATS_URLS=f'nats://127.0.0.1:{args.nats_port}')
            process = subprocess.Popen([str(args.binary.resolve()), '--config', str((args.out / 'config.json').resolve()),
                                        '--log-format', 'json'], cwd=args.source, env=env,
                                       stdout=log, stderr=subprocess.STDOUT)
            wait_ready(api + '/rules', process)
            frames = Frames(args.nats_port)
            with (args.out / 'websocket-frames.jsonl').open('w') as wslog, (args.out / 'websocket.log').open('w') as wserr:
                ws_process = subprocess.Popen(['node', str(Path(__file__).with_name('websocket.mjs').resolve()),
                    f'ws://127.0.0.1:{args.ws_port}/ws'], stdout=wslog, stderr=wserr)
            if args.slow_client:
                slow = socket.socket()
                slow.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 1024)
                slow.settimeout(5)
                slow.connect(('127.0.0.1', args.ws_port))
                slow.sendall((f'GET /ws HTTP/1.1\r\nHost: 127.0.0.1:{args.ws_port}\r\n'
                    'Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\n'
                    'Sec-WebSocket-Key: MDEyMzQ1Njc4OWFiY2RlZg==\r\n\r\n').encode())
                headers = b''
                while not headers.endswith(b'\r\n\r\n'):
                    byte = slow.recv(1)
                    if not byte:
                        raise RuntimeError('slow client handshake closed')
                    headers += byte
                (args.out / 'slow-client-handshake.txt').write_bytes(headers)
                if b'101 Switching Protocols' not in headers:
                    raise RuntimeError('slow client upgrade failed')
            fetch(api + '/rules/cull', {'enabled': args.profile == 'churn'})
            fetch(api + '/population/churn-hz', {'hz': args.churn_hz})
            save(args.out / 'rules.json', json.loads(fetch(api + '/rules')))
            warmup_end = time.monotonic() + args.warmup
            while time.monotonic() < warmup_end:
                time.sleep(min(args.interval, max(0, warmup_end - time.monotonic())))
                capture('warmup')
            capture('start')
            start_idx = len(snapshots) - 1
            measured_end = time.monotonic() + args.window
            while time.monotonic() < measured_end:
                time.sleep(min(args.interval, max(0, measured_end - time.monotonic())))
                if process.poll() is not None:
                    raise RuntimeError(f'semboids exited during measurement: {process.returncode}')
                capture('sample' if time.monotonic() < measured_end else 'end')
            first, start = snapshots[start_idx]
            last, end = snapshots[-1]
            elapsed = last['at'] - first['at']
            measured_frames = [row for row in frames.rows if first['at'] <= row['at'] < last['at']]
            gaps = [b['at'] - a['at'] for a, b in zip(measured_frames, measured_frames[1:])]
            missing_ticks = sum(max(0, b['tick'] - a['tick'] - 1) for a, b in zip(measured_frames, measured_frames[1:]))
            changes = {key: delta(start, end, name) for key, name in COUNTERS.items()}
            ws_rows = [json.loads(row) for row in (args.out / 'websocket-frames.jsonl').read_text().splitlines()]
            ws_measured = [row for row in ws_rows if first['at'] <= row['at'] < last['at']]
            ws_gaps = sum(max(0, b['tick'] - a['tick'] - 1) for a, b in zip(ws_measured, ws_measured[1:]))
            summary = {'label': args.label, 'profile': args.profile, 'elapsed_seconds': elapsed,
                       'deltas': changes, 'rates_per_s': {key: value / elapsed if value is not None else None
                                                       for key, value in changes.items()},
                       'backlog_start': first['consumer'], 'backlog_end': last['consumer'],
                       'backlog_growth_per_s': (last['consumer']['outstanding'] - first['consumer']['outstanding']) / elapsed,
                       'physics_egress_fps': len(measured_frames) / elapsed,
                       'max_frame_gap_seconds': max(gaps) if gaps else None, 'missing_egress_ticks': missing_ticks,
                       'population_start': measured_frames[0]['population'] if measured_frames else None,
                       'population_end': measured_frames[-1]['population'] if measured_frames else None,
                       'e2e_p50': quantile(start, end, 'boids_graph_e2e_latency_seconds', 0.5),
                       'e2e_p99': quantile(start, end, 'boids_graph_e2e_latency_seconds', 0.99),
                       'websocket_fps': len(ws_measured) / elapsed, 'websocket_missing_ticks': ws_gaps,
                       'websocket_max_gap_seconds': max((b['at']-a['at'] for a,b in zip(ws_measured, ws_measured[1:])), default=None),
                       'websocket_max_delivery_age_ms': max((row['at']*1000-row['frame_timestamp_ms'] for row in ws_measured), default=None),
                       'load_end': os.getloadavg(), 'frame_reader_error': frames.error}
            save(args.out / 'summary.json', summary)
            print('SUMMARY ' + json.dumps(summary), flush=True)
    finally:
        cleanup_resources(args.out, container, process, frames, ws_process, slow)



if __name__ == '__main__':
    main()
