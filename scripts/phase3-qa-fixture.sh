#!/bin/sh
# Prepare a reproducible phase-3 black-box fixture.  This script only creates
# the isolated project, performs the documented install/bootstrap operations,
# and materializes the launcher-supplied intake receipt.  Assertions remain in
# the black-box procedure and are executed through the candidate CLI.
set -eu

usage() {
  cat >&2 <<'EOF'
usage: phase3-qa-fixture.sh prepare --project-root DIR --home DIR --source DIR --candidate BIN --receipt PATH [--old-confirmed-at]
       phase3-qa-fixture.sh mutate --input PATH --output PATH --field FIELD
       phase3-qa-fixture.sh mutate-envelope --input-project DIR --output-project DIR --run-id ID --field FIELD

prepare fields: source, authority, transport, requirementSource,
requirementRevision, artifacts, solutionRevision, solutionDigest, confirmedAt.
The receipt is written only
to the requested fixture path; no stable workflow state is modified after
the documented confirmation step.
EOF
  exit 2
}

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

prepare() {
  project= home= source= candidate= receipt= old_confirmed=0
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --project-root) project=$2; shift 2 ;;
      --home) home=$2; shift 2 ;;
      --source) source=$2; shift 2 ;;
      --candidate) candidate=$2; shift 2 ;;
      --receipt) receipt=$2; shift 2 ;;
      --old-confirmed-at) old_confirmed=1; shift ;;
      *) usage ;;
    esac
  done
  [ -n "$project" ] && [ -n "$home" ] && [ -n "$source" ] && [ -n "$candidate" ] && [ -n "$receipt" ] || usage
  mkdir -p "$project" "$home"
  # The repository may contain a generated bin/formal-gates from an older
  # snapshot. Stage a private package and rebuild that payload so bootstrap's
  # installed-binary smoke exercises the same candidate code as the launcher.
  staged="$home/phase3-source"
  mkdir -p "$staged"
  cp -R "$source"/. "$staged"/
  mkdir -p "$staged/bin"
  (cd "$source" && go build -o "$staged/bin/formal-gates" ./cmd/formal-gates)
  # Install maintenance is only accepted through the stable launcher path.
  # Keep the candidate target under test separate, while making the launcher
  # explicit inside this isolated HOME.
  stable_launcher="$home/.local/bin/formal-gates"
  mkdir -p "$(dirname "$stable_launcher")"
  cp "$candidate" "$stable_launcher"
  chmod +x "$stable_launcher"
  printf '%s\n' '# phase3 fixture requirement' > "$project/requirements.md"
  printf '%s\n' '# phase3 fixture solution' > "$project/design.md"
  printf '%s\n' '.gates/ .formal-gates-resources/ .codex/' > "$project/.gitignore"
  git -C "$project" init -q
  git -C "$project" config user.email phase3-fixture@example.invalid
  git -C "$project" config user.name phase3-fixture
  git -C "$project" add requirements.md design.md .gitignore
  git -C "$project" commit -qm phase3-fixture

  env HOME="$home" USERPROFILE="$home" CODEX_HOME= "$stable_launcher" install --source "$staged" --host codex --scope project --project "$project" --skip-hooks --force >/dev/null
  env HOME="$home" USERPROFILE="$home" CODEX_HOME= "$stable_launcher" install --source "$staged" --host codex --scope project --project "$project" --binary-target "$stable_launcher" --bootstrap --skip-hooks --force >/dev/null
  package_root="$project/.codex/skills/formal-gates"
  run_id=phase3-fixture-legacy
  env HOME="$home" USERPROFILE="$home" CODEX_HOME= "$stable_launcher" workflow start --root "$project" --package-root "$package_root" --run-id "$run_id" --requirement requirements.md --requirement-artifact design.md --vcs git --split no >/dev/null
  env HOME="$home" USERPROFILE="$home" CODEX_HOME= "$stable_launcher" workflow prepare-action --root "$project" --package-root "$package_root" --run-id "$run_id" --action requirements-clarification >/dev/null
  dispatch=$(python3 - "$project" "$run_id" <<'PY'
import json, pathlib, sys
project, run_id = sys.argv[1:]
state = json.loads((pathlib.Path(project) / '.gates' / 'tmp' / run_id / 'state.json').read_text())
print(next(iter(state['dispatches'])))
PY
  )
  env HOME="$home" USERPROFILE="$home" CODEX_HOME= "$stable_launcher" workflow record-action --root "$project" --package-root "$package_root" --run-id "$run_id" --action requirements-clarification --dispatch "$dispatch" --status PASS >/dev/null
  env HOME="$home" USERPROFILE="$home" CODEX_HOME= "$stable_launcher" workflow requirement --root "$project" --package-root "$package_root" --run-id "$run_id" --confirmed >/dev/null

  mkdir -p "$(dirname "$receipt")"
  python3 - "$project" "$home" "$run_id" "$receipt" "$old_confirmed" <<'PY'
import hashlib, json, pathlib, sys
project, home, run_id, receipt, old = sys.argv[1:]
state_path = pathlib.Path(project) / '.gates' / 'tmp' / run_id / 'state.json'
state = json.loads(state_path.read_text())
artifacts = state.get('requirementArtifacts', [])
if not artifacts:
    raise SystemExit('fixture: confirmed run has no requirement artifacts')
