"""Behavioral guards for claims and cleanup failures identified in review."""
import copy
from pathlib import Path
import tempfile
import subprocess
import json
import unittest
from unittest.mock import Mock, patch

import analyze
import load
import operations


def fixture(last=100, outstanding=10):
    return {'name': 'ENTITY', 'created': 'same-stream', 'config': {'subjects': ['entity.>']},
            'state': {'first_seq': 1, 'last_seq': last, 'messages': last},
            'consumer_detail': [{'name': 'graph-ingest-entity-wildcard', 'stream_name': 'ENTITY',
                'created': 'same-consumer', 'config': {'filter_subject': 'entity.>', 'ack_policy': 'explicit',
                    'deliver_policy': 'all', 'max_deliver': -1},
                'num_pending': outstanding, 'num_ack_pending': 0, 'num_redelivered': 0,
                'delivered': {'consumer_seq': last-outstanding, 'stream_seq': last-outstanding}}]}


class ReviewGuards(unittest.TestCase):
    def test_complete_lossless_stream_allows_conservation(self):
        result = analyze.conservation(fixture(), fixture(200, 30), 10)
        self.assertEqual(result['completed_consumer_work_per_s'], 8)
        self.assertEqual(result['ineligible_reasons'], [])

    def test_holes_filter_change_recreated_consumer_and_redelivery_reject_claim(self):
        mutations = [
            lambda s: s['state'].update(messages=199),
            lambda s: s['state'].update(num_deleted=1),
            lambda s: s['consumer_detail'][0]['config'].update(filter_subject='entity.some'),
            lambda s: s['consumer_detail'][0].update(created='new-consumer'),
            lambda s: s['consumer_detail'][0].update(num_redelivered=1),
            lambda s: s['consumer_detail'][0]['config'].update(max_deliver=5),
            lambda s: s['consumer_detail'][0]['delivered'].update(consumer_seq=171),
        ]
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                end = fixture(200, 30)
                mutate(end)
                result = analyze.conservation(fixture(), end, 10)
                self.assertIsNone(result['completed_consumer_work_per_s'])
                self.assertTrue(result['ineligible_reasons'])

    def test_consumer_filter_must_cover_all_offered_subjects_even_if_unchanged(self):
        start, end = fixture(), fixture(200, 30)
        for state in [start, end]:
            state['consumer_detail'][0]['config']['filter_subject'] = 'entity.some'
        self.assertIsNone(analyze.conservation(start, end, 10)['completed_consumer_work_per_s'])

    def test_offered_load_tracks_changing_population_and_flags_missing_frames(self):
        rows = [{'at': tick / 30, 'tick': tick, 'population': 200 + tick // 30}
                for tick in range(1, 61)]
        actual = analyze.offered_from_frames(rows, 0, 2.01, 30, 1)
        self.assertEqual(actual['observed_offered_entities'], 403)
        self.assertEqual(actual['observed_snapshot_offers'], 2)
        missing = analyze.offered_from_frames(rows[:29] + rows[30:], 0, 2.01, 30, 1)
        self.assertIsNone(missing['inferred_offered_entities_per_s'])
        self.assertTrue(missing['ineligible_reasons'])

    def test_websocket_observer_recovers_and_logs_disconnect(self):
        observer = Path(__file__).with_name('websocket.mjs').resolve().as_uri()
        code = """let count=0;let finish;const done=new Promise(r=>finish=r);
        global.WebSocket=class {
          constructor(){count++;queueMicrotask(()=>{this.onopen?.();
            if(count===1)this.onclose({code:1006,reason:'test disconnect'});
            else{this.onmessage({data:JSON.stringify({tick:42,t:1,boids:[]})});finish();}});}
          close(){this.onclose({code:1000,reason:'stop'});}
        };
        await import(MODULE);await done;if(count!==2)throw Error('no reconnect');process.exit(0);
        """.replace('MODULE', json.dumps(observer))
        result = subprocess.run(['node', '--input-type=module', '-e', code],
                                capture_output=True, text=True, timeout=4, check=True)
        self.assertIn('"tick":42', result.stdout)
        self.assertIn('closed 1006 test disconnect', result.stderr)
        self.assertEqual(result.stderr.count('connected'), 2)

    def test_steady_activation_requires_full_public_readiness(self):
        statuses = {name: {'enabled': True, 'state': 'started', 'healthy': True}
                    for name in load.REQUIRED_COMPONENTS}
        text = 'boids_lifecycle_spawns_total 200\nboids_lifecycle_culls_total 0\n'
        text += 'boids_graph_dial_hz 0\nboids_graph_entities_published_total 0\n'
        for provider in ('graph_ingest', 'graph_index'):
            for suffix, value in [('bootstrap_complete', 1), ('readiness', 1), ('lag', 0)]:
                text += f'semstreams_{provider}_{suffix} {value}\n'
        metrics = load.exposition(text)
        queue = {'outstanding': 0}
        self.assertEqual(load.readiness_failures(statuses, metrics, queue, 200), [])
        for mutate in [lambda s,m,q: s['graph-ingest'].update(state='initialized'),
                       lambda s,m,q: s['graph-index'].update(healthy=False),
                       lambda s,m,q: m.pop('semstreams_graph_index_bootstrap_complete'),
                       lambda s,m,q: m.update(boids_lifecycle_spawns_total=[({}, 199)]),
                       lambda s,m,q: m.update(boids_lifecycle_culls_total=[({}, 1)]),
                       lambda s,m,q: q.update(outstanding=1)]:
            ss, mm, qq = copy.deepcopy(statuses), copy.deepcopy(metrics), copy.deepcopy(queue)
            mutate(ss, mm, qq)
            self.assertTrue(load.readiness_failures(ss, mm, qq, 200))

    def test_cleanup_keeps_app_and_broker_cleanup_after_observer_failures(self):
        app = Mock()
        app.poll.return_value = None
        app.returncode = 0
        ws = Mock()
        ws.poll.return_value = None
        ws.wait.side_effect = RuntimeError('observer wait failed')
        frames = Mock(rows=[])
        frames.close.side_effect = RuntimeError('frame close failed')
        with tempfile.TemporaryDirectory() as directory, patch.object(load.subprocess, 'run') as run:
            errors = load.cleanup_resources(Path(directory), 'owned-broker', app, frames, ws, None)
            app.send_signal.assert_called_once()
            app.wait.assert_called_once()
            self.assertTrue(any(call.args[0][:2] == ['docker', 'stop'] for call in run.call_args_list))
            self.assertTrue(any('websocket' in entry for entry in errors))
            self.assertTrue(any('frames-close' in entry for entry in errors))

    def test_live_probe_rejects_stalled_ticks_or_wrong_population(self):
        rows = [{'tick': n, 'population': 30} for n in range(1, 6)]
        operations.validate_live(rows, {'last_seq': 31}, 30, 30)
        for bad in [[{'tick': 1, 'population': 30}] * 5,
                    [{'tick': n, 'population': 29} for n in range(1, 6)]]:
            with self.assertRaises(RuntimeError):
                operations.validate_live(bad, {'last_seq': 31}, 30, 30)
        with self.assertRaises(RuntimeError):
            operations.validate_live(rows, {'last_seq': 30}, 30, 30)


if __name__ == '__main__':
    unittest.main()
