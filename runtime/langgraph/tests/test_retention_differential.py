"""End-to-end differential: the ten G3b retention judgements vs pre-migration reference.

Every case calls the real Go binary through driver_bridge().call(op, payload)
and compares the WHOLE answer, canonically, with reference_retention (the
lifted pre-migration logic of retention.py at HEAD f287033). The corpus is
deliberately discriminating: each operation has at least one row that pins an
order, a priority or a boundary — SKIP before the symlink refusal, the
duplicate-members phase before any manifest work, the member-set check before
path safety and content, KEEP filtering with the (-depth, name) removal sort,
the checkpoints.sqlite logical-hash special case and component-wise path
containment — so a Go implementation that sorted the projections, reordered
the branches or moved a boundary by one turns this differential red.
"""
import unittest

from formal_gates_harness.infra import canonical, driver_bridge
import reference_retention as reference

PROOF_A = {'sha256': 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'size': 10}
PROOF_B = {'sha256': 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'size': 20}
PROOF_DB_LIVE = {'sha256': 'live-bytes-hash', 'size': 9}
PROOF_DB_BACKUP = {'sha256': 'backup-bytes-hash', 'size': 9}


class RetentionDifferential(unittest.TestCase):
    def assertSame(self, op, payload, reference_answer, label):
        answered = driver_bridge().call(op, payload)
        self.assertEqual(canonical(answered), canonical(reference_answer),
                         msg=f"{label}: {op} answer differs\nreference={reference_answer!r}\nbridge={answered!r}")
        return answered

    # ---- archive-inventory ---------------------------------------------------

    def test_inventory_skip_beats_symlink_and_walked_order_is_kept(self):
        # runner.lock is exactly a symlink here and must NOT refuse: SKIP runs
        # before the symlink judgement. The collected order is the walked
        # order, not sorted order.
        entries = [{'name': 'runner.lock', 'kind': 'symlink'},
                   {'name': 'checkpoints.sqlite-wal', 'kind': 'other'},
                   {'name': 'archive.json', 'kind': 'symlink'},
                   {'name': 'zeta.txt', 'kind': 'file'},
                   {'name': 'alpha', 'kind': 'dir'},
                   {'name': 'a/child.txt', 'kind': 'file'}]
        answered = self.assertSame('archive-inventory', {'entries': entries},
                                   reference.archive_inventory(entries), 'skip-first')
        self.assertEqual(answered['files'], ['zeta.txt', 'a/child.txt'])

    def test_inventory_symlink_and_non_regular_refusals(self):
        entries = [{'name': 'outside', 'kind': 'symlink'}]
        answered = self.assertSame('archive-inventory', {'entries': entries},
                                   reference.archive_inventory(entries), 'symlink')
        self.assertEqual(answered['refusal'],
                         'ARCHIVE_UNSUPPORTED: symlink outside; no files deleted')
        entries = [{'name': 'jobs/x', 'kind': 'symlink'}, {'name': 'fifo', 'kind': 'other'}]
        # The symlink refusal names the FIRST offending entry in walked order.
        answered = self.assertSame('archive-inventory', {'entries': entries},
                                   reference.archive_inventory(entries), 'symlink-order')
        self.assertEqual(answered['refusal'],
                         'ARCHIVE_UNSUPPORTED: symlink jobs/x; no files deleted')
        entries = [{'name': 'fifo', 'kind': 'other'}]
        answered = self.assertSame('archive-inventory', {'entries': entries},
                                   reference.archive_inventory(entries), 'non-regular')
        self.assertEqual(answered['refusal'], 'ARCHIVE_UNSUPPORTED: non-regular entry fifo')

    # ---- archive-destination ---------------------------------------------------

    def test_destination_component_containment(self):
        rows = [
            ('/runs/r', '/runs/r/archives/x.zip', 'inside-deeper'),
            ('/runs/r', '/runs/r', 'equal-counts-inside'),
            ('/runs/r', '/runs/r-2/x.zip', 'sibling-prefix-not-inside'),
            ('/runs/r', '/elsewhere/x.zip', 'outside'),
        ]
        for directory, destination, label in rows:
            answered = self.assertSame('archive-destination',
                                       {'directory': directory, 'destination': destination},
                                       reference.archive_destination(directory, destination), label)
            inside = label.startswith('inside') or label == 'equal-counts-inside'
            self.assertEqual(answered.get('ok'), not inside)

    # ---- archive-settled ---------------------------------------------------

    def test_settled_status_first_then_job_rules_in_order(self):
        # A nonterminal status wins over an unsettled job: the status check
        # runs first, exactly as before.
        unsettled = [{'name': 'j1', 'has_request': True, 'has_exit': False, 'processes': []}]
        answered = self.assertSame('archive-settled', {'status_name': 'PAUSED', 'jobs': unsettled},
                                   reference.archive_settled('PAUSED', unsettled), 'status-first')
        self.assertEqual(answered['refusal'],
                         'RUN_NOT_TERMINAL: active or paused run cannot be archived/pruned')
        for status in ('HARNESS_COMPLETE', 'DEMO_COMPLETE', 'ABORTED'):
            answered = self.assertSame('archive-settled', {'status_name': status, 'jobs': []},
                                       reference.archive_settled(status, []), f'terminal-{status}')
            self.assertTrue(answered['ok'])

    def test_settled_job_branches(self):
        settled_job = {'name': 'j1', 'has_request': True, 'has_exit': True, 'processes': []}
        answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [settled_job]},
                                   reference.archive_settled('ABORTED', [settled_job]), 'settled')
        self.assertTrue(answered['ok'])
        no_exit = {'name': 'native-op', 'has_request': True, 'has_exit': False,
                   'processes': [{'pid': 7, 'probe': 'alive'}]}
        # The missing exit receipt wins over any process rule.
        answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [no_exit]},
                                   reference.archive_settled('ABORTED', [no_exit]), 'no-exit')
        self.assertEqual(answered['refusal'],
                         'RUN_UNSETTLED: native-op has no exit receipt; no cleanup')
        for bad in (0, -5, 3.5, "7", None):
            job = {'name': 'j1', 'has_request': False, 'has_exit': False,
                   'processes': [{'pid': bad, 'probe': 'dead'}]}
            answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [job]},
                                       reference.archive_settled('ABORTED', [job]), f'pid-{bad!r}')
            self.assertEqual(answered['refusal'], 'RUN_UNSETTLED: invalid process identity')
        dead_then_alive = {'name': 'j1', 'has_request': True, 'has_exit': True,
                           'processes': [{'pid': 111, 'probe': 'dead'}, {'pid': 4242, 'probe': 'alive'}]}
        answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [dead_then_alive]},
                                   reference.archive_settled('ABORTED', [dead_then_alive]), 'alive-second')
        self.assertEqual(answered['refusal'], 'RUN_UNSETTLED: PID 4242 still exists; no cleanup')
        permission = {'name': 'j1', 'has_request': True, 'has_exit': True,
                      'processes': [{'pid': 999, 'probe': 'permission'}]}
        answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [permission]},
                                   reference.archive_settled('ABORTED', [permission]), 'permission')
        self.assertEqual(answered['refusal'], 'RUN_UNSETTLED: process cannot be checked')

    # ---- archive-index-plan ---------------------------------------------------

    def test_index_plan_exists_refusal_and_assembly(self):
        payload = {'destination_exists': True, 'source_directory': '/runs/r', 'run_id': 'r1',
                   'revision': 'rev0', 'runtime_identity': 'ident', 'status': {'status': 'ABORTED'},
                   'files': {'a.txt': PROOF_A}, 'database_logical_sha256': 'logical',
                   'checkpoints_proof': PROOF_DB_BACKUP}
        answered = self.assertSame('archive-index-plan', payload,
                                   reference.archive_index_plan(**payload), 'exists-first')
        self.assertEqual(answered['refusal'], 'ARCHIVE_EXISTS: use a new destination; not overwritten')
        # A byte-level proof under 'checkpoints.sqlite' is replaced by the
        # backup proof, and the status value passes through untouched.
        payload = dict(payload, destination_exists=False,
                       files={'checkpoints.sqlite': PROOF_DB_LIVE, 'a.txt': PROOF_A},
                       status={'status': 'DEMO_COMPLETE', 'run_id': 'r1', 'nested': [1, 2.5]})
        answered = self.assertSame('archive-index-plan', payload,
                                   reference.archive_index_plan(**payload), 'assembly')
        index = answered['index']
        self.assertEqual(index['files']['checkpoints.sqlite'], PROOF_DB_BACKUP)
        self.assertEqual(index['status']['nested'], [1, 2.5])
        self.assertEqual(index['format'], 'formal-gates-archive-v1')

    # ---- archive-verify ---------------------------------------------------

    def test_verify_phase_one_rejects_duplicates_before_any_manifest_work(self):
        answered = self.assertSame('archive-verify',
                                   {'names': ['archive-manifest.json', 'run/a', 'run/a']},
                                   reference.archive_verify(['archive-manifest.json', 'run/a', 'run/a']),
                                   'duplicates')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: duplicate members')
        answered = self.assertSame('archive-verify', {'names': ['archive-manifest.json', 'run/a']},
                                   reference.archive_verify(['archive-manifest.json', 'run/a']), 'unique')
        self.assertTrue(answered['ok'])

    def test_verify_wrong_format_or_nonterminal(self):
        names = ['archive-manifest.json', 'run/a.txt']
        good_files = {'a.txt': PROOF_A}
        proofs = {'a.txt': PROOF_A}
        for label, index in [
            ('no-format', {'status': {'status': 'ABORTED'}, 'files': good_files}),
            ('wrong-format', {'format': 'other-v0', 'status': {'status': 'ABORTED'}, 'files': good_files}),
            ('nonterminal', {'format': 'formal-gates-archive-v1', 'status': {'status': 'PAUSED'},
                             'files': good_files}),
            ('nonstring-status', {'format': 'formal-gates-archive-v1', 'status': {'status': 5},
                                  'files': good_files}),
        ]:
            payload = {'names': names, 'index': index, 'member_proofs': proofs}
            answered = self.assertSame('archive-verify', payload,
                                       reference.archive_verify(names, index, proofs), label)
            self.assertFalse(answered['ok'])
            self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: wrong format or nonterminal run')

    def test_verify_member_set_check_beats_safety_and_content(self):
        # The names set does not match, so the unsafe path and the wrong proof
        # below are never reached: the set verdict wins.
        files = {'../evil': PROOF_A, 'a.txt': PROOF_B}
        index = {'format': 'formal-gates-archive-v1', 'status': {'status': 'ABORTED'}, 'files': files}
        names = ['archive-manifest.json', 'run/a.txt']
        proofs = {'a.txt': PROOF_A}
        answered = self.assertSame('archive-verify', {'names': names, 'index': index, 'member_proofs': proofs},
                                   reference.archive_verify(names, index, proofs), 'set-first')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: missing or extra members')
        extra = ['archive-manifest.json', 'run/a.txt', 'run/extra']
        answered = self.assertSame('archive-verify',
                                   {'names': extra, 'index': index, 'member_proofs': proofs},
                                   reference.archive_verify(extra, index, proofs), 'extra-member')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: missing or extra members')

    def test_verify_unsafe_paths_before_content_per_name(self):
        # Per name, safety runs before content; and names are visited in the
        # manifest's files order, so the first entry decides.
        proofs = {'/etc/passwd': PROOF_A}
        files = {'/etc/passwd': PROOF_A}
        index = {'format': 'formal-gates-archive-v1', 'status': {'status': 'ABORTED'}, 'files': files}
        names = ['archive-manifest.json', 'run//etc/passwd']
        answered = self.assertSame('archive-verify', {'names': names, 'index': index, 'member_proofs': proofs},
                                   reference.archive_verify(names, index, proofs), 'absolute')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: unsafe member path')
        for unsafe, member in [('a/../b', 'run/a/../b'), ('win\\name', 'run/win\\name')]:
            files = {unsafe: PROOF_A}
            index = {'format': 'formal-gates-archive-v1', 'status': {'status': 'ABORTED'}, 'files': files}
            names = ['archive-manifest.json', member]
            answered = self.assertSame('archive-verify',
                                       {'names': names, 'index': index, 'member_proofs': {unsafe: PROOF_A}},
                                       reference.archive_verify(names, index, {unsafe: PROOF_A}), unsafe)
            self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: unsafe member path')
        # Safe-but-wrong content first, unsafe second: the first name's
        # content verdict wins because iteration follows files order.
        files = {'good.txt': PROOF_A, 'bad\\path': PROOF_A}
        index = {'format': 'formal-gates-archive-v1', 'status': {'status': 'ABORTED'}, 'files': files}
        names = ['archive-manifest.json', 'run/good.txt', 'run/bad\\path']
        answered = self.assertSame('archive-verify',
                                   {'names': names, 'index': index, 'member_proofs': {'good.txt': PROOF_B, 'bad\\path': PROOF_A}},
                                   reference.archive_verify(names, index,
                                                            {'good.txt': PROOF_B, 'bad\\path': PROOF_A}),
                                   'content-before-later-unsafe')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: content differs: good.txt')

    def test_verify_full_pass_returns_index_with_number_fidelity(self):
        files = {'z.txt': PROOF_A, 'a.bin': {'sha256': 'cc', 'size': 3.5}}
        index = {'format': 'formal-gates-archive-v1', 'run_id': 'r1',
                 'status': {'status': 'HARNESS_COMPLETE', 'run_id': 'r1'}, 'files': files,
                 'database_logical_sha256': 'logical'}
        names = ['archive-manifest.json', 'run/z.txt', 'run/a.bin']
        proofs = {'z.txt': PROOF_A, 'a.bin': {'sha256': 'cc', 'size': 3.5}}
        answered = self.assertSame('archive-verify', {'names': names, 'index': index, 'member_proofs': proofs},
                                   reference.archive_verify(names, index, proofs), 'pass')
        self.assertTrue(answered['ok'])
        self.assertEqual(answered['index']['files']['a.bin']['size'], 3.5)

    # ---- archive-stale ---------------------------------------------------

    def test_stale_new_files_win_even_when_missing_allowed(self):
        current = {'new.txt': PROOF_A}
        answered = self.assertSame('archive-stale',
                                   {'current': current, 'files': {}, 'allow_missing': True,
                                    'database_logical_sha256': 'logical', 'live_database_logical_sha256': None},
                                   reference.archive_stale(current, {}, True, 'logical', None), 'new-files')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: new files appeared; no deletion')

    def test_stale_missing_files_and_allow_missing_both_ways(self):
        current = {'manifest.json': PROOF_A}
        files = {'manifest.json': PROOF_A, 'jobs/x.json': PROOF_B}
        payload = {'current': current, 'files': files, 'allow_missing': False,
                   'database_logical_sha256': 'logical', 'live_database_logical_sha256': None}
        answered = self.assertSame('archive-stale', payload,
                                   reference.archive_stale(current, files, False, 'logical', None), 'missing')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: source files missing')
        payload = dict(payload, allow_missing=True)
        answered = self.assertSame('archive-stale', payload,
                                   reference.archive_stale(current, files, True, 'logical', None), 'allowed')
        self.assertTrue(answered['ok'])

    def test_stale_differs_names_in_current_iteration_order(self):
        # Insertion order puts the differing entry first even though a sorted
        # iteration would reach 'a.txt' first.
        current = {'z.txt': PROOF_B, 'a.txt': PROOF_A}
        files = {'a.txt': PROOF_A, 'z.txt': PROOF_A}
        payload = {'current': current, 'files': files, 'allow_missing': False,
                   'database_logical_sha256': 'logical', 'live_database_logical_sha256': None}
        answered = self.assertSame('archive-stale', payload,
                                   reference.archive_stale(current, files, False, 'logical', None), 'order')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: source differs: z.txt')

    def test_stale_database_logical_special_case(self):
        # Same byte proof, different logical hash: refuse.
        current = {'checkpoints.sqlite': PROOF_DB_BACKUP}
        files = {'checkpoints.sqlite': PROOF_DB_BACKUP}
        payload = {'current': current, 'files': files, 'allow_missing': False,
                   'database_logical_sha256': 'logical-index', 'live_database_logical_sha256': 'logical-live'}
        answered = self.assertSame('archive-stale', payload,
                                   reference.archive_stale(current, files, False, 'logical-index', 'logical-live'),
                                   'logical-differs')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: source differs: checkpoints.sqlite')
        # Different byte proof (page layout moved), same logical hash: pass.
        current = {'checkpoints.sqlite': PROOF_DB_LIVE}
        files = {'checkpoints.sqlite': PROOF_DB_BACKUP}
        payload = {'current': current, 'files': files, 'allow_missing': False,
                   'database_logical_sha256': 'logical', 'live_database_logical_sha256': 'logical'}
        answered = self.assertSame('archive-stale', payload,
                                   reference.archive_stale(current, files, False, 'logical', 'logical'),
                                   'logical-same')
        self.assertTrue(answered['ok'])

    # ---- archive-projection ---------------------------------------------------

    def _projection_index(self):
        return {'source_directory': '/runs/r',
                'status': {'status': 'ABORTED', 'run_id': 'r1', 'revision': 'rev0', 'candidate': 'c1'},
                'files': {'a.txt': PROOF_A, 'checkpoints.sqlite': PROOF_DB_BACKUP}}

    def test_projection_without_marker(self):
        index = self._projection_index()
        answered = self.assertSame('archive-projection',
                                   {'index': index, 'archive': '/keep/run.zip', 'marker': None, 'directory': ''},
                                   reference.archive_projection(index, '/keep/run.zip', None, ''), 'plain')
        status = answered['status']
        self.assertEqual(status['archived'], True)
        self.assertEqual(status['archive'], '/keep/run.zip')
        self.assertEqual(status['archive_verified'], True)
        self.assertEqual(status['source_directory'], '/runs/r')
        self.assertEqual(status['file_count'], 2)
        self.assertEqual(status['model_calls'], 0)
        self.assertNotIn('cleanup_complete', status)

    def test_projection_pre_check_phase_fires_changed_before_any_index(self):
        # Without an index the op is archived_status's pre-check: a marker
        # whose proof no longer matches refuses ARCHIVE_CHANGED before the
        # zip is ever opened.
        marker = {'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'run_id': 'r1',
                  'revision': 'rev0'}
        payload = {'archive': '/keep/run.zip', 'archive_proof': PROOF_B,
                   'marker': marker, 'directory': '/runs/r'}
        answered = self.assertSame('archive-projection', payload,
                                   reference.archive_projection(None, '/keep/run.zip', marker,
                                                                '/runs/r', PROOF_B), 'changed-early')
        self.assertEqual(answered['refusal'], 'ARCHIVE_CHANGED: retained archive missing or altered')
        payload = dict(payload, archive_proof=PROOF_A)
        answered = self.assertSame('archive-projection', payload,
                                   reference.archive_projection(None, '/keep/run.zip', marker,
                                                                '/runs/r', PROOF_A), 'unchanged')
        self.assertTrue(answered['ok'])
        answered = self.assertSame('archive-projection',
                                   {'archive': '/keep/run.zip', 'marker': None, 'directory': ''},
                                   reference.archive_projection(None, '/keep/run.zip', None, ''), 'no-marker')
        self.assertTrue(answered['ok'])

    def test_projection_changed_and_binding_refusals(self):
        index = self._projection_index()
        changed = {'archive': '/keep/run.zip', 'archive_proof': PROOF_B, 'run_id': 'r1',
                   'revision': 'rev0'}
        payload = {'index': index, 'archive': '/keep/run.zip', 'archive_proof': PROOF_A,
                   'marker': changed, 'directory': '/runs/r'}
        answered = self.assertSame('archive-projection', payload,
                                   reference.archive_projection(index, '/keep/run.zip', changed,
                                                                '/runs/r', PROOF_A), 'changed')
        self.assertEqual(answered['refusal'], 'ARCHIVE_CHANGED: retained archive missing or altered')
        for label, marker in [
            ('run-id', {'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'run_id': 'other',
                        'revision': 'rev0'}),
            ('revision', {'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'run_id': 'r1',
                          'revision': 'rev1'}),
            ('source', {'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'run_id': 'r1',
                        'revision': 'rev0'}),
        ]:
            directory = '/elsewhere' if label == 'source' else '/runs/r'
            payload = {'index': index, 'archive': '/keep/run.zip', 'archive_proof': PROOF_A,
                       'marker': marker, 'directory': directory}
            answered = self.assertSame('archive-projection', payload,
                                       reference.archive_projection(index, '/keep/run.zip', marker,
                                                                    directory, PROOF_A), label)
            self.assertEqual(answered['refusal'], 'ARCHIVE_BINDING: tombstone does not match archive')

    def test_projection_with_marker_adds_cleanup_complete(self):
        index = self._projection_index()
        for label, marker, want in [
            ('default', {'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'run_id': 'r1',
                         'revision': 'rev0'}, False),
            ('done', {'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'run_id': 'r1',
                      'revision': 'rev0', 'cleanup_complete': True}, True),
            ('passthrough', {'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'run_id': 'r1',
                             'revision': 'rev0', 'cleanup_complete': 7}, 7),
        ]:
            payload = {'index': index, 'archive': '/keep/run.zip', 'archive_proof': PROOF_A,
                       'marker': marker, 'directory': '/runs/r'}
            answered = self.assertSame('archive-projection', payload,
                                       reference.archive_projection(index, '/keep/run.zip', marker,
                                                                    '/runs/r', PROOF_A), label)
            self.assertTrue(answered['ok'])
            self.assertEqual(answered['status']['cleanup_complete'], want)

    # ---- prune-admission ---------------------------------------------------

    def test_prune_admission_rules_and_component_semantics(self):
        rows = [
            ('/harness/runs', '/harness/runs/r1', 'r1', 'r1', '/keep/run.zip', True, None),
            ('/harness/runs', '/harness/runs/r1', 'r1', 'r1', '/harness/runs/r1/x.zip', False,
             'ARCHIVE_PATH: archive must be outside the run'),
            ('/harness/runs', '/harness/runs/r1', 'r1', 'r1', '/harness/runs/r1', False,
             'ARCHIVE_PATH: archive must be outside the run'),
            ('/harness/runs', '/harness/runs/r1', 'r1', 'r1', '/harness/runs/r1-2/x.zip', True, None),
            ('/harness/runs', '/elsewhere/r1', 'r1', 'r1', '/keep/run.zip', False,
             "PRUNE_PATH: only this installation's run directories are eligible"),
            ('/harness/runs', '/harness/runs/r1', 'r1', 'other', '/keep/run.zip', False,
             "PRUNE_PATH: only this installation's run directories are eligible"),
            # PRUNE_PATH wins when both rules refuse.
            ('/harness/runs', '/elsewhere/r1', 'r1', 'other', '/elsewhere/r1/x.zip', False,
             "PRUNE_PATH: only this installation's run directories are eligible"),
        ]
        for runs_dir, directory, name, run_id, archive, want_ok, want_refusal in rows:
            payload = {'runs_dir': runs_dir, 'directory': directory, 'directory_name': name,
                       'run_id': run_id, 'archive': archive}
            answered = self.assertSame('prune-admission', payload,
                                       reference.prune_admission(runs_dir, directory, name, run_id, archive),
                                       f'{directory}->{archive}')
            self.assertEqual(answered.get('ok'), want_ok)
            if want_refusal:
                self.assertEqual(answered['refusal'], want_refusal)

    # ---- prune-bind ---------------------------------------------------

    def test_prune_bind_triple(self):
        payload = {'directory': '/harness/runs/r1', 'run_id': 'r1', 'revision': 'rev0',
                   'source_directory': '/harness/runs/r1', 'index_run_id': 'r1', 'index_revision': 'rev0'}
        answered = self.assertSame('prune-bind', payload,
                                   reference.prune_bind(**{k: payload[k] for k in
                                                           ('directory', 'run_id', 'revision',
                                                            'source_directory', 'index_run_id', 'index_revision')}),
                                   'ok')
        self.assertTrue(answered['ok'])
        for label, override in [('source', {'source_directory': '/elsewhere'}),
                                ('run-id', {'index_run_id': 'other'}),
                                ('revision', {'index_revision': 'rev1'}),
                                ('nonstring-run-id', {'index_run_id': 7})]:
            case = dict(payload, **override)
            answered = self.assertSame('prune-bind', case,
                                       reference.prune_bind(**{k: case[k] for k in
                                                               ('directory', 'run_id', 'revision',
                                                                'source_directory', 'index_run_id',
                                                                'index_revision')}), label)
            self.assertEqual(answered['refusal'], 'ARCHIVE_BINDING: wrong run/revision')

    # ---- prune-plan ---------------------------------------------------

    def _plan_payload(self, **override):
        current = {'manifest.json': PROOF_A, 'a/b/z.txt': PROOF_B, 'runner.lock': PROOF_A,
                   'a/y.txt': PROOF_B, 'b.txt': PROOF_A, 'a/b.txt': PROOF_B, 'archive.json': PROOF_A}
        files = dict(current)
        payload = {'marker': None, 'archive': '/keep/run.zip', 'archive_proof': PROOF_A,
                   'current': current, 'files': files, 'database_logical_sha256': 'logical',
                   'live_database_logical_sha256': None, 'allow_missing': False,
                   'run_id': 'r1', 'revision': 'rev0', 'terminal_status': 'ABORTED',
                   'directory': '/harness/runs/r1'}
        payload.update(override)
        return payload

    def _plan_reference(self, payload):
        return reference.prune_plan(
            payload['marker'], payload['archive'], payload['archive_proof'], payload['current'],
            payload['files'], payload['database_logical_sha256'],
            payload['live_database_logical_sha256'], payload['allow_missing'],
            payload['run_id'], payload['revision'], payload['terminal_status'], payload['directory'])

    def test_prune_plan_keep_filter_and_removal_sort(self):
        payload = self._plan_payload()
        answered = self.assertSame('prune-plan', payload, self._plan_reference(payload), 'plan')
        # Depth first (a/b/z.txt has two slashes), then same-depth alphabetical
        # (a/b.txt before a/y.txt), then the shallow entry; KEEP names stay.
        self.assertEqual(answered['remove'], ['a/b/z.txt', 'a/b.txt', 'a/y.txt', 'b.txt'])
        self.assertEqual(answered['report']['files_to_remove'], 4)
        self.assertEqual(answered['report']['bytes_to_remove'], 70)
        self.assertEqual(answered['report']['terminal_status'], 'ABORTED')
        self.assertEqual(answered['marker'],
                         {'format': 'formal-gates-prune-v1', 'run_id': 'r1', 'revision': 'rev0',
                          'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'cleanup_complete': False})

    def test_prune_plan_marker_binding_before_staleness(self):
        marker = {'archive': '/other/run.zip', 'archive_proof': PROOF_A}
        payload = self._plan_payload(marker=marker, allow_missing=True)
        payload['current']['surprise.txt'] = PROOF_A  # would refuse stale, after binding
        answered = self.assertSame('prune-plan', payload, self._plan_reference(payload), 'original-archive')
        self.assertEqual(answered['refusal'], 'ARCHIVE_BINDING: use the original cleanup archive')
        marker = {'archive': '/keep/run.zip', 'archive_proof': PROOF_B}
        payload = self._plan_payload(marker=marker)
        answered = self.assertSame('prune-plan', payload, self._plan_reference(payload), 'proof-mismatch')
        self.assertEqual(answered['refusal'], 'ARCHIVE_BINDING: use the original cleanup archive')

    def test_prune_plan_matching_marker_reports_without_new_plan(self):
        marker = {'archive': '/keep/run.zip', 'archive_proof': PROOF_A, 'run_id': 'r1',
                  'revision': 'rev0', 'cleanup_complete': True}
        payload = self._plan_payload(marker=marker, allow_missing=True)
        payload['files']['gone.txt'] = PROOF_B  # allowed to be missing under a marker
        del payload['current']['a/y.txt']
        answered = self.assertSame('prune-plan', payload, self._plan_reference(payload), 'matching')
        self.assertTrue(answered['ok'])
        self.assertIsNone(answered['marker'])
        self.assertEqual(answered['remove'], ['a/b/z.txt', 'a/b.txt', 'b.txt'])

    def test_prune_plan_empty_marker_object_is_falsy_binding_skipped(self):
        # `if marker and ...`: an empty marker object skips the binding check
        # but is not None, so no tombstone plan is attached either.
        payload = self._plan_payload(marker={})
        answered = self.assertSame('prune-plan', payload, self._plan_reference(payload), 'empty-marker')
        self.assertTrue(answered['ok'])
        self.assertIsNone(answered['marker'])

    def test_prune_plan_staleness_and_database_special_case(self):
        payload = self._plan_payload()
        payload['current']['new.txt'] = PROOF_A
        answered = self.assertSame('prune-plan', payload, self._plan_reference(payload), 'new-files')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: new files appeared; no deletion')
        payload = self._plan_payload(
            current={'checkpoints.sqlite': PROOF_DB_BACKUP}, files={'checkpoints.sqlite': PROOF_DB_BACKUP},
            database_logical_sha256='logical-index', live_database_logical_sha256='logical-live')
        answered = self.assertSame('prune-plan', payload, self._plan_reference(payload), 'logical-differs')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: source differs: checkpoints.sqlite')


if __name__ == '__main__':
    unittest.main()
