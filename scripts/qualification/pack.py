#!/usr/bin/env python3
"""Package qualification runs and readable summaries with a content hash index."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import tarfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--runs', type=Path, required=True)
p.add_argument('--destination', type=Path, required=True)
a = p.parse_args()
a.destination.mkdir(parents=True, exist_ok=True)
readable = a.destination / 'summaries'
readable.mkdir(exist_ok=True)
runs = sorted(path.parent for path in a.runs.rglob('manifest.json'))
archive = a.destination / 'qualification-evidence.tar.gz'
with tarfile.open(archive, 'w:gz') as tar:
    for run in runs:
        relative = run.relative_to(a.runs)
        tar.add(run, arcname=str(relative))
        for name in ('manifest.json', 'summary.json', 'cross-checks.json', 'results.json', 'shutdown.json'):
            file = run / name
            if file.exists():
                shutil.copy2(file, readable / (str(relative).replace('/', '--') + '-' + name))
index = {'archive': archive.name, 'sha256': hashlib.sha256(archive.read_bytes()).hexdigest(),
         'bytes': archive.stat().st_size, 'included_runs': [str(run.relative_to(a.runs)) for run in runs],
         'harness_files': {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
                           for path in sorted(Path(__file__).parent.iterdir())
                           if path.is_file() and path.suffix in ('.py', '.mjs')}}
(a.destination / 'qualification-evidence-index.json').write_text(json.dumps(index, indent=2) + '\n')
print(json.dumps(index))