def digest(path):
    data = pathlib.Path(project, path).read_bytes().replace(b'\r\n', b'\n')
    return hashlib.sha256(data).hexdigest()
req_source = state['requirementSource']
req_rev = state['requirementRevision']
solution = next((a for a in artifacts if a['path'] != req_source), artifacts[0])
solution_path = solution['path']
receipt_obj = {
    'source': 'stable-driver',
    'authority': 'stable-driver',
    'transport': 'stable-launcher',
    'requirementSource': req_source,
    'requirementRevision': req_rev,
    'artifacts': [{'path': a['path'], 'revision': a['revision']} for a in artifacts],
    'solutionRevision': solution['revision'],
    'solutionDigest': 'sha256:' + digest(solution_path),
    'confirmedAt': '2000-01-01T00:00:00Z' if old == '1' else '2026-08-29T00:00:00Z',
}
path = pathlib.Path(receipt)
path.write_text(json.dumps(receipt_obj, sort_keys=True, indent=2) + '\n')
stable_launcher = str(pathlib.Path(home, '.local/bin/formal-gates'))
print(json.dumps({'projectRoot': project, 'packageRoot': str(pathlib.Path(project, '.codex/skills/formal-gates')), 'receipt': receipt, 'runId': run_id, 'candidate': stable_launcher, 'candidateStablePath': stable_launcher, 'stableLauncher': stable_launcher}, sort_keys=True))
PY
}

mutate() {
  input= output= field=
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --input) input=$2; shift 2 ;;
      --output) output=$2; shift 2 ;;
      --field) field=$2; shift 2 ;;
      *) usage ;;
    esac
  done
  [ -n "$input" ] && [ -n "$output" ] && [ -n "$field" ] || usage
  python3 - "$input" "$output" "$field" <<'PY'
import json, pathlib, sys
src, dst, field = sys.argv[1:]
obj = json.loads(pathlib.Path(src).read_text())
if field == 'source': obj['source'] = 'wrong-source'
elif field == 'requirementRevision': obj['requirementRevision'] = 'wrong-revision'
elif field == 'solutionRevision': obj['solutionRevision'] = 'wrong-solution'
elif field == 'solutionDigest': obj['solutionDigest'] = 'sha256:' + '0' * 64
elif field == 'artifactRevision': obj['artifacts'][0]['revision'] = 'wrong-artifact'
elif field == 'artifactSet': obj['artifacts'] = list(reversed(obj['artifacts'])) + [{'path': 'extra.md', 'revision': 'wrong-extra'}]
elif field == 'authority': obj['authority'] = 'unregistered-authority'
elif field == 'transport': obj['transport'] = 'non-fixed-transport'
else: raise SystemExit('fixture: unknown mutation field ' + field)
pathlib.Path(dst).write_text(json.dumps(obj, sort_keys=True, indent=2) + '\n')
PY
}

mutate_envelope() {
  input_project= output_project= run_id= field=
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --input-project) input_project=$2; shift 2 ;;
      --output-project) output_project=$2; shift 2 ;;
      --run-id) run_id=$2; shift 2 ;;
      --field) field=$2; shift 2 ;;
      *) usage ;;
    esac
  done
  [ -n "$input_project" ] && [ -n "$output_project" ] && [ -n "$run_id" ] && [ -n "$field" ] || usage
  mkdir -p "$output_project"
  cp -R "$input_project"/. "$output_project"/
  python3 - "$output_project" "$run_id" "$field" <<'PY'
import json, pathlib, sys
project, run_id, field = sys.argv[1:]
state_path = pathlib.Path(project) / '.gates' / 'engine' / run_id / 'state.json'
obj = json.loads(state_path.read_text())
if field not in {'writer', 'stateSchemaVersion', 'workflowDefinitionVersion', 'definitionSource', 'definitionDigest', 'packageDigest', 'installedTargetIdentity'}:
    raise SystemExit('fixture: unknown envelope field ' + field)
if field == 'writer': obj.pop('writer', None)
elif field == 'stateSchemaVersion': obj['stateSchemaVersion'] = '0'
elif field == 'workflowDefinitionVersion': obj['workflowDefinitionVersion'] = '0'
elif field == 'definitionSource': obj['definitionSource'] = 'wrong-definition.json'
elif field == 'definitionDigest': obj['definitionDigest'] = 'sha256:' + '0' * 64
elif field == 'packageDigest': obj['packageDigest'] = 'sha256:' + '0' * 64
elif field == 'installedTargetIdentity': obj['installedTargetIdentity'] = 'wrong-installed-target'
state_path.write_text(json.dumps(obj, sort_keys=True, indent=2) + '\n')
print(json.dumps({'runRoot': str(pathlib.Path(project)), 'runId': run_id, 'mutatedPath': str(state_path), 'field': field}, sort_keys=True))
PY
}

[ "$#" -gt 0 ] || usage
command=$1
shift
case "$command" in
  prepare) prepare "$@" ;;
  mutate) mutate "$@" ;;
  mutate-envelope) mutate_envelope "$@" ;;
  *) usage ;;
esac
