"""Protect against the silent-missing-metric and invisible in-flight traps."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('qualification_load', Path(__file__).with_name('load.py'))
load = importlib.util.module_from_spec(spec)
spec.loader.exec_module(load)


class EvidenceTests(unittest.TestCase):
    def test_missing_is_not_zero(self):
        self.assertIsNone(load.total({}, 'missing'))
        self.assertIsNone(load.delta({}, {'m': [({}, 4)]}, 'm'))
        self.assertEqual(load.total({'m': [({}, 0)]}, 'm'), 0)
        self.assertIsNone(load.quantile({}, {}, 'latency', .99))

    def test_ack_pending_is_work_even_when_pending_zero(self):
        record = {'name': 'graph-ingest-entity-wildcard', 'num_pending': 0,
                  'num_ack_pending': 17, 'num_redelivered': 2,
                  'ack_floor': {'consumer_seq': 3}, 'delivered': {'consumer_seq': 20}}
        jsz = {'account_details': [{'stream_detail': [{'name': 'ENTITY', 'consumer_detail': [record]}]}]}
        self.assertEqual(load.consumer(jsz)['outstanding'], 17)
        with self.assertRaises(RuntimeError):
            load.consumer({})

    def test_labeled_series_and_histogram_delta(self):
        start = load.exposition('m{x="a"} 3\nm{x="b"} 4\nh_bucket{le="1"} 1\nh_bucket{le="+Inf"} 2\n')
        end = load.exposition('m{x="a"} 8\nm{x="b"} 5\nh_bucket{le="1"} 3\nh_bucket{le="+Inf"} 4\n')
        self.assertEqual(load.delta(start, end, 'm'), 6)
        self.assertEqual(load.quantile(start, end, 'h', .5),
                         {'seconds': .5, 'lower_bound': False, 'observations': 2})
        end = load.exposition('h_bucket{le="1"} 1\nh_bucket{le="+Inf"} 4\n')
        self.assertEqual(load.quantile(start, end, 'h', .99),
                         {'seconds': 1, 'lower_bound': True, 'observations': 2})


if __name__ == '__main__':
    unittest.main()
