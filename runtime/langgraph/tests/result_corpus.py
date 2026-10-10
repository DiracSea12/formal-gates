"""Fixed sample corpus for the result-framing replacement unit.

Shared by the differential test (reference implementation vs the Go bridge).
The result-parser corpus deliberately includes Unicode, empty
collections and refusal cases, so that equivalent behaviour is checked on the
inputs where cross-language libraries tend to diverge.
"""

CASES: list[tuple[str, str]] = [
    # -- acceptance: plain shapes ------------------------------------------
    ("plain object", '{"status":"PASS"}'),
    ("empty object", "{}"),
    ("nested empty collections", '{"a":[],"b":{},"c":[[]],"d":{"e":{}}}'),
    ("surrounding whitespace", "\n\t {\"a\":1} \r\n"),
    ("single key", '{"a":null}'),
    ("deep nesting", '{"a":' * 12 + "1" + "}" * 12),
    ("large integer", '{"n":123456789012345678901234567890}'),
    ("exponent and negative zero", '{"a":1e2,"b":-0.0,"c":2E-3}'),
    ("booleans and nulls", '{"a":true,"b":false,"c":null}'),
    ("key order preserved by value not by order", '{"z":1,"a":2}'),

    # -- acceptance: unicode ----------------------------------------------
    ("non-bmp characters", '{"note":"日本語 🚀 café"}'),
    ("escaped unicode", '{"note":"\\u65e5\\u672c\\u8a9e \\ud83d\\ude80"}'),
    ("combining marks", '{"note":"e\\u0301"}'),
    ("rtl text", '{"note":"مرحبا"}'),
    ("unicode key", '{"名前":"値"}'),
    ("escaped braces inside string", '{"note":"{not: a result} and [not a list]"}'),
    ("string containing NaN", '{"note":"NaN"}'),
    ("string containing fence markers", '{"note":"```json"}'),
    ("nul escape", '{"note":"a\\u0000b"}'),

    # -- acceptance: fences ----------------------------------------------
    ("json fence", '```json\n{"a":1}\n```'),
    ("bare fence", '```\n{"a":1}\n```'),
    ("uppercase tag", '```JSON\n{"a":1}\n```'),
    ("mixed case tag", '```Json\n{"a":1}\n```'),
    ("crlf fence", '```json\r\n{"a":1}\r\n```'),
    ("fence without trailing newline", '```json\n{"a":1}\n```'),
    ("fence with leading prose", 'Here is the result.\n```json\n{"a":1}\n```'),

    # -- acceptance: bare object in prose --------------------------------
    ("prose then object on its own line", 'Explanation.\n\n{"a":1}'),
    ("object with trailing prose", '{"a":1}\nThat is all.'),
    ("object followed by a blank line", '{"a":1}\n\n'),

    # -- acceptance: prose that merely mentions non-finite words ------------
    ("prose mentions NaN before the object", 'Checked edge cases: NaN and Infinity.\n{"a":1}'),
    ("prose lists NaN after a colon", 'Values seen: NaN, Infinity.\n{"a":1}'),
    ("prose mentions NaN after the object", '{"a":1}\nNote: no NaN was used.'),
    ("prose mentions Infinity before a fence", 'Checked Infinity.\n```json\n{"a":1}\n```'),
    ("single empty key", '{"":1}'),

    # -- refusal: duplicates ---------------------------------------------
    ("duplicate top level", '{"a":1,"a":2}'),
    ("duplicate nested", '{"o":{"b":1,"b":2}}'),
    ("duplicate in fence", '```json\n{"a":1,"a":2}\n```'),
    ("duplicate escaped key", '{"\\u0061":1,"a":2}'),
    ("duplicate inside bare object", 'prose\n{"a":1,"a":2}'),
    ("duplicate empty key", '{"":1,"":2}'),
    ("duplicate empty key in fence", '```json\n{"":1,"":2}\n```'),

    # -- refusal: non-finite ---------------------------------------------
    ("nan value", '{"a":NaN}'),
    ("infinity value", '{"a":Infinity}'),
    ("negative infinity value", '{"a":-Infinity}'),
    ("nan in array", '{"a":[1,NaN]}'),
    ("nan top level", "NaN"),
    ("non-finite inside the object after prose", 'Checked NaN.\n{"a":NaN}'),
    ("non-finite inside the fence after prose", 'Checked Infinity.\n```json\n{"a":Infinity}\n```'),
    ("non-finite inside a bare object", 'prose\n{"a":-Infinity}'),

    # -- refusal: competing structured content ---------------------------
    ("two fences", '```json\n{"a":1}\n```\ntext\n```json\n{"a":2}\n```'),
    ("two documents on separate lines", '{"a":1}\n{"b":2}'),
    ("object followed by array", '{"a":1}\n[1]'),
    ("prose containing brackets", 'See [1].\n{"a":1}'),

    # -- refusal: top level is not an object ------------------------------
    ("top level array", "[1,2]"),
    ("top level empty array", "[]"),
    ("top level string", '"text"'),
    ("top level number", "42"),
    ("top level null", "null"),
    ("top level true", "true"),
    ("top level array in fence", '```json\n[1,2]\n```'),

    # -- refusal: malformed ----------------------------------------------
    ("empty input", ""),
    ("whitespace only", "   \n\t "),
    ("prose only", "I could not decide."),
    ("prose with a mid-line brace", 'Result {"a":1} plus [extra]'),
    ("unclosed fence", '```json\n{"a":1}'),
    ("unclosed brace", '{"a":1'),
    ("fence with invalid json", '```json\n{"a":}\n```'),
    ("bare object invalid json", 'prose\n{"a":}'),
    ("trailing comma", '{"a":1,}'),
    ("single quotes", "{'a':1}"),
    ("comment", '{"a":1} // done'),
    ("raw control character in string", '{"a":"line\nbreak"}'),
    ("byte order mark only", "\ufeff"),
    ("bom then object", '\ufeff{"a":1}'),
]
