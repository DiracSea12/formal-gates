"""End-to-end differential: the five G3a domain judgements vs pre-migration reference.

Every case calls the real Go binary through driver_bridge().call(op, payload)
and compares the WHOLE answer, canonically, with reference_domain (the lifted
pre-migration logic of graph.py). The corpus is deliberately discriminating:
each operation has at least one row that pins an order, a priority or a
boundary, so a Go implementation that sorted the projections, reordered the
branches or moved a boundary by one turns this differential red.
"""
import unittest

from formal_gates_harness.infra import canonical, driver_bridge
import reference_domain


class DomainJudgmentsDifferential(unittest.TestCase):
    def assertSame(self, op, payload, reference_answer, label):
        answered = driver_bridge().call(op, payload)
        self.assertEqual(canonical(answered), canonical(reference_answer),
                         msg=f"{label}: {op} answer differs\nreference={reference_answer!r}\nbridge={answered!r}")
        return answered

    # ---- evaluate-route ---------------------------------------------------

    def test_evaluate_route_empty_collections_seal(self):
        self.assertSame('evaluate-route', {'checks': [], 'gates': []},
                        reference_domain.evaluate_route([], []), 'empty')

    def test_evaluate_route_only_p3_seals(self):
        checks = [{'id': 'c1', 'status': 'PASS'}]
        gates = [{'gate': 'complexity-gate',
                  'result': {'status': 'PASS', 'summary': 's', 'check_ids': [],
                             'findings': [{'id': 'f1', 'severity': 'P3', 'message': 'm', 'paths': []},
                                          {'id': 'f2', 'severity': 'P3', 'message': 'm', 'paths': ['x']}]}}]
        answered = self.assertSame('evaluate-route', {'checks': checks, 'gates': gates},
                                   reference_domain.evaluate_route(checks, gates), 'only-p3')
        self.assertEqual(answered['route'], 'seal')
        self.assertEqual(answered['failed_checks'], [])
        self.assertEqual(answered['blocking_gates'], [])

    def test_evaluate_route_order_is_input_order_not_sorted(self):
        # Failures and blocking gates are deliberately named so that input order
        # differs from sorted order: a Go side that sorted either projection
        # would return ['aa-check', ...] / ['aa-gate', ...] and go red here.
        checks = [{'id': 'zz-check', 'status': 'FAIL'},
                  {'id': 'aa-check', 'status': 'FAIL'},
                  {'id': 'mm-check', 'status': 'PASS'}]
        gates = [{'gate': 'zz-gate',
                  'result': {'status': 'PASS', 'summary': 's', 'check_ids': [],
                             'findings': [{'id': 'f1', 'severity': 'P2', 'message': 'm', 'paths': ['a']}]}},
                 {'gate': 'aa-gate',
                  'result': {'status': 'FAIL', 'summary': 's', 'check_ids': [],
                             'findings': [{'id': 'f2', 'severity': 'P0', 'message': 'm', 'paths': []}]}},
                 {'gate': 'mm-gate',
                  'result': {'status': 'PASS', 'summary': 's', 'check_ids': [],
                             'findings': [{'id': 'f3', 'severity': 'P3', 'message': 'm', 'paths': []}]}}]
        answered = self.assertSame('evaluate-route', {'checks': checks, 'gates': gates},
                                   reference_domain.evaluate_route(checks, gates), 'order-pinned')
        self.assertEqual(answered['route'], 'repair')
        self.assertEqual(answered['failed_checks'], ['zz-check', 'aa-check'])
        self.assertEqual(answered['blocking_gates'], ['zz-gate', 'aa-gate'])

    def test_evaluate_route_pass_with_p2_blocks_without_failed_checks(self):
        checks = [{'id': 'c1', 'status': 'PASS'}]
        gates = [{'gate': 'complexity-gate',
                  'result': {'status': 'PASS', 'summary': 's', 'check_ids': [],
                             'findings': [{'id': 'f1', 'severity': 'P2', 'message': 'm', 'paths': []}]}}]
        answered = self.assertSame('evaluate-route', {'checks': checks, 'gates': gates},
                                   reference_domain.evaluate_route(checks, gates), 'pass-with-p2')
        self.assertEqual(answered['route'], 'repair')
        self.assertEqual(answered['failed_checks'], [])

    def test_evaluate_route_failed_check_blocks_with_clean_gates(self):
        checks = [{'id': 'c1', 'status': 'FAIL'}]
        gates = [{'gate': 'complexity-gate',
                  'result': {'status': 'PASS', 'summary': 's', 'check_ids': [], 'findings': []}}]
        answered = self.assertSame('evaluate-route', {'checks': checks, 'gates': gates},
                                   reference_domain.evaluate_route(checks, gates), 'failed-only')
        self.assertEqual(answered['route'], 'repair')
        self.assertEqual(answered['blocking_gates'], [])

    # ---- worker-outcome ---------------------------------------------------

    def test_worker_outcome_matrix(self):
        rows = [
            # CONTINUE and BLOCKED are status facts: they win over needs_repair.
            ('development', 'CONTINUE', True, 'continue'),
            ('repair', 'BLOCKED', True, 'blocked'),
            ('development', 'CONTINUE', False, 'continue'),
            ('repair', 'BLOCKED', False, 'blocked'),
            # needs_repair only forces a retry for development/repair.
            ('development', 'PASS', True, 'unresolved-findings'),
            ('repair', 'FAIL', True, 'unresolved-findings'),
            ('qa-review', 'PASS', True, 'accept'),
            ('complexity-gate', 'FAIL', True, 'accept'),
            ('product-review', 'FAIL', True, 'accept'),
            ('development', 'PASS', False, 'accept'),
            ('qa-review', 'PASS', False, 'accept'),
        ]
        for role, status, needs_repair, want in rows:
            payload = {'role': role, 'status': status, 'needs_repair': needs_repair}
            answered = self.assertSame('worker-outcome', payload,
                                       reference_domain.worker_outcome(role, status, needs_repair),
                                       f'{role}/{status}/{needs_repair}')
            self.assertEqual(answered['action'], want)

    # ---- check-retention, both phases ---------------------------------------

    def test_check_retention_phase_one(self):
        rows = [
            ('no-old', 'b', None, 'run'),
            ('binding-mismatch', 'b', {'binding': 'other', 'status': 'PASS', 'candidate': 'c1'}, 'run'),
            ('unsettled-status', 'b', {'binding': 'b', 'status': 'BLOCKED', 'candidate': 'c1'}, 'run'),
            ('pass-eligible', 'b', {'binding': 'b', 'status': 'PASS', 'candidate': 'c1'}, 'verify'),
            ('fail-eligible', 'b', {'binding': 'b', 'status': 'FAIL', 'candidate': 'c1'}, 'verify'),
        ]
        for label, binding, old, want in rows:
            payload = {'binding': binding, 'old': old}
            answered = self.assertSame('check-retention', payload,
                                       reference_domain.check_retention(binding, old), label)
            self.assertEqual(answered['action'], want)

    def _two_phase(self, label, binding, old, candidate, attempt, evidence_valid):
        """Run the pair the way one_check does: eligibility first, then the verdict."""
        first = self.assertSame('check-retention', {'binding': binding, 'old': old},
                                reference_domain.check_retention(binding, old), label + '/phase1')
        self.assertEqual(first['action'], 'verify')
        payload = {'binding': binding, 'old': old, 'candidate': candidate,
                   'attempt': attempt, 'evidence_valid': evidence_valid}
        return self.assertSame('check-retention', payload,
                               reference_domain.check_retention(binding, old, candidate, attempt, evidence_valid),
                               label + '/phase2')

    def test_check_retention_retain_same_candidate(self):
        answered = self._two_phase('retain-same', 'b', {'binding': 'b', 'status': 'PASS', 'candidate': 'c1'},
                                   'c1', 0, True)
        self.assertEqual(answered['action'], 'retain')
        self.assertEqual(answered['inherited_from'], '')

    def test_check_retention_retain_moved_candidate_records_inheritance(self):
        answered = self._two_phase('retain-moved', 'b', {'binding': 'b', 'status': 'FAIL', 'candidate': 'old-head'},
                                   'new-head', 3, True)
        self.assertEqual(answered['action'], 'retain')
        self.assertEqual(answered['inherited_from'], 'old-head')

    def test_check_retention_invalid_evidence_first_attempt_fails(self):
        answered = self._two_phase('fail-first', 'b', {'binding': 'b', 'status': 'PASS', 'candidate': 'c1'},
                                   'c1', 0, False)
        self.assertEqual(answered['action'], 'fail')

    def test_check_retention_invalid_evidence_retry_reruns_with_reason(self):
        for attempt in (1, 2):
            answered = self._two_phase(f'invalid-retry-{attempt}', 'b',
                                       {'binding': 'b', 'status': 'PASS', 'candidate': 'c1'},
                                       'c2', attempt, False)
            self.assertEqual(answered['action'], 'run')
            self.assertEqual(answered['reason'], 'check-evidence-invalid')

    # ---- repair-allowance ---------------------------------------------------

    def test_repair_allowance_matrix(self):
        rows = [
            # Null allowance falls back to the spec default; equality already asks.
            (0, None, 2, 2, False),
            (2, None, 2, 2, True),
            (5, None, 3, 3, True),
            # An explicit allowance beats the spec limit in both directions.
            (2, 3, 2, 3, False),
            (3, 3, 2, 3, True),
            (1, 1, 100, 1, True),
        ]
        for round_, allowance, limit, want_allowance, want_ask in rows:
            payload = {'round': round_, 'allowance': allowance, 'repair_limit': limit}
            answered = self.assertSame('repair-allowance', payload,
                                       reference_domain.repair_allowance(round_, allowance, limit),
                                       f'round={round_}/allowance={allowance}/limit={limit}')
            self.assertEqual(answered['allowance'], want_allowance)
            self.assertEqual(answered['ask_user'], want_ask)

    # ---- candidate-admission ------------------------------------------------

    def test_candidate_admission_four_combinations(self):
        rows = [
            (True, True, 'Unresolved development result cannot create a verification candidate'),
            (True, False, 'Unresolved development result cannot create a verification candidate'),
            (False, True, 'QA not approved'),
            (False, False, None),
        ]
        for dev, qa, want_refusal in rows:
            payload = {'development_needs_repair': dev, 'qa_review_needs_repair': qa}
            answered = self.assertSame('candidate-admission', payload,
                                       reference_domain.candidate_admission(dev, qa), f'dev={dev}/qa={qa}')
            if want_refusal is None:
                self.assertTrue(answered['ok'])
            else:
                self.assertFalse(answered['ok'])
                self.assertEqual(answered['kind'], 'binding')
                self.assertEqual(answered['refusal'], want_refusal)


if __name__ == '__main__':
    unittest.main()
