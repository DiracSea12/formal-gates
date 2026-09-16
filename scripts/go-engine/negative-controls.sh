#!/bin/sh
# Negative and positive controls for the go-engine acceptance runner.
#
# These exist because an earlier revision of the G0 baseline script returned
# success even when a required item failed, and reused one log directory so a
# re-run erased the earlier failure. Each control below drives the *real*
# runner against an isolated fixture root and asserts the conclusion:
#
#   NC-A  a required item genuinely fails            -> runner exits non-zero
#   NC-B  the demo flow stalls at WAITING_USER       -> runner exits non-zero
#   NC-C  two attempts never share a directory and   -> first attempt survives
#         the earlier failure is not overwritten
#   PC-D  a well-formed DEMO_COMPLETE fixture        -> runner exits zero
#
# No product source, no original acceptance record and no active run is touched.
set -u

OUT=${1:-}
if [ -z "$OUT" ]; then
  OUT=$(cd "$(dirname "$0")/../.." && pwd)/.harness/validation/acceptance-negatives/$(date -u +%Y%m%dT%H%M%SZ)-$$
fi
RUNNER=$(cd "$(dirname "$0")" && pwd)/acceptance.py

failures=0
say() { printf '%s\n' "$*"; }
verdict() {
  label=$1; expected=$2; actual=$3
  if [ "$expected" = "$actual" ]; then
    say "  OK   $label: exit $actual as expected"
  else
    say "  BAD  $label: exit $actual, expected $expected"
    failures=$((failures + 1))
  fi
}

mkdir -p "$OUT"
say "negative controls -> $OUT"

# ---- NC-A: a required item that genuinely fails -----------------------------
say ""
say "NC-A required item fails (no candidate runtime in the fixture root)"
A="$OUT/nc-a/root"
mkdir -p "$A"
git -C "$A" init -q
printf 'x\n' > "$A/README"
git -C "$A" -c user.name=ctl -c user.email=ctl@localhost -c commit.gpgsign=false add -A
git -C "$A" -c user.name=ctl -c user.email=ctl@localhost -c commit.gpgsign=false commit -qm fixture
python3 "$RUNNER" --unit nc-a --root "$A" --only identity > "$OUT/nc-a.stdout" 2> "$OUT/nc-a.stderr"
verdict "NC-A" 1 $?
python3 -c "
import json;d=json.load(open('$A/.harness/validation/acceptance/nc-a/'+__import__('os').listdir('$A/.harness/validation/acceptance/nc-a')[0]+'/summary.json'))
print('  conclusion:',d['overall'],'failed:',d['failed_items'])
assert d['overall']=='FAIL'
" || failures=$((failures + 1))

# ---- fixture root used by NC-B, NC-C and PC-D ------------------------------
fixture() {
  root=$1
  run=$2
  terminal=$3
  mkdir -p "$root/.harness/venv/bin" "$root/.harness/bin" "$root/scripts" "$run"
  printf '#!/bin/sh\nexit 0\n' > "$root/.harness/venv/bin/python"
  printf 'stub\n' > "$root/.harness/bin/formal-gates"
  printf '#!/bin/sh\nprintf '"'"'{"created_run": "%s"}\n'"'"'\n' "$run" > "$root/scripts/harness"
  chmod +x "$root/.harness/venv/bin/python" "$root/scripts/harness"
  cat > "$run/harness-result.json" <<JSON
{
  "contract_version": 1,
  "profile": "harness-v1",
  "status": "$terminal",
  "evidence_level": "fixture",
  "candidate": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
  "revision": "0000000000000000000000000000000000000000000000000000000000000000",
  "baseline": "517de91f1b480ec969bd91385c29ce38d8bc2841",
  "full_3_5c_equivalence": false,
  "checks": [{"id": "addition", "status": "PASS", "exit_code": 0, "candidate": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}],
  "gates": [{"id": "complexity-gate", "result": {"status": "PASS", "summary": "fixture", "findings": [], "check_ids": []}}]
}
JSON
  printf '{"runtime_identity": "cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe"}\n' > "$run/manifest.json"
}

# ---- NC-B: the normal flow stalls waiting for the user ----------------------
say ""
say "NC-B demo unexpectedly stalls at WAITING_USER"
B="$OUT/nc-b/root"
fixture "$B" "$OUT/nc-b/run" "WAITING_USER"
python3 "$RUNNER" --unit nc-b --root "$B" --only demo-fixture > "$OUT/nc-b.stdout" 2> "$OUT/nc-b.stderr"
verdict "NC-B" 1 $?
grep -q "terminal status was 'WAITING_USER'" "$OUT/nc-b.stdout" \
  && say "  OK   NC-B reported the terminal-status problem" \
  || { say "  BAD  NC-B did not report the terminal-status problem"; failures=$((failures + 1)); }

