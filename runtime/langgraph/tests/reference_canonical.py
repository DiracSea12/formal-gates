"""Independent test oracle for canonical encoding and content digests.

Production code must not import this module."""
import hashlib
import json


def canonical(value) -> bytes:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()


def digest(value) -> str:
    return hashlib.sha256(canonical(value)).hexdigest()
