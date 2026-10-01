#!/usr/bin/env python3
"""Measure raw non-test Go lines per package directory in an already captured closure.

The TSV is the authoritative build-selected package census. Directory LOC includes
all non-test .go files, including other build tags/platforms; it is not linked size.
"""
import argparse
import csv
import json
from pathlib import Path
import re

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--module-dir', type=Path, required=True)
parser.add_argument('--consumer-dir', type=Path, required=True)
parser.add_argument('--evidence-dir', type=Path, required=True)
args = parser.parse_args()
module = 'github.com/c360studio/semstreams'

def package_set(filename):
    with (args.evidence_dir / filename).open() as stream:
        return {
            row['package'].split(' [')[0]
            for row in csv.DictReader(stream, delimiter='\t')
            if row['module'] == module
        }

production = package_set('production-packages.tsv')
integration = package_set('integration-test-packages.tsv')
direct = set()
for path in args.consumer_dir.rglob('*.go'):
    if path.name.endswith('_test.go') or 'ui' in path.relative_to(args.consumer_dir).parts:
        continue
    direct.update(re.findall(r'"(github.com/c360studio/semstreams/[^"\s]+)"', path.read_text()))

def measurement(packages):
    rows = []
    for package in sorted(packages):
        directory = args.module_dir / package.removeprefix(module + '/')
        sources = [p for p in directory.glob('*.go') if not p.name.endswith('_test.go')]
        rows.append({
            'package': package,
            'non_test_go_files': len(sources),
            'non_test_go_lines': sum(len(p.read_bytes().splitlines()) for p in sources),
        })
    return {
        'packages': len(rows),
        'non_test_go_lines': sum(row['non_test_go_lines'] for row in rows),
        'package_rows': rows,
    }

print(json.dumps({
    'module_directory': str(args.module_dir),
    'consumer_directory': str(args.consumer_dir),
    'method': 'Raw non-test .go directory lines; package reachability from recorded TSV; not linked symbols',
    'direct_production_imports': measurement(direct),
    'production_closure': measurement(production),
    'integration_closure': measurement(integration),
    'integration_only': measurement(integration - production),
}, indent=2))
