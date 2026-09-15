"""One supervised native invocation. Does not own workflow transitions."""
from pathlib import Path
import json
import sys
from .infra import atomic, read, SessionUnavailable
from .native_hosts import adapter_for

def main():
    directory = Path(sys.argv[1]).resolve()
    try:
        request = read(directory/'native-request.json')
        result = adapter_for(request['provider']).execute(request,directory,probe=bool(request.get('probe')))
        print(json.dumps(result,ensure_ascii=False)); return 0
    except Exception as exc:
        detail={'error':f'{type(exc).__name__}: {exc}'}
        if isinstance(exc,SessionUnavailable):
            detail.update(error_kind='SESSION_UNAVAILABLE',identity=exc.identity,method=exc.method,native_code=exc.code)
        atomic(directory/'native-error.json',detail)
        print(detail['error'],file=sys.stderr); return 1

if __name__=='__main__': raise SystemExit(main())
