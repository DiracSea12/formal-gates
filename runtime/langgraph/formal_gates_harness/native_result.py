"""Strict result framing shared by native transports; no workflow control."""
import json
import re
from .infra import RuntimeBlocked

def decode_result(text: str) -> dict:
    """Accept one object, optionally fenced; never choose between competing results."""
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise RuntimeBlocked(f'NATIVE_RESULT: duplicate JSON field {key}')
            result[key] = value
        return result
    def nonfinite(value):
        raise RuntimeBlocked(f'NATIVE_RESULT: non-finite number {value}')
    body = text.strip()
    try:
        result = json.loads(body, object_pairs_hook=unique, parse_constant=nonfinite)
    except json.JSONDecodeError as original:
        fences = list(re.finditer(r'^```(?:json)?[ \t]*\r?\n(.*?)^```[ \t]*(?:\r?\n|\Z)',
                                  body, re.MULTILINE | re.DOTALL | re.IGNORECASE))
        if not fences and '```' not in body:
            start = re.search(r'(?m)^[ \t]*\{', body)
            if not start:
                raise RuntimeBlocked('NATIVE_RESULT: no unambiguous result object') from original
            decoder = json.JSONDecoder(object_pairs_hook=unique, parse_constant=nonfinite)
            offset = start.start() + len(start[0]) - 1
            try:
                result, length = decoder.raw_decode(body[offset:])
            except json.JSONDecodeError as exc:
                raise RuntimeBlocked('NATIVE_RESULT: invalid result object') from exc
            outside = body[:offset] + body[offset + length:]
        else:
            if len(fences) != 1 or body.count('```') != 2:
                raise RuntimeBlocked('NATIVE_RESULT: final message is not one unambiguous JSON object') from original
            match = fences[0]
            outside = body[:match.start()] + body[match.end():]
            try:
                result = json.loads(match[1], object_pairs_hook=unique, parse_constant=nonfinite)
            except json.JSONDecodeError as exc:
                raise RuntimeBlocked('NATIVE_RESULT: invalid JSON result fence') from exc
        if any(c in outside for c in '{}[]'):
            raise RuntimeBlocked('NATIVE_RESULT: competing structured content outside result')
    if not isinstance(result, dict):
        raise RuntimeBlocked('NATIVE_RESULT: final result must be an object')
    return result
