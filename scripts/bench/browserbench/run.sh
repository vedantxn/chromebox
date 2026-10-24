#!/usr/bin/env bash
# Steel's open-source remote-browser benchmark (github.com/steel-dev/browserbench,
# MIT), with Baton added as a provider by baton.patch and nothing else changed.
# It times create, connect (connectOverCDP), goto (domcontentloaded on
# google.com) and release, one session at a time after 10 warm-up runs, the
# same as the numbers Steel published. Runs on the bench client in us-east-1.
#
#   BATON_API_KEY=bk_... RUNS=300 RATE=9 ./run.sh
#
# RATE is sessions per minute: the free account allows 10 new sessions a
# minute, so the run paces itself under that rather than asking for exemptions.
set -euo pipefail
cd "$(dirname "$0")"
PIN=847e4ed604764ce8b887265709ddb2d1c3c5f020
RUNS=${RUNS:-300}
RATE=${RATE:-9}

if [ ! -d src-bb/.git ]; then
  git clone -q https://github.com/steel-dev/browserbench src-bb
fi
cd src-bb
git reset -q --hard "$PIN"
git clean -fdq
git apply ../baton.patch
npm install --ignore-scripts --no-audit --no-fund --prefer-online >/dev/null
# Upstream's documented dev run (tsx): its compiled output is ES modules in a
# package without "type": "module", which only Node 22+ runs.
npm run dev -- --provider baton --runs "$RUNS" --rate "$RATE" --url https://google.com/ --out ../baton.jsonl
