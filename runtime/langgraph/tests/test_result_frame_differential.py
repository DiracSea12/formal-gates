"""Differential check for the result-framing replacement unit (G1a-1).

The pre-migration implementation is kept verbatim in `reference_result.py` and
production never imports it. Both implementations are run over the fixed corpus
in `result_corpus.py`, which deliberately spans Unicode, empty collections and
refusal cases, and their accept/refuse verdicts, refusal reasons and accepted
values are compared.
"""
import json
import tempfile
import unittest
from pathlib import Path

from formal_gates_harness.infra import Bridge, RuntimeBlocked, atomic
from formal_gates_harness.native_dsh import decode_result
from reference_result import decode_result as reference_decode_result
from result_corpus import CASES

PACKAGE = Path(__file__).resolve().parents[3]
BINARY = PACKAGE / ".harness/bin/formal-gates"


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def run_reference(text):
    try:
        return ("accept", canonical(reference_decode_result(text)), None)
    except RuntimeBlocked as exc:
        return ("refuse", None, str(exc))


def run_go(bridge, text):
    try:
        value = bridge.call("frame-result", {"text": text})
    except Exception as exc:  # noqa: BLE001 - surfaced as a divergence below
        return ("error", None, f"{type(exc).__name__}: {exc}")
    if value.get("accepted"):
        return ("accept", canonical(value["result"]), None)
    return ("refuse", None, value.get("refusal"))


class ResultFrameDifferential(unittest.TestCase):
    def setUp(self):
        self.bridge = Bridge(str(BINARY))

    def test_corpus_verdicts_and_reasons_match(self):
        divergences = []
        for name, text in CASES:
            want = run_reference(text)
            got = run_go(self.bridge, text)
            if want[0] != got[0]:
                divergences.append(f"{name}: verdict {want[0]} vs {got[0]} ({want[2]} | {got[2]})")
            elif want[0] == "accept" and want[1] != got[1]:
                divergences.append(f"{name}: value {want[1]} vs {got[1]}")
            elif want[0] == "refuse" and want[2] != got[2]:
                divergences.append(f"{name}: reason {want[2]!r} vs {got[2]!r}")
        self.assertEqual(divergences, [], "corpus divergences:\n" + "\n".join(divergences))

    def test_corpus_covers_unicode_empty_and_refusals(self):
        """Coverage is asserted by outcome, not by how a case happens to be named."""
        accepted = refused = 0
        for _, text in CASES:
            if run_reference(text)[0] == "accept":
                accepted += 1
            else:
                refused += 1
        self.assertGreaterEqual(len(CASES), 50)
        self.assertGreaterEqual(accepted, 20, "corpus needs a substantial acceptance set")
        self.assertGreaterEqual(refused, 20, "corpus needs a substantial refusal set")
        self.assertTrue(any(text == "" for _, text in CASES), "corpus needs an empty input")
        self.assertTrue(any("日本" in text or "🚀" in text for _, text in CASES), "corpus needs Unicode")
        self.assertTrue(any(text in ("{}", "[]") for _, text in CASES), "corpus needs empty collections")

    def test_production_wrapper_agrees_with_reference(self):
        """The production entry point must behave like the implementation it replaced."""
        sample = [
            '{"status":"PASS","summary":"ok","findings":[],"check_ids":[]}',
            'Completed.\n\n```json\n{"status":"PASS"}\n```',
            '{"a":1,"a":2}',
            '{"x":NaN}',
            '[]',
        ]
        for text in sample:
            want = run_reference(text)
            try:
                value = decode_result(text)
                got = ("accept", canonical(value), None)
            except RuntimeBlocked as exc:
                got = ("refuse", None, str(exc))
            self.assertEqual(want, got, f"production wrapper differs for {text!r}")

    def test_documented_divergences_from_the_reference(self):
        """Two measured, bounded differences between the reference and Go.

        Both are recorded in the C1 acceptance record. They are asserted here so
        that any change in either direction is noticed rather than assumed.
        """
        # (1) A lone surrogate written as a JSON escape. Both implementations
        #     accept the frame, but Python preserves the surrogate code point
        #     while Go's decoder substitutes U+FFFD, because a lone surrogate is
        #     not representable in a UTF-8 Go string.
        escaped = '{"note":"\\ud800"}'
        want = run_reference(escaped)
        got = run_go(self.bridge, escaped)
        self.assertEqual(want[0], "accept")
        self.assertEqual(got[0], "accept")
        self.assertEqual(want[1], '{"note":"\\ud800"}')
        self.assertEqual(got[1], '{"note":"\\ufffd"}')
        self.assertNotEqual(want[1], got[1], "if these agree now, the record must be updated")

        # (2) A literal lone surrogate in the text. This fails on both sides with
        #     UnicodeEncodeError and writes no answer.json: before the migration
        #     the failure happened one step later, inside atomic(), because
        #     infra.canonical encodes with ensure_ascii=False. The failure point
        #     moved earlier; the outcome at both call sites did not change.
        literal = '{"note":"' + chr(0xD800) + '"}'
        self.assertEqual(run_reference(literal)[0], "accept")
        self.assertEqual(run_go(self.bridge, literal)[0], "error")
        with self.assertRaises(UnicodeEncodeError):
            atomic(Path(tempfile.mkdtemp()) / "answer.json", {"note": chr(0xD800)})

        # (3) An out-of-range float literal is NOT a framing divergence: with
        #     UseNumber the literal survives, so both paths accept it and both
        #     report +Inf. Where it does bite is the next step - canonicalising
        #     that value cannot cross the bridge. That end-to-end shape is
        #     asserted in test_canonical_differential.py.
        overflow = '{"n":1e400}'
        self.assertEqual(run_reference(overflow)[0], "accept")
        self.assertEqual(run_go(self.bridge, overflow)[0], "accept")
        self.assertEqual(run_reference(overflow)[1], run_go(self.bridge, overflow)[1])


if __name__ == "__main__":
    unittest.main()
