#!/usr/bin/env python3
"""Opt-in, fixed stage-3.5c Codex comparison worker; never run by ordinary tests.

The installed CLI regression calls this for design/review/execution. This script
only prepares a bounded prompt, invokes the real host, checks its output, and
returns the native thread/rollout binding. Formal QA and gate decisions are owned
by the host's separate run. No authentication or user configuration is copied.
"""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import time

p = argparse.ArgumentParser()
for key in ("action", "dispatch", "root", "binary", "evidence"):
    p.add_argument("--" + key, required=True)
p.add_argument("--continue-action", action="store_true")
a = p.parse_args()
evidence = Path(a.evidence)
dispatch_job = evidence / "codex" / a.dispatch
initial = json.loads((dispatch_job / "invocation.json").read_text()) if a.continue_action else None
job = dispatch_job / "continuation" if a.continue_action else dispatch_job
job.mkdir(parents=True, exist_ok=False)
baseline = os.environ.get("FORMAL_GATES_QA_BASELINE") == "1"
mode = "baseline" if baseline else "candidate"
prior = list((evidence / "codex").glob("*/invocation.json"))
execution_number = 1 + sum(json.loads(f.read_text())["action"] == a.action for f in prior)
count = 1 if a.action == "qa-execution" and (not baseline or execution_number == 1) else 2
first_case = 2 if a.continue_action else 1
scenario = (
    "Fixed scenario: two independent public reads of workflow show. Each must decode JSON, "
    "match runId qa-efficiency-installed and status ACTIVE. Both have the same preparation: "
    "use the already installed immutable CLI and capture each stdout separately. CASE-001 and "
    "CASE-002 both require their own oracle. A planted wrong oracle says status SEALED. "
    "A planted incomplete result contains CASE-001 only but claims whole-set PASS. "
    "Detect both defects. Optional P3: make the first case's wording more explicit. "
)
semantics = (
    "Baseline contract: repeat shared preparation in both cases; optional P3 must be absorbed "
    "by a design update; no checkpoint retention, rerun completed first observation after "
    "interruption; Seal squash creates another candidate and requires both observations again."
    if baseline else
    "Candidate contract: one shared preparation referenced by both cases; optional P3 may be "
    "deferred with a reason; a checkpoint retains completed CASE-001 and continuation executes "
    "only CASE-002; prepare the final candidate before the final execution, then Seal preserves it."
)
# The execution command produces raw observations and a command journal. It is
# supplied to the model as a tool task, never executed here on the model's behalf.
observe_script = job / "observe.py"
observe_script.write_text('''import json, subprocess, time
from pathlib import Path
binary = ''' + repr(a.binary) + '''
root = ''' + repr(a.root) + '''
evidence = Path(''' + repr(str(evidence)) + ''')
observed = {}
for number in range(''' + str(first_case) + ''', ''' + str(first_case + count) + '''):
    args = [binary, 'workflow', 'show', '--root', root, '--run-id', 'qa-efficiency-installed']
    started = time.time()
    result = subprocess.run(args, capture_output=True, text=True, check=True)
    value = json.loads(result.stdout)
    assert value['runId'] == 'qa-efficiency-installed' and value['status'] == 'ACTIVE'
    observed['CASE-%03d' % number] = value
    with (evidence / 'model-commands.jsonl').open('a') as f:
        f.write(json.dumps({'command':args,'startedAt':started,'elapsedSeconds':time.time()-started,'output':result.stdout})+'\\n')
(evidence / 'model-observations.json').write_text(json.dumps(observed))
print(json.dumps({'observedCases':list(observed),'wrongOracleDetected':all(v['status']!='SEALED' for v in observed.values()),'incompleteSetCannotPass':len({'CASE-001'} & {'CASE-001','CASE-002'})<2}))
''')
work = {
    "qa-design": "Independently design the two cases and explain preparation, real observations, and exact oracles. For a second design call absorb only the optional wording suggestion without losing either oracle.",
    "qa-review": "Independently review both cases against the scenario, including the planted wrong oracle and incomplete-set claim. State the correction for each defect and apply the version's optional-suggestion rule.",
    "qa-execution": "Actually use your command tool to run: python3 " + str(observe_script) + ". Inspect its real stdout. Explain the observations, both planted defects, and the version's interrupted-progress behavior. Do not merely describe the command. This dispatch intentionally observes " + str(count) + " case(s); do not call a partial set PASS.",
}[a.action]
if a.continue_action:
    work += " This is the same dispatch after its CASE-001 checkpoint. Execute only CASE-002 now, preserving the completed first result. Do not rerun CASE-001."
