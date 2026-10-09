"""Independent test oracle for materialize_comparison and comparison.json encoding.

Production code must not import this module. Encoding uses reference_canonical."""
from pathlib import Path
import subprocess
from formal_gates_harness.infra import RuntimeBlocked, file_proof
from reference_canonical import canonical


def materialize_comparison(workspace, materials, base, candidate):
    """Export the exact Git delta for reviewers without a command tool.

    Blob copies are regular files, including symlink target text. Gitlinks carry
    commit identities only: their nested repository contents are not available.
    """
    directory = materials / 'comparison'
    files = {}

    def git_bytes(*args):
        process = subprocess.run(['git', '-C', str(workspace), *args], capture_output=True)
        if process.returncode:
            raise RuntimeBlocked('COMPARISON_INPUT: ' + process.stderr.decode(errors='replace').strip())
        return process.stdout

    def save(name, body):
        path = directory / name
        if path.exists():
            if path.read_bytes() != body:
                raise RuntimeBlocked('Saved comparison material changed: ' + name)
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(body)
        files[name] = file_proof(path)

    options = ('--no-ext-diff', '--no-textconv', '--find-renames')
    save('full.diff', git_bytes('diff', *options, '--binary', '--full-index', base, candidate, '--'))
    raw = git_bytes('diff', *options, '--raw', '--no-abbrev', '-z', base, candidate, '--')
    records = iter(raw.split(b'\0')[:-1])
    changes = []
    for header in records:
        before_mode, after_mode, before_oid, after_oid, status = header.decode().split()
        before_mode = before_mode.removeprefix(':')
        old_path = next(records).decode()
        new_path = next(records).decode() if status[0] in ('R', 'C') else old_path
        change = {'status':status}
        for side, mode, oid, name in (('base', before_mode, before_oid, old_path),
                                      ('candidate', after_mode, after_oid, new_path)):
            if mode == '000000':
                change[side] = None
                continue
            entry = {'path':name, 'mode':mode, 'object':oid}
            if mode == '160000':
                entry['note'] = 'Gitlink only; nested repository content is not included.'
            else:
                entry['material'] = side + '/' + name
                save(entry['material'], git_bytes('cat-file', 'blob', oid))
            change[side] = entry
        changes.append(change)
    save('comparison.json', canonical({'base':base, 'candidate':candidate, 'diff':'full.diff', 'changes':changes}))
    return {'base':base, 'candidate':candidate, 'directory':str(directory), 'files':files}
