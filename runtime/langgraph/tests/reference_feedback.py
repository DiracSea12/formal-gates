"""Pre-migration reference copy of task_input and evidence_feedback.

Test-only. Production must not import this module. The bodies below are the
migration-PRIOR implementations of task_input and evidence_feedback, byte for
byte, except that `digest` is taken from reference_canonical so the canonical
bytes being diffed are truly the pre-migration format (the live infra.digest is
now backed by Go and is the thing under test).
"""
from formal_gates_harness.infra import HarnessError
from reference_canonical import digest


def evidence_feedback(store, ref: str, purpose: str) -> dict:
    value = store.verify_evidence(ref)
    if value.get('revision', store.manifest['revision']) != store.manifest['revision']:
        raise HarnessError('FEEDBACK_BINDING: evidence is from a different requirement')
    return {'kind': 'evidence-data/v1', 'authority': 'none', 'purpose': purpose,
            'source': {'artifact': ref, 'role': value.get('role', 'verification'),
                       'revision': store.manifest['revision']},
            'content': value.get('result', value)}


def task_input(*, role, revision, base, candidate, checks, feedback):
    # Nothing from feedback is merged into bindings, tools, check commands or decisions.
    wrapped = None if feedback is None else {
        'kind': 'untrusted-feedback/v1', 'authority': 'none',
        'sha256': digest(feedback), 'content': feedback}
    return {'profile': 'harness-v1', 'role': role, 'revision': revision,
            'base': base, 'candidate': candidate, 'checks': checks, 'feedback': wrapped,
            'control': {'can_approve': False, 'can_choose_next_stage': False,
                        'can_edit_state': False, 'result_schema_only': True}}