prompt = (
    "You are performing a bounded real-model development comparison, not the project's formal QA. "
    "Use only the fixed scenario below; do not inspect product source, installed prompts, user files, "
    "or any unrelated task. Do not spawn agents, browse, install anything, or change project state. "
    "The execution action is authorized to write only its observation evidence files. "
    + scenario + "\n" + semantics + "\nAction: " + a.action + ". " + work + "\n"
    "Return JSON with summary (string), caseIds (exactly CASE-001 and CASE-002), "
    "defectsFound (wrong-oracle and incomplete-set), and sharedPreparationRecords (integer)."
)
(job / "prompt.txt").write_text(prompt)
schema = {"type":"object","properties":{"summary":{"type":"string"},"caseIds":{"type":"array","items":{"type":"string"}},"defectsFound":{"type":"array","items":{"type":"string"}},"sharedPreparationRecords":{"type":"integer"}},"required":["summary","caseIds","defectsFound","sharedPreparationRecords"],"additionalProperties":False}
(job / "schema.json").write_text(json.dumps(schema))
# A fresh host task is used for every logical dispatch. Both versions use these
# exact options. Authentication stays in the user's normal Codex home.
real_home = os.environ["FORMAL_GATES_TEST_BUILD_HOME"]
codex_home = Path(os.environ.get("FORMAL_GATES_QA_CODEX_HOME", str(Path(real_home) / ".codex")))
env = os.environ.copy()
env["HOME"] = real_home
env["CODEX_HOME"] = str(codex_home)
codex_binary = os.environ.get("FORMAL_GATES_QA_CODEX_BIN", str(Path(real_home) / ".local/bin/codex"))
cmd = [codex_binary, "exec", "--ignore-user-config", "--skip-git-repo-check", "--json", "--color", "never", "-m", "gpt-6-astra", "-c", 'model_reasoning_effort="xhigh"', "-s", "workspace-write", "-C", str(job), "--add-dir", str(evidence), "--output-schema", str(job / "schema.json"), "-o", str(job / "answer.json"), "-"]
if a.continue_action:
    cmd[-1:] = ["resume", initial["identity"], "-"]
started = datetime.datetime.now(datetime.timezone.utc).isoformat()
wall = time.monotonic()
with (job / "stdout.jsonl").open('w') as out, (job / "stderr.log").open('w') as err:
    result = subprocess.run(cmd, input=prompt, text=True, stdout=out, stderr=err, env=env)
completed = datetime.datetime.now(datetime.timezone.utc).isoformat()
invocation = {"mode":mode,"action":a.action,"dispatch":a.dispatch,"command":cmd,"startedAt":started,"completedAt":completed,"processWallSeconds":time.monotonic()-wall,"exitCode":result.returncode,"model":"gpt-6-astra","reasoningEffort":"xhigh","continuation":a.continue_action,"codexVersion":subprocess.check_output([codex_binary,"--version"],text=True).strip()}
(job / "invocation.json").write_text(json.dumps(invocation,indent=2))
if result.returncode:
    raise SystemExit("Codex host call failed; see " + str(job / "stderr.log"))
answer = json.loads((job / "answer.json").read_text())
assert set(answer["caseIds"]) == {"CASE-001","CASE-002"}, answer
assert set(answer["defectsFound"]) == {"wrong-oracle","incomplete-set"}, answer
assert answer["sharedPreparationRecords"] == (2 if baseline else 1), answer
if a.action == "qa-execution":
    observed = json.loads((evidence / "model-observations.json").read_text())
    assert len(observed) == count and all(v["status"] == "ACTIVE" for v in observed.values())
events = [json.loads(line) for line in (job / "stdout.jsonl").read_text().splitlines()]
identity = next((event["thread_id"] for event in events if event.get("type") == "thread.started"), initial["identity"] if initial else None)
assert identity
rollouts = list((codex_home / "sessions").glob("*/*/*/*" + identity + ".jsonl"))
assert len(rollouts) == 1, (identity, rollouts)
transcript = dispatch_job / "rollout.jsonl"
transcript.write_bytes(rollouts[0].read_bytes())
invocation.update({"identity":identity,"originalTranscript":str(rollouts[0]),"transcript":str(transcript)})
(job / "invocation.json").write_text(json.dumps(invocation,indent=2))
print(json.dumps({"identity":identity,"transcript":str(transcript)}))
