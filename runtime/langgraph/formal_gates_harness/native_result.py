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
from .infra import HarnessError, RuntimeBlocked, driver_bridge


def decode_result(text: str) -> dict:
    """Accept one object, optionally fenced; never choose between competing results."""
    answered = driver_bridge().call('frame-result', {'text': text})
    if answered.get('accepted'):
        return answered['result']
    refusal = answered.get('refusal')
    if not refusal:
        raise HarnessError('Go bridge returned an unusable frame-result answer')
    raise RuntimeBlocked(refusal)
