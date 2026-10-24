#!/usr/bin/env bash
# Notte's Browser Arena (github.com/nottelabs/browserarena, MIT), its
# hello-browser benchmark, with Baton added as a provider by baton.patch and
# nothing else changed. Per session: create, connect over CDP, load
# https://example.com/ (domcontentloaded), release; 10 warm-up sessions, then
# RUNS measured, one at a time. Runs on the bench client in us-east-1.
#
#   BATON_API_KEY=bk_... RUNS=100 RATE=9 ./run.sh
#
# RATE is sessions per minute: the free account allows 10 new sessions a minute.
set -euo pipefail
cd "$(dirname "$0")"
PIN=d7992c64ab95ea6f6566c7f611e03159505b886a
RUNS=${RUNS:-100}
RATE=${RATE:-9}

if [ ! -d src-arena/.git ]; then
  git clone -q https://github.com/nottelabs/browserarena src-arena
fi
cd src-arena
git reset -q --hard "$PIN"
git clean -fdq
git apply ../baton.patch
npm install --ignore-scripts --no-audit --no-fund --prefer-online >/dev/null
npm install --no-save --ignore-scripts --no-audit --no-fund --prefer-online batonvm@0.3.0 >/dev/null
npm run dev -- --benchmark=hello-browser --provider=baton --runs="$RUNS" --rate="$RATE" --concurrency=1 --out=../baton.jsonl