# ---- NC-C: repeated attempts must not overwrite each other ------------------
say ""
say "NC-C a second attempt must not overwrite the first"
C="$OUT/nc-c/root"
fixture "$C" "$OUT/nc-c/run" "WAITING_USER"
python3 "$RUNNER" --unit nc-c --root "$C" --only demo-fixture > "$OUT/nc-c-1.stdout" 2>&1
first_exit=$?
first_dir=$(ls -d "$C/.harness/validation/acceptance/nc-c/"*/ | head -1)
first_digest=$(cat "$first_dir/summary.json" | shasum -a 256 | cut -d' ' -f1)
python3 "$RUNNER" --unit nc-c --root "$C" --only demo-fixture > "$OUT/nc-c-2.stdout" 2>&1
second_exit=$?
count=$(ls -d "$C/.harness/validation/acceptance/nc-c/"*/ | wc -l | tr -d ' ')
verdict "NC-C first" 1 $first_exit
verdict "NC-C second" 1 $second_exit
if [ "$count" -ne 2 ]; then
  say "  BAD  NC-C expected 2 distinct attempt directories, found $count"
  failures=$((failures + 1))
else
  say "  OK   NC-C kept $count distinct attempt directories"
fi
if [ ! -f "$first_dir/summary.json" ]; then
  say "  BAD  NC-C first attempt summary disappeared"
  failures=$((failures + 1))
elif [ "$(cat "$first_dir/summary.json" | shasum -a 256 | cut -d' ' -f1)" != "$first_digest" ]; then
  say "  BAD  NC-C first attempt summary was rewritten"
  failures=$((failures + 1))
else
  say "  OK   NC-C first attempt evidence is byte-identical after the second run"
fi

# ---- PC-D: the same fixture with the expected terminal must pass ------------
say ""
say "PC-D a well-formed DEMO_COMPLETE fixture must be accepted"
D="$OUT/pc-d/root"
fixture "$D" "$OUT/pc-d/run" "DEMO_COMPLETE"
python3 "$RUNNER" --unit pc-d --root "$D" --only demo-fixture > "$OUT/pc-d.stdout" 2> "$OUT/pc-d.stderr"
verdict "PC-D" 0 $?

# ---- runner stream-handling and re-sign controls ---------------------------
# "No stall" is only meaningful if the monitor actually fires, so these drive the
# real streaming method with cheap stand-ins instead of waiting the production
# budget. They also cover re-signing over an already tracked record.
RUNNER_CONTROLS=$(cd "$(dirname "$0")" && pwd)/runner-controls.py
say ""
say "Runner stream-handling and re-sign controls"
python3 "$RUNNER_CONTROLS" --root "$(cd "$(dirname "$0")/../.." && pwd)" --out "$OUT/runner-controls" > "$OUT/runner-controls.stdout" 2>&1
runner_exit=$?
sed -n 's/^/  /p' "$OUT/runner-controls.stdout"
if [ "$runner_exit" -ne 0 ]; then
  say "  BAD  runner controls reported failures (exit $runner_exit)"
  failures=$((failures + 1))
fi

# ---- admission controls ----------------------------------------------------
# The admission gate has its own control module: it needs JSON fixtures and a
# clone with an unpopulated runtime, which is clearer in Python than in shell.
# Pass the real record path as the second argument.
CONTROLS=$(cd "$(dirname "$0")" && pwd)/admission-controls.py
if [ -n "${2:-}" ] && [ -f "${2:-}" ]; then
  say ""
  say "Admission controls against $2"
  python3 "$CONTROLS" --record "$2" --out "$OUT/admission" > "$OUT/admission.stdout" 2>&1
  admission_exit=$?
  sed -n 's/^/  /p' "$OUT/admission.stdout"
  if [ "$admission_exit" -ne 0 ]; then
    say "  BAD  admission controls reported failures (exit $admission_exit)"
    failures=$((failures + 1))
  fi
else
  say ""
  say "Admission controls skipped: pass the real record path as the second argument."
fi

say ""
if [ "$failures" -eq 0 ]; then
  say "CONTROLS PASSED ($OUT)"
  exit 0
fi
say "CONTROLS FAILED: $failures unexpected result(s) ($OUT)"
exit 1
