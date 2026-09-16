"""Differential check for the evidence proof unit (G1a-2, C2).

The pre-migration `file_proof` is kept verbatim in `reference_evidence.py` and
production never imports it. Both implementations are run over a corpus of real
files spanning empty, binary, Unicode-named, symlinked, unreadable and absent
entries, and their proofs and refusal reasons are compared.
"""
import os
import tempfile
import unittest
from pathlib import Path

from formal_gates_harness.infra import RuntimeBlocked, file_proof
from reference_evidence import file_proof as reference_file_proof


def proof_of(fn, path):
    try:
        return ("proof", fn(path), None)
    except RuntimeBlocked as exc:
        return ("refuse", None, str(exc))


class EvidenceProofDifferential(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self._tmp.name)

    def tearDown(self):
        self._tmp.cleanup()

    def write(self, name, body: bytes) -> Path:
        path = self.dir / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
        return path

    def corpus(self):
        cases = [
            ("empty", self.write("empty.bin", b"")),
            ("one byte", self.write("one.bin", b"x")),
            ("ascii", self.write("ascii.txt", b"hello\nworld\n")),
            ("crlf", self.write("crlf.txt", b"a\r\nb\r\n")),
            ("utf8 non-bmp", self.write("utf8.txt", "日本語 🚀 café\n".encode())),
            ("nul and high bytes", self.write("binary.bin", bytes([0, 1, 255, 254, 0, 127]))),
            ("unicode file name", self.write("日本語 🚀.txt", b"named\n")),
            ("nested path", self.write("a/b/c/deep.txt", b"deep\n")),
            ("large", self.write("large.bin", (bytes(range(251)) * 4300)[: (1 << 20) + 7])),
            ("no trailing newline", self.write("bare.txt", b"no newline")),
        ]
        link = self.dir / "link.txt"
        try:
            link.symlink_to(self.dir / "ascii.txt")
            cases.append(("symlink to file", link))
        except OSError:
            pass
        broken = self.dir / "broken.txt"
        try:
            broken.symlink_to(self.dir / "nowhere.txt")
            cases.append(("broken symlink", broken))
        except OSError:
            pass
        cases.append(("directory", self.dir))
        cases.append(("absent", self.dir / "absent.txt"))
        cases.append(("absent nested", self.dir / "no" / "such" / "file.txt"))
        return cases

    def test_corpus_proofs_and_refusals_match(self):
        divergences = []
        cases = self.corpus()
        for name, path in cases:
            want = proof_of(reference_file_proof, path)
            got = proof_of(file_proof, path)
            if want[0] != got[0]:
                divergences.append(f"{name}: {want[0]} vs {got[0]} ({want[2]} | {got[2]})")
            elif want[0] == "proof" and want[1] != got[1]:
                divergences.append(f"{name}: {want[1]} vs {got[1]}")
            elif want[0] == "refuse" and want[2] != got[2]:
                divergences.append(f"{name}: reason {want[2]!r} vs {got[2]!r}")
        self.assertEqual(divergences, [], "corpus divergences:\n" + "\n".join(divergences))
        self.assertGreaterEqual(len(cases), 12)

    def test_corpus_covers_acceptance_and_refusal(self):
        kinds = [proof_of(reference_file_proof, path)[0] for _, path in self.corpus()]
        self.assertGreaterEqual(kinds.count("proof"), 8)
        self.assertGreaterEqual(kinds.count("refuse"), 3)

    def test_proof_shape_is_unchanged(self):
        path = self.write("shape.txt", b"shape\n")
        proof = file_proof(path)
        self.assertEqual(sorted(proof), ["sha256", "size"])
        self.assertIsInstance(proof["sha256"], str)
        self.assertIsInstance(proof["size"], int)
        self.assertEqual(len(proof["sha256"]), 64)

    def test_unreadable_file_refuses_like_the_reference(self):
        path = self.write("unreadable.txt", b"secret\n")
        if os.geteuid() == 0:
            self.skipTest("running as root: permissions cannot be removed")
        os.chmod(path, 0o000)
        try:
            want = proof_of(reference_file_proof, path)
            got = proof_of(file_proof, path)
            self.assertEqual(want[0], got[0])
            self.assertEqual(want[2], got[2])
        finally:
            os.chmod(path, 0o644)

    def test_proof_changes_when_content_changes(self):
        path = self.write("mutable.txt", b"before\n")
        first = file_proof(path)
        path.write_bytes(b"after\n")
        second = file_proof(path)
        self.assertNotEqual(first["sha256"], second["sha256"])


if __name__ == "__main__":
    unittest.main()
