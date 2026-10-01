#!/usr/bin/env python3
"""Run sequential A-B-A-B stable/churn comparisons; requires exclusive host window."""
import argparse
from pathlib import Path
import subprocess
import sys

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--baseline-binary', type=Path, required=True)
p.add_argument('--baseline-source', type=Path, required=True)
p.add_argument('--target-binary', type=Path, required=True)
p.add_argument('--target-source', type=Path, required=True)
p.add_argument('--out', type=Path, required=True)
a = p.parse_args()
a.out.mkdir(parents=True, exist_ok=True)
for profile, hz, churn in [('stable', '30', '0'), ('churn', '1', '1')]:
    for label, ordinal in [('baseline', 1), ('target', 1), ('baseline', 2), ('target', 2)]:
        name = f'{label}-{profile}-{ordinal}'
        binary = a.baseline_binary if label == 'baseline' else a.target_binary
        source = a.baseline_source if label == 'baseline' else a.target_source
        command = [sys.executable, str(Path(__file__).with_name('load.py')), '--binary', str(binary),
                   '--source', str(source), '--label', name, '--out', str(a.out / name),
                   '--profile', profile, '--hz', hz, '--churn-hz', churn,
                   '--boids', '200', '--seed', '1', '--warmup', '20', '--window', '45',
                   '--activate-after-readiness']
        print('START ' + name, flush=True)
        subprocess.run(command, check=True)
        subprocess.run([sys.executable, str(Path(__file__).with_name('analyze.py')), str(a.out / name)],
                       check=True, stdout=subprocess.DEVNULL)
        print('COMPLETE ' + name, flush=True)
