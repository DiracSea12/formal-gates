"""Compare retention judgments with an independent fact/chunk oracle through the Go bridge.

The corpus pins refusal priorities, fact-before-batch ordering, array order,
logical database hashes, pruning order, tombstone binding and heterogeneous
identity normalization. Sequence tests also cover cross-batch and cross-operation
ordering in the Python wrapper."""
import hashlib
import json
import os
import shutil
import unittest
import uuid
import zipfile
from unittest.mock import patch

import test_driver as fixtures
from formal_gates_harness.infra import HarnessError, atomic, canonical, driver_bridge, runtime_identity
from formal_gates_harness import retention as retention_module
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

    def test_inventory_rejects_an_oversized_batch(self):
        # A whole inventory in one request is the capacity regression the
        # chunk rule exists to prevent; the bridge must fail loudly.
        entries = [{'name': f'f-{i:05d}', 'kind': 'file'} for i in range(reference.CHUNK + 1)]
        with self.assertRaises(HarnessError):
            driver_bridge().call('archive-inventory', {'entries': entries})
        at_limit = self.assertSame('archive-inventory', {'entries': entries[:-1]},
                                   reference.archive_inventory(entries[:-1]), 'at-limit')
        self.assertEqual(len(at_limit['files']), reference.CHUNK)

    def test_inventory_second_batch_refusal_comes_after_first_batch_collection(self):
        # Cross-batch order is the batch order: batch 1 collects, batch 2
        # refuses, and nothing after batch 2 is judged.
        batch1 = [{'name': 'a.txt', 'kind': 'file'}, {'name': 'alpha', 'kind': 'dir'}]
        batch2 = [{'name': 'z-late', 'kind': 'symlink'}, {'name': 'z.txt', 'kind': 'file'}]
        answered = self.assertSame('archive-inventory', {'entries': batch1},
                                   reference.archive_inventory(batch1), 'batch1-collects')
        self.assertEqual(answered['files'], ['a.txt'])
        answered = self.assertSame('archive-inventory', {'entries': batch2},
                                   reference.archive_inventory(batch2), 'batch2-refuses')
        self.assertEqual(answered['refusal'],
                         'ARCHIVE_UNSUPPORTED: symlink z-late; no files deleted')

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
                   'processes': [{'pid': 7, 'pid_valid': True, 'probe': 'alive'}]}
        # The missing exit receipt wins over any process rule.
        answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [no_exit]},
                                   reference.archive_settled('ABORTED', [no_exit]), 'no-exit')
        self.assertEqual(answered['refusal'],
                         'RUN_UNSETTLED: native-op has no exit receipt; no cleanup')
        # pid_valid is the wrapper's fact; the pid rides as int or null.
        for pid, label in ((0, 'zero'), (-5, 'negative'), (None, 'null'), (4242, 'unprobed')):
            job = {'name': 'j1', 'has_request': False, 'has_exit': False,
                   'processes': [{'pid': pid, 'pid_valid': False, 'probe': 'dead'}]}
            answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [job]},
                                       reference.archive_settled('ABORTED', [job]), f'pid-{label}')
            self.assertEqual(answered['refusal'], 'RUN_UNSETTLED: invalid process identity')
        dead_then_alive = {'name': 'j1', 'has_request': True, 'has_exit': True,
                           'processes': [{'pid': 111, 'pid_valid': True, 'probe': 'dead'},
                                         {'pid': 4242, 'pid_valid': True, 'probe': 'alive'}]}
        answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [dead_then_alive]},
                                   reference.archive_settled('ABORTED', [dead_then_alive]), 'alive-second')
        self.assertEqual(answered['refusal'],
                         'RUN_UNSETTLED: PID 4242 still exists; no cleanup')
        permission = {'name': 'j1', 'has_request': True, 'has_exit': True,
                      'processes': [{'pid': 999, 'pid_valid': True, 'probe': 'permission'}]}
        answered = self.assertSame('archive-settled', {'status_name': 'ABORTED', 'jobs': [permission]},
                                   reference.archive_settled('ABORTED', [permission]), 'permission')
        self.assertEqual(answered['refusal'], 'RUN_UNSETTLED: process cannot be checked')

    # ---- archive-index-plan ---------------------------------------------------

    def test_index_plan_assembles_skeleton_without_files(self):
        payload = {'source_directory': '/runs/r', 'run_id': 'r1', 'revision': 'rev0',
                   'runtime_identity': 'ident', 'status': {'status': 'ABORTED', 'nested': [1, 2.5]},
                   'database_logical_sha256': 'logical'}
        answered = self.assertSame('archive-index-plan', payload,
                                   reference.archive_index_plan(**payload), 'skeleton')
        skeleton = answered['skeleton']
        self.assertEqual(skeleton['format'], 'formal-gates-archive-v1')
        self.assertEqual(skeleton['status']['nested'], [1, 2.5])
        self.assertNotIn('files', skeleton)  # the proofs join on the Python side

    # ---- archive-verify ---------------------------------------------------

    def test_verify_duplicate_fact_refuses_with_entries_present(self):
        # Facts win over the same call's entries, in the baseline's order:
        # duplicates first.
        payload = {'duplicates': True, 'format': 'formal-gates-archive-v1',
                   'status_name': 'ABORTED', 'names_match': False,
                   'entries': [{'name': 'a.txt', 'actual': PROOF_A, 'expected': PROOF_B}]}
        answered = self.assertSame('archive-verify', payload,
                                   reference.archive_verify(**payload), 'duplicates-first')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: duplicate members')

    def test_verify_wrong_format_or_nonterminal(self):
        for label, fmt, status in [('missing-format', None, 'ABORTED'),
                                   ('wrong-format', 'other-v0', 'ABORTED'),
                                   ('nonterminal', 'formal-gates-archive-v1', 'PAUSED'),
                                   ('nonstring-status', 'formal-gates-archive-v1', None)]:
            payload = {'duplicates': False, 'format': fmt, 'status_name': status,
                       'names_match': True, 'entries': []}
            answered = self.assertSame('archive-verify', payload,
                                       reference.archive_verify(**payload), label)
            self.assertFalse(answered['ok'])
            self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: wrong format or nonterminal run')

    def test_verify_names_match_fact_beats_entries(self):
        payload = {'duplicates': False, 'format': 'formal-gates-archive-v1',
                   'status_name': 'ABORTED', 'names_match': False,
                   'entries': [{'name': '../evil', 'actual': PROOF_A, 'expected': PROOF_A},
                               {'name': 'a.txt', 'actual': PROOF_A, 'expected': PROOF_B}]}
        answered = self.assertSame('archive-verify', payload,
                                   reference.archive_verify(**payload), 'set-before-entries')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: missing or extra members')

    def test_verify_unsafe_paths_before_content_per_entry(self):
        good = {'duplicates': False, 'format': 'formal-gates-archive-v1',
                'status_name': 'ABORTED', 'names_match': True}
        for unsafe in ('/etc/passwd', 'a/../b', 'win\\name'):
            payload = {**good, 'entries': [{'name': unsafe, 'actual': PROOF_A, 'expected': PROOF_A}]}
            answered = self.assertSame('archive-verify', payload,
                                       reference.archive_verify(**payload), unsafe)
            self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: unsafe member path')
        # Per entry, safety runs before content; and the FIRST entry decides.
        payload = {**good, 'entries': [{'name': 'good.txt', 'actual': PROOF_A, 'expected': PROOF_B},
                                       {'name': 'bad\\path', 'actual': PROOF_A, 'expected': PROOF_A}]}
        answered = self.assertSame('archive-verify', payload,
                                   reference.archive_verify(**payload), 'content-before-later-unsafe')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: content differs: good.txt')
        # Typed comparison: a size difference alone refuses.
        payload = {**good, 'entries': [{'name': 'a.txt', 'actual': PROOF_A,
                                        'expected': {'sha256': PROOF_A['sha256'], 'size': 11}}]}
        answered = self.assertSame('archive-verify', payload,
                                   reference.archive_verify(**payload), 'size-differs')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: content differs: a.txt')

    def test_verify_full_pass_and_second_batch_refusal(self):
        good = {'duplicates': False, 'format': 'formal-gates-archive-v1',
                'status_name': 'HARNESS_COMPLETE', 'names_match': True}
        batch1 = [{'name': 'a.txt', 'actual': PROOF_A, 'expected': PROOF_A},
                  {'name': 'b.txt', 'actual': PROOF_B, 'expected': PROOF_B}]
        answered = self.assertSame('archive-verify', {**good, 'entries': batch1},
                                   reference.archive_verify(**good, entries=batch1), 'batch1-pass')
        self.assertTrue(answered['ok'])
        self.assertEqual(set(answered), {'ok'})  # success returns no index
        batch2 = [{'name': 'c.txt', 'actual': PROOF_A, 'expected': PROOF_B}]
        answered = self.assertSame('archive-verify', {**good, 'entries': batch2},
                                   reference.archive_verify(**good, entries=batch2), 'batch2-differs')
        self.assertEqual(answered['refusal'], 'ARCHIVE_INVALID: content differs: c.txt')

    def test_verify_rejects_an_oversized_batch(self):
        good = {'duplicates': False, 'format': 'formal-gates-archive-v1',
                'status_name': 'ABORTED', 'names_match': True}
        entries = [{'name': f'f-{i:05d}', 'actual': PROOF_A, 'expected': PROOF_A}
                   for i in range(reference.CHUNK + 1)]
        with self.assertRaises(HarnessError):
            driver_bridge().call('archive-verify', {**good, 'entries': entries})

    # ---- archive-stale ---------------------------------------------------

    def test_stale_facts_new_files_win_even_when_missing_allowed(self):
        payload = {'has_new': True, 'has_missing': True, 'allow_missing': True}
        answered = self.assertSame('archive-stale', payload,
                                   reference.archive_stale_facts(**payload), 'new-files')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: new files appeared; no deletion')

    def test_stale_facts_missing_both_ways(self):
        payload = {'has_new': False, 'has_missing': True, 'allow_missing': False}
        answered = self.assertSame('archive-stale', payload,
                                   reference.archive_stale_facts(**payload), 'missing')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: source files missing')
        payload = dict(payload, allow_missing=True)
        answered = self.assertSame('archive-stale', payload,
                                   reference.archive_stale_facts(**payload), 'allowed')
        self.assertTrue(answered['ok'])

    def test_stale_entries_in_current_iteration_order(self):
        # Insertion order puts the differing entry first even though a sorted
        # iteration would reach 'a.txt' first.
        entries = [{'name': 'z.txt', 'kind': 'file', 'actual_proof': PROOF_B, 'expected_proof': PROOF_A},
                   {'name': 'a.txt', 'kind': 'file', 'actual_proof': PROOF_A, 'expected_proof': PROOF_A}]
        answered = self.assertSame('archive-stale', {'entries': entries},
                                   reference.archive_stale_entries(entries), 'order')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: source differs: z.txt')

    def test_stale_database_logical_special_case(self):
        # Same byte proof, different logical hash: refuse. The unused byte
        # proofs ride along but the db strings decide.
        entries = [{'name': 'checkpoints.sqlite', 'kind': 'database',
                    'actual_proof': PROOF_DB_BACKUP, 'expected_proof': PROOF_DB_BACKUP,
                    'actual_db': 'logical-live', 'expected_db': 'logical-index'}]
        answered = self.assertSame('archive-stale', {'entries': entries},
                                   reference.archive_stale_entries(entries), 'logical-differs')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: source differs: checkpoints.sqlite')
        # Different byte proof (page layout moved), same logical hash: pass.
        entries = [{'name': 'checkpoints.sqlite', 'kind': 'database',
                    'actual_proof': PROOF_DB_LIVE, 'expected_proof': PROOF_DB_BACKUP,
                    'actual_db': 'logical', 'expected_db': 'logical'}]
        answered = self.assertSame('archive-stale', {'entries': entries},
                                   reference.archive_stale_entries(entries), 'logical-same')
        self.assertTrue(answered['ok'])

    def test_stale_second_batch_refusal(self):
        batch1 = [{'name': 'a.txt', 'kind': 'file', 'actual_proof': PROOF_A, 'expected_proof': PROOF_A}]
        batch2 = [{'name': 'checkpoints.sqlite', 'kind': 'database',
                   'actual_db': 'logical-live', 'expected_db': 'logical-index'}]
        answered = self.assertSame('archive-stale', {'entries': batch1},
                                   reference.archive_stale_entries(batch1), 'batch1-pass')
        self.assertTrue(answered['ok'])
        answered = self.assertSame('archive-stale', {'entries': batch2},
                                   reference.archive_stale_entries(batch2), 'batch2-db-differs')
        self.assertEqual(answered['refusal'], 'ARCHIVE_STALE: source differs: checkpoints.sqlite')

    def test_stale_rejects_an_oversized_batch(self):
        entries = [{'name': f'f-{i:05d}', 'kind': 'file',
                    'actual_proof': PROOF_A, 'expected_proof': PROOF_A}
                   for i in range(reference.CHUNK + 1)]
        with self.assertRaises(HarnessError):
            driver_bridge().call('archive-stale', {'entries': entries})

    # ---- archive-projection ---------------------------------------------------

    def test_projection_pre_check_phase_fires_changed_before_any_status(self):
        # Without a status the op is archived_status's pre-check: a marker
        # whose proof no longer matches refuses ARCHIVE_CHANGED before the
        # zip is ever opened.
        payload = {'archive': '/keep/run.zip', 'archive_proof': PROOF_B,
                   'marker': {'archive_proof': PROOF_A}}
        answered = self.assertSame('archive-projection', payload,
                                   reference.archive_projection_precheck(PROOF_B, payload['marker']),
                                   'changed-early')
        self.assertEqual(answered['refusal'], 'ARCHIVE_CHANGED: retained archive missing or altered')
        payload = dict(payload, archive_proof=PROOF_A)
        answered = self.assertSame('archive-projection', payload,
                                   reference.archive_projection_precheck(PROOF_A, payload['marker']),
                                   'unchanged')
        self.assertTrue(answered['ok'])
        answered = self.assertSame('archive-projection', {'archive': '/keep/run.zip'},
                                   reference.archive_projection_precheck(None, None), 'no-marker')
        self.assertTrue(answered['ok'])

    def _status(self):
        return {'status': 'ABORTED', 'run_id': 'r1', 'revision': 'rev0', 'candidate': 'c1'}

    def test_projection_without_marker(self):
        payload = {'status': self._status(), 'source_directory': '/runs/r', 'file_count': 2,
                   'archive': '/keep/run.zip', 'archive_proof': None, 'marker': None, 'directory': ''}
        answered = self.assertSame('archive-projection', payload,
                                   reference.archive_projection(self._status(), '/runs/r', 2,
                                                                '/keep/run.zip', None, ''), 'plain')
        status = answered['status']
        self.assertEqual(status['archived'], True)
        self.assertEqual(status['archive'], '/keep/run.zip')
        self.assertEqual(status['archive_verified'], True)
        self.assertEqual(status['source_directory'], '/runs/r')
        self.assertEqual(status['file_count'], 2)
        self.assertEqual(status['model_calls'], 0)
        self.assertNotIn('cleanup_complete', status)

    def test_projection_tombstone_binding_triple(self):
        for label, marker, directory in [
            ('run-id', {'run_id': 'other', 'revision': 'rev0', 'cleanup_complete': False}, '/runs/r'),
            ('revision', {'run_id': 'r1', 'revision': 'rev1', 'cleanup_complete': False}, '/runs/r'),
            ('source', {'run_id': 'r1', 'revision': 'rev0', 'cleanup_complete': False}, '/elsewhere'),
        ]:
            payload = {'status': self._status(), 'source_directory': '/runs/r', 'file_count': 2,
                       'archive': '/keep/run.zip', 'archive_proof': PROOF_A,
                       'marker': marker, 'directory': directory}
            answered = self.assertSame('archive-projection', payload,
                                       reference.archive_projection(self._status(), '/runs/r', 2,
                                                                    '/keep/run.zip', marker, directory), label)
            self.assertEqual(answered['refusal'], 'ARCHIVE_BINDING: tombstone does not match archive')

    def test_projection_with_marker_adds_cleanup_complete(self):
        for label, cleanup, want in [('default', False, False), ('done', True, True),
                                     ('passthrough', 7, 7)]:
            marker = {'run_id': 'r1', 'revision': 'rev0', 'cleanup_complete': cleanup}
            payload = {'status': self._status(), 'source_directory': '/runs/r', 'file_count': 2,
                       'archive': '/keep/run.zip', 'archive_proof': PROOF_A,
                       'marker': marker, 'directory': '/runs/r'}
            answered = self.assertSame('archive-projection', payload,
                                       reference.archive_projection(self._status(), '/runs/r', 2,
                                                                    '/keep/run.zip', marker, '/runs/r'), label)
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

    def test_prune_bind_triple_with_none_normalization(self):
        payload = {'directory': '/harness/runs/r1', 'run_id': 'r1', 'revision': 'rev0',
                   'source_directory': '/harness/runs/r1', 'index_run_id': 'r1', 'index_revision': 'rev0'}
        answered = self.assertSame('prune-bind', payload,
                                   reference.prune_bind(**payload), 'ok')
        self.assertTrue(answered['ok'])
        # None is what the wrapper sends for a heterogeneous index value; it
        # can never match the run's own strings.
        for label, override in [('source', {'source_directory': None}),
                                ('run-id', {'index_run_id': None}),
                                ('revision', {'index_revision': None}),
                                ('wrong-string', {'index_run_id': 'other'})]:
            case = dict(payload, **override)
            answered = self.assertSame('prune-bind', case,
                                       reference.prune_bind(**case), label)
            self.assertEqual(answered['refusal'], 'ARCHIVE_BINDING: wrong run/revision')

    # ---- prune-marker ---------------------------------------------------

    def test_prune_marker_binding_both_sides(self):
        payload = {'archive': '/keep/run.zip', 'archive_proof': PROOF_A,
                   'marker_archive': '/keep/run.zip', 'marker_archive_proof': PROOF_A}
        answered = self.assertSame('prune-marker', payload,
                                   reference.prune_marker(**payload), 'ok')
        self.assertTrue(answered['ok'])
        for label, override in [('other-archive', {'marker_archive': '/other/run.zip'}),
                                ('other-proof', {'marker_archive_proof': PROOF_B})]:
            case = dict(payload, **override)
            answered = self.assertSame('prune-marker', case,
                                       reference.prune_marker(**case), label)
            self.assertEqual(answered['refusal'], 'ARCHIVE_BINDING: use the original cleanup archive')

    # ---- prune-plan ---------------------------------------------------

    def _entries(self):
        return [{'name': 'manifest.json', 'size': 10}, {'name': 'a/b/z.txt', 'size': 20},
                {'name': 'runner.lock', 'size': 10}, {'name': 'a/y.txt', 'size': 20},
                {'name': 'b.txt', 'size': 10}, {'name': 'a/b.txt', 'size': 20},
                {'name': 'archive.json', 'size': 10}]

    def test_prune_plan_keep_filter_and_removal_sort(self):
        payload = {'entries': self._entries(), 'run_id': 'r1', 'revision': 'rev0',
                   'terminal_status': 'ABORTED', 'archive': '/keep/run.zip', 'marker_exists': False}
        answered = self.assertSame('prune-plan', payload,
                                   reference.prune_plan(**payload), 'plan')
        # Depth first (a/b/z.txt has two slashes), then same-depth alphabetical
        # (a/b.txt before a/y.txt), then the shallow entry; KEEP names stay.
        self.assertEqual(answered['remove'], ['a/b/z.txt', 'a/b.txt', 'a/y.txt', 'b.txt'])
        self.assertEqual(answered['report']['files_to_remove'], 4)
        self.assertEqual(answered['report']['bytes_to_remove'], 70)
        self.assertEqual(answered['report']['terminal_status'], 'ABORTED')
        # The plan carries no archive_proof: the wrapper merges its own.
        self.assertEqual(answered['marker'],
                         {'format': 'formal-gates-prune-v1', 'run_id': 'r1', 'revision': 'rev0',
                          'archive': '/keep/run.zip', 'cleanup_complete': False})

    def test_prune_plan_existing_marker_reports_without_new_plan(self):
        payload = {'entries': self._entries(), 'run_id': 'r1', 'revision': 'rev0',
                   'terminal_status': 'ABORTED', 'archive': '/keep/run.zip', 'marker_exists': True}
        answered = self.assertSame('prune-plan', payload,
                                   reference.prune_plan(**payload), 'existing-marker')
        self.assertTrue(answered['ok'])
        self.assertIsNone(answered['marker'])


