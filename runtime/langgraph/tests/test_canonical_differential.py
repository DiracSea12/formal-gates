"""Compare canonical bytes and digests against an independent encoding oracle.

The fixed corpus pins the harness content-addressing format byte for byte."""
import random
import unittest

from formal_gates_harness.infra import Bridge, _transport_bytes, canonical, digest

PACKAGE = __import__("pathlib").Path(__file__).resolve().parents[3]
from reference_canonical import canonical as reference_canonical
from reference_canonical import digest as reference_digest


def corpus():
    values = [
        {}, [], [{}], {"a": 1}, {"b": 2, "a": 1}, {"z": 1, "A": 2, "a": 3},
        {"": 1}, {"": {"": []}}, {"日本": [1, 2, 3]}, {"k<x>": 1},
        {"a": None, "b": True, "c": False},
        {"n": 0}, {"n": -1}, {"n": 123456789012345678901234567890},
        # Arbitrary precision is part of the contract, and these are the values
        # that a float64-narrowing step anywhere in the chain would reject.
        {"n": 2 ** 1024 - 1}, {"n": 10 ** 309}, {"n": 10 ** 400}, {"n": -(2 ** 512)},
        {"n": 2 ** 63}, {"n": 2 ** 64}, {"n": 2 ** 200},
        {"a": [2 ** 200, {"b": 2 ** 1024 - 1}], "c": 10 ** 309},
        {"n": -0}, {"n": 0.0}, {"n": -0.0}, {"n": 1.0}, {"n": 100.0}, {"n": 1.5}, {"n": -2.75},
        {"n": 0.1}, {"n": 0.1 + 0.2}, {"n": 1e14}, {"n": 1e15}, {"n": 1e16}, {"n": 1e21},
        {"n": 1e-4}, {"n": 1e-5}, {"n": 0.0001}, {"n": 0.00001},
        {"n": 5e-324}, {"n": 2.5e-308}, {"n": 1.7976931348623157e308},
        {"n": 3.141592653589793}, {"n": 1234567890123456789.0}, {"n": 1.2345678901234568e18},
        {"n": 0.0},
        {"s": ""}, {"s": "plain"}, {"s": 'quote" back\\ slash'},
        {"s": "tab\tnl\ncr\rbs\bff\f"}, {"s": "\u0000\u0001\u001f"},
        {"s": "a<b>c&d"}, {"s": "line\u2028sep\u2029par"},
        {"s": "日本語 🚀 café"}, {"s": "مرحبا"}, {"s": "e\u0301"},
        {"s": "```json"}, {"s": "NaN"}, {"s": "null"}, {"s": "{}"}, {"s": "[]"},
        {"deep": {"a": [{"b": [{"c": 1}]}]}},
        {"mixed": [1, "2", 3.5, None, True, {}, []]},
        {"u": "\u00e9\u4e2d\U0001F600"},
        {"bool": [True, False, None]},
    ]

    rng = random.Random(20260916)
    for _ in range(300):
        values.append({"n": rng.uniform(-1e6, 1e6) * (10 ** rng.randint(-20, 20))})
    for _ in range(50):
        values.append({"n": rng.randint(-(10 ** 18), 10 ** 18)})
    for _ in range(100):
        values.append({"v": random_value(rng)})
    return values


def random_value(rng, depth=0):
    if depth > 2 or rng.random() < 0.35:
        return rng.choice(
            [
                None,
                True,
                False,
                rng.randint(-99, 99),
                round(rng.uniform(-1e3, 1e3), rng.randint(0, 8)),
                "".join(rng.choice('ab<> &"\\\n\t日本語🚀') for _ in range(rng.randint(0, 8))),
            ]
        )
    if rng.random() < 0.7:
        return [random_value(rng, depth + 1) for _ in range(rng.randint(0, 4))]
    return {rng.choice(["a", "b", "", "key", "日本", "z z"]): random_value(rng, depth + 1) for _ in range(rng.randint(0, 4))}


class CanonicalDifferential(unittest.TestCase):
    def test_corpus_bytes_and_digests_match(self):
        values = corpus()
        self.assertGreaterEqual(len(values), 400)
        mismatches = []
        for value in values:
            want, got = reference_canonical(value), canonical(value)
            if want != got:
                mismatches.append(f"bytes for {value!r}:\n  python {want!r}\n  go     {got!r}")
                continue
            if reference_digest(value) != digest(value):
                mismatches.append(f"digest for {value!r}")
        self.assertEqual(mismatches[:5], [], f"{len(mismatches)} mismatch(es):\n" + "\n".join(mismatches[:5]))

    def test_corpus_is_substantially_random(self):
        values = corpus()
        floatish = sum(
            1 for v in values if isinstance(v, dict) and isinstance(v.get("n"), float)
        )
        self.assertGreaterEqual(floatish, 300, "the corpus must be dominated by measured float renderings")

    def test_known_boundary_renderings(self):
        cases = {
            1e15: '{"n":1000000000000000.0}',
            1e16: '{"n":1e+16}',
            1e21: '{"n":1e+21}',
            1e-4: '{"n":0.0001}',
            1e-5: '{"n":1e-05}',
            5e-324: '{"n":5e-324}',
            -0.0: '{"n":-0.0}',
            100.0: '{"n":100.0}',
        }
        for value, expected in cases.items():
            with self.subTest(value=value):
                self.assertEqual(canonical({"n": value}).decode(), expected)

    def test_transport_encoding_is_not_a_second_canonical_implementation(self):
        """The bridge envelope must stay a transport encoding, not the format.

        If this ever equals the canonical bytes, the envelope has quietly become
        a second implementation of the content-addressing format.
        """
        value = {"b": 1, "a": 2}
        envelope = {"operation": "x", "payload": {"value": value}}
        self.assertNotEqual(_transport_bytes(envelope), canonical(envelope))
        self.assertEqual(_transport_bytes(envelope).decode(), '{"operation":"x","payload":{"value":{"b":1,"a":2}}}')

    def test_non_finite_values_cannot_cross_the_transport(self):
        """The JSON transport refuses non-finite values before canonical encoding.

        The encoder can render an over-range numeric token received as raw JSON,
        but Python non-finite floats cannot cross the bridge envelope."""
        with self.assertRaises(Exception):
            canonical({"n": float("inf")})

        # The encoder does render it; the boundary is the envelope, not the
        # format. Sending the literal as raw JSON reaches the encoder directly.
        import subprocess

        raw = '{"operation":"canonical","payload":{"value":{"n":1e400}}}'
        sent = subprocess.run(
            [str(PACKAGE / ".harness/bin/formal-gates"), "harness", "bridge"],
            input=raw.encode(), capture_output=True,
        )
        rendered = sent.stdout.decode()
        self.assertIn("Infinity", rendered)
        self.assertNotIn("1e400", rendered)

    def test_over_range_literal_is_accepted_then_cannot_be_canonicalised(self):
        """An over-range result literal can pass framing but cannot cross canonical transport."""
        bridge = Bridge(str(PACKAGE / ".harness/bin/formal-gates"))
        accepted = bridge.call("frame-result", {"text": '{"n":1e400}'})
        self.assertTrue(accepted["accepted"])
        self.assertEqual(accepted["result"], {"n": float("inf")})
        with self.assertRaises(Exception):
            canonical(accepted["result"])

    def test_digest_is_the_sha256_of_the_canonical_bytes(self):
        import hashlib

        value = {"status": "PASS", "note": "x<y & z", "n": 1e16}
        self.assertEqual(digest(value), hashlib.sha256(canonical(value)).hexdigest())


if __name__ == "__main__":
    unittest.main()
