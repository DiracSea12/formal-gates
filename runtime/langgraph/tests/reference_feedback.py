"""Independent test oracle for task-input and evidence-feedback wrappers.

Production code must not import this module. Digests use reference_canonical."""
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