class RecordingBridge:
    """Forward every retention bridge call and log the operation footprint."""

    def __init__(self, inner, log):
        self.inner, self.log = inner, log

    def call(self, operation, payload):
        answer = self.inner.call(operation, payload)
        entries = payload.get('entries') if isinstance(payload, dict) else None
        self.log.append({'operation': operation,
                         'entries': len(entries) if isinstance(entries, list) else None,
                         'ok': bool(answer.get('ok')) if isinstance(answer, dict) else None})
        return answer


class RetentionSequenceTests(unittest.TestCase):
    """Wrapper-level orders over the real bridge: batch sequences and the
    prune operation order the op-level rows cannot see alone."""

    def setUp(self):
        fixtures.DriverTests.setUp(self)
        self.run_id = 'retention-seq-' + uuid.uuid4().hex
        old = self.directory
        self.directory = fixtures.PACKAGE / '.harness/runs' / self.run_id
        shutil.copytree(old, self.directory)
        self.addCleanup(shutil.rmtree, self.directory, True)
        policy = (fixtures.PACKAGE / 'prompts/reviewer-base.md').read_bytes()
        (self.directory / 'reviewer-base.md').write_bytes(policy)
        self.manifest.update(run_id=self.run_id,
                             runtime_identity=runtime_identity(fixtures.PACKAGE, str(fixtures.BINARY)),
                             reviewer_policy_digest=hashlib.sha256(policy).hexdigest())
        atomic(self.directory / 'manifest.json', self.manifest)
        self.archive = self.root / 'archives' / 'run.zip'
        env = patch.dict(os.environ, FORMAL_GATES_HARNESS_ROOT=str(fixtures.PACKAGE),
                         FORMAL_GATES_HARNESS_BINARY=str(fixtures.BINARY),
                         PYTHONPATH=str(fixtures.PACKAGE / 'runtime/langgraph'))
        env.start(); self.addCleanup(env.stop)

    def tearDown(self): fixtures.DriverTests.tearDown(self)

    def stopped(self):
        from formal_gates_harness.cli import run_graph
        first = run_graph(self.directory)
        return run_graph(self.directory, {'decision': 'abort', 'revision': first['revision']},
                         first['interrupts'][0]['id'])

    def recorded(self, log):
        real = retention_module.driver_bridge()
        return patch.object(retention_module, 'driver_bridge',
                            lambda: RecordingBridge(real, log))

    def test_second_batch_symlink_refusal_comes_after_first_batch_collection(self):
        self.stopped()
        bulk = self.directory / 'bulk'; bulk.mkdir()
        for i in range(2500): (bulk / f'{i:05d}.txt').write_text('x')
        external = self.root / 'must-keep'; external.write_text('sentinel')
        (self.directory / 'zz-link').symlink_to(external)  # sorts after every bulk file
        log = []
        with self.recorded(log):
            with self.assertRaisesRegex(HarnessError, 'ARCHIVE_UNSUPPORTED: symlink zz-link'):
                retention_module.archive_run(self.directory, self.archive)
        batches = [row for row in log if row['operation'] == 'archive-inventory']
        self.assertEqual(len(batches), 2)  # the walk spans two CHUNK-sized batches
        self.assertTrue(batches[0]['ok'])
        self.assertFalse(batches[1]['ok'])
        self.assertEqual(external.read_text(), 'sentinel')
        self.assertFalse(self.archive.exists())

    def test_checked_archive_judges_facts_before_chunked_entries(self):
        self.stopped()
        bulk = self.directory / 'bulk'; bulk.mkdir()
        for i in range(2500): (bulk / f'{i:05d}.txt').write_text('x')
        retention_module.archive_run(self.directory, self.archive)
        log = []
        with self.recorded(log):
            index = retention_module.checked_archive(self.archive)
        self.assertGreater(len(index['files']), 2500)
        calls = [row for row in log if row['operation'] == 'archive-verify']
        self.assertEqual(calls[0]['entries'], 0)  # the facts call precedes any entry batch
        self.assertTrue(all(row['entries'] <= reference.CHUNK for row in calls[1:]))
        self.assertGreater(len(calls), 2)  # the members span more than one batch
        self.assertTrue(all(row['ok'] for row in calls))

    def test_prune_marker_binding_precedes_staleness(self):
        self.stopped()
        retention_module.archive_run(self.directory, self.archive)
        retention_module.prune_run(self.directory, self.archive, confirm=True)
        other = self.archive.parent / 'copy.zip'
        shutil.copyfile(self.archive, other)  # same bytes, different binding
        log = []
        with self.recorded(log):
            with self.assertRaisesRegex(HarnessError,
                                        'ARCHIVE_BINDING: use the original cleanup archive'):
                retention_module.prune_run(self.directory, other, True)
        operations = [row['operation'] for row in log]
        self.assertIn('prune-marker', operations)
        self.assertNotIn('archive-stale', operations)  # binding fires first
        self.assertNotIn('prune-plan', operations)

    def test_foreign_manifest_nonstring_identity_refuses_bind(self):
        self.stopped()
        retention_module.archive_run(self.directory, self.archive)
        forged = self.root / 'forged.zip'
        with zipfile.ZipFile(self.archive) as src, zipfile.ZipFile(forged, 'w') as dst:
            for info in src.infolist():
                body = src.read(info.filename)
                if info.filename == 'archive-manifest.json':
                    manifest = json.loads(body); manifest['run_id'] = 7
                    body = json.dumps(manifest).encode()
                dst.writestr(info, body)
        with self.assertRaisesRegex(HarnessError, 'ARCHIVE_BINDING: wrong run/revision'):
            retention_module.prune_run(self.directory, forged, True)


if __name__ == '__main__':
    unittest.main()
