#!/usr/bin/env python3
"""Process restart/cancellation probes, deliberately outside timed load runs."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import time

import load


def until(test, timeout=35):
    deadline = time.monotonic() + timeout
    error = None
    while time.monotonic() < deadline:
        try:
            result = test()
            if result:
                return result
        except Exception as exc:
            error = str(exc)
        time.sleep(.2)
    raise RuntimeError(f'condition not met in {timeout}s; last error={error}')


def validate_live(rows, graph_state, after_seq, expected_population):
    if len(rows) < 5 or any(b['tick'] <= a['tick'] for a, b in zip(rows, rows[1:])):
        raise RuntimeError('physics ticks failed to advance strictly')
    if any(row['population'] != expected_population for row in rows):
        raise RuntimeError('observed unexpected live population')
    if graph_state['last_seq'] <= after_seq:
        raise RuntimeError('graph failed to advance')


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--binary', type=Path, required=True)
    p.add_argument('--source', type=Path, required=True)
    p.add_argument('--label', required=True)
    p.add_argument('--out', type=Path, required=True)
    p.add_argument('--only-port-conflict', action='store_true')
    a = p.parse_args()
    a.out.mkdir(parents=True, exist_ok=False)
    cfg = json.loads((a.source / 'configs/flock.json').read_text())
    nats_port, monitor_port, api_port, metrics_port, ws_port = 45222, 49222, 48080, 49090, 48081
    cfg['nats']['urls'] = [f'nats://127.0.0.1:{nats_port}']
    cfg['services']['service-manager']['config']['http_port'] = api_port
    cfg['services']['metrics']['config']['port'] = metrics_port
    cfg['components']['sim']['config'].update(boids=30, seed=1, tick_hz=30, graph_hz=1, zones=[])
    rules = cfg['components']['rule-processor']['config']
    rules['rules_files'] = [str((a.source / path).resolve()) for path in rules['rules_files']]
    cfg['components']['frames-websocket']['config']['ports']['outputs'][0]['config']['port'] = ws_port
    load.save(a.out / 'config.json', cfg)
    load.save(a.out / 'manifest.json', {'label': a.label, 'binary_sha256': hashlib.sha256(a.binary.read_bytes()).hexdigest(),
              'source': str(a.source), 'image': load.IMAGE,
              'go_version_m': subprocess.check_output(['go', 'version', '-m', str(a.binary)], text=True)})
    container = 'semboids-ops-' + str(os.getpid())
    monitor = f'http://127.0.0.1:{monitor_port}'
    api = f'http://127.0.0.1:{api_port}/boids'
    active = []
    results = []

    def record(name, **fields):
        row = dict(probe=name, utc=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), **fields)
        results.append(row)
        load.save(a.out / 'results.json', results)
        print(json.dumps(row), flush=True)

    def start(label, override=None):
        log = (a.out / (label + '.log')).open('w')
        env = dict(os.environ, SEMBOIDS_NATS_URLS=override or f'nats://127.0.0.1:{nats_port}')
        proc = subprocess.Popen([str(a.binary.resolve()), '--config', str((a.out / 'config.json').resolve()),
                                 '--log-format', 'json'], cwd=a.source, env=env, stdout=log, stderr=subprocess.STDOUT)
        log.close()
        active.append(proc)
        return proc

    def stop(proc, label, sig=signal.SIGTERM):
        at = time.monotonic()
        if proc.poll() is None:
            proc.send_signal(sig)
        forced = False
        try:
            proc.wait(timeout=25)
        except subprocess.TimeoutExpired:
            forced = True
            proc.kill()
            proc.wait()
        record(label, returncode=proc.returncode, seconds=time.monotonic()-at, forced_kill=forced)

    def state(label):
        jsz = json.loads(load.fetch(monitor + '/jsz?accounts=true&streams=true&consumers=true&config=true'))
        load.save(a.out / (label + '-jsz.json'), jsz)
        for account in jsz.get('account_details', []):
            for stream in account.get('stream_detail', []):
                if stream['name'] == 'KV_ENTITY_STATES':
                    return stream['state']
        raise RuntimeError('ENTITY_STATES stream absent')

    def verify_live(label, after_seq=0):
        load.wait_ready(api + '/rules', active[-1])
        reader = load.Frames(nats_port)
        try:
            until(lambda: len(reader.rows) >= 5)
            graph_state = until(lambda: (s if (s := state(label))['last_seq'] > after_seq else None))
            validate_live(reader.rows, graph_state, after_seq, 30)
            load.save(a.out / (label + '-frames.json'), reader.rows)
            record(label, frames=len(reader.rows), tick_first=reader.rows[0]['tick'],
                   tick_last=reader.rows[-1]['tick'], population=reader.rows[-1]['population'],
                   entity_states=graph_state, passed=True)
            return graph_state['last_seq'], reader.rows[-1]['tick']
        finally:
            reader.close()

    try:
        subprocess.run(['docker', 'run', '--rm', '-d', '--name', container,
                        '-p', f'127.0.0.1:{nats_port}:4222', '-p', f'127.0.0.1:{monitor_port}:8222',
                        load.IMAGE, '-js', '-m', '8222'], check=True, stdout=subprocess.PIPE)
        load.wait_ready(monitor + '/healthz')
        if not a.only_port_conflict:
            first = start('startup')
            seq1, tick1 = verify_live('fresh_startup')
            stop(first, 'first_sigterm')
            retained_before = state('before_app_restart')['last_seq']
            second = start('app-restart')
            seq2, tick2 = verify_live('retained_application_restart', retained_before)
            before_broker = state('before_broker_restart')
            before_metrics = load.exposition(load.fetch(f'http://127.0.0.1:{metrics_port}/metrics'))
            before_index = load.total(before_metrics, 'semstreams_graph_index_events_processed_total')
            subprocess.run(['docker', 'restart', '--time', '5', container], check=True, stdout=subprocess.PIPE)
            load.wait_ready(monitor + '/healthz')
            after_broker = state('after_broker_restart')
            record('broker_storage_retained', before=before_broker, after=after_broker,
                   passed=after_broker['last_seq'] >= before_broker['last_seq'])
            verify_live('broker_reconnect', after_broker['last_seq'])
            def index_progress():
                raw = load.fetch(f'http://127.0.0.1:{metrics_port}/metrics')
                (a.out / 'after-broker-reconnect.prom').write_text(raw)
                current = load.total(load.exposition(raw), 'semstreams_graph_index_events_processed_total')
                return current if current is not None and before_index is not None and current > before_index else None
            try:
                after_index = until(index_progress, timeout=35)
                before_index = after_index
                continuing_index = until(index_progress, timeout=35)
                record('index_reconnect_progress', after_reconnect=after_index, subsequent=continuing_index, passed=True)
            except RuntimeError as error:
                record('index_reconnect_progress', before=before_index, passed=False, error=str(error))
            stop(second, 'restart_sigterm')

            stalled = socket.socket()
            stalled.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            stalled.bind(('127.0.0.1', 45999))
            stalled.listen()
            stalled.settimeout(10)
            try:
                unavailable = start('cancel-stalled-nats', 'nats://127.0.0.1:45999')
                connection, address = stalled.accept()
                record('startup_cancellation_signal', process_alive=unavailable.poll() is None,
                       accepted_peer=str(address), passed=unavailable.poll() is None)
                stop(unavailable, 'cancellation_during_stalled_nats')
                connection.close()
            finally:
                stalled.close()

        conflict = socket.socket()
        conflict.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        conflict.bind(('0.0.0.0', api_port))
        conflict.listen()
        conflict6 = socket.socket(socket.AF_INET6, socket.SOCK_STREAM)
        conflict6.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        conflict6.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
        conflict6.bind(('::', api_port))
        conflict6.listen()
        record('occupied_listeners', ipv4=str(conflict.getsockname()), ipv6=str(conflict6.getsockname()),
               ipv6_only=conflict6.getsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY))
        try:
            failed = start('failed-startup-port-conflict')
            at = time.monotonic()
            try:
                code = failed.wait(timeout=25)
                record('failed_startup_port_conflict', returncode=code, seconds=time.monotonic()-at,
                       passed=code != 0 and 'address already in use' in (a.out / 'failed-startup-port-conflict.log').read_text(),
                           explicit_bind_error='address already in use' in (a.out / 'failed-startup-port-conflict.log').read_text(),
                           forced_kill=False)
            except subprocess.TimeoutExpired:
                record('failed_startup_port_conflict', passed=False, error='did not exit within 25 seconds',
                           explicit_bind_error='address already in use' in (a.out / 'failed-startup-port-conflict.log').read_text())
                stop(failed, 'failed_startup_forced_cancellation')
        finally:
            conflict6.close()
            conflict.close()
    except Exception as error:
        record('probe_error', error=str(error), passed=False)
        raise
    finally:
        for proc in active:
            if proc.poll() is None:
                stop(proc, 'cleanup_sigterm')
        with (a.out / 'nats.log').open('w') as log:
            subprocess.run(['docker', 'logs', container], stdout=log, stderr=subprocess.STDOUT, check=False)
        subprocess.run(['docker', 'stop', '--time', '5', container], check=False, stdout=subprocess.PIPE)


if __name__ == '__main__':
    main()
