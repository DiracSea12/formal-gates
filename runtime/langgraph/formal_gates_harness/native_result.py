"""Result framing adapter.

The framing decision ("is there exactly one unambiguous result object here?")
is owned by Go: `internal/harness.FrameResult`, reached through the
`frame-result` bridge operation. This module is the transitional wrapper: it
converts the input, calls Go, and translates a typed refusal back into
RuntimeBlocked carrying the reason Go produced.

It deliberately contains no framing rules of its own. The refusal reasons are
observable behaviour (existing tests assert them and they reach user-facing
reports), so they are passed through verbatim rather than rewritten here.
"""
from pathlib import Path

from .infra import Bridge, HarnessError, RuntimeBlocked

_BRIDGE = None


def bridge() -> Bridge:
    """Resolve the driver binary the same way the development entry does."""
    global _BRIDGE
    if _BRIDGE is None:
        from .cli import binary_path
        try:
            binary = binary_path()
        except HarnessError:
            # No binding exported (a unit test importing this module directly).
            # Fall back to the location scripts/harness and the test fixtures
            # already use, so there is still exactly one driver binary.
            binary = str(Path(__file__).resolve().parents[3] / '.harness' / 'bin' / 'formal-gates')
        _BRIDGE = Bridge(binary)
    return _BRIDGE


def decode_result(text: str) -> dict:
    """Accept one object, optionally fenced; never choose between competing results."""
    answered = bridge().call('frame-result', {'text': text})
    if answered.get('accepted'):
        return answered['result']
    refusal = answered.get('refusal')
    if not refusal:
        raise HarnessError('Go bridge returned an unusable frame-result answer')
    raise RuntimeBlocked(refusal)
