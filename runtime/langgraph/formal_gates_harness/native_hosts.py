"""Small execution registry, separate from the graph and existing host catalog.

One entry owns launch/probe, native decoding and recovery for one transport.
No adapter receives a Flow, SQLite connection, approval capability or run state.
"""
from __future__ import annotations
from dataclasses import dataclass
import importlib
import json
from pathlib import Path
import subprocess
import sys
from .infra import (HarnessError, RuntimeBlocked, SessionUnavailable, atomic, driver_bridge,
                    file_proof, read, refuse)
from .native_transport import SOURCE_FILES

@dataclass(frozen=True)
class NativeAdapter:
    name: str
    module: str

    def entry_argv(self, binary, directory):
        return [sys.executable, '-m', 'formal_gates_harness.native_entry', str(directory)]

    def implementation(self):
        return importlib.import_module(self.module, __package__)

    def capture(self):
        return self.implementation().capture_binding()

    def validate_binding(self, binding):
        if binding.get('provider') != self.name:
            raise HarnessError('HOST_BINDING: provider differs')
        for name, proof in binding.get('files', {}).items():
            if file_proof(Path(name)) != proof:
                raise RuntimeBlocked(f'HOST_CHANGED: bound native executable/script changed: {name}')

    def execute(self, request, directory, probe=False):
        if request.get('binding'): self.validate_binding(request['binding'])
        return self.implementation().execute(request, directory, probe=probe)

    def reconcile(self, directory, process):
        return self.implementation().reconcile_completed_output(directory, process)

    def transport_files(self):
        module = self.implementation()
        if hasattr(module, 'TRANSPORT_FILES'):
            return module.TRANSPORT_FILES
        return (Path(module.__file__).relative_to(Path(__file__).parent).as_posix(),)

    def verify_source(self, directory, source):
        if source.get('provider') != self.name or not source.get('identity'):
            raise RuntimeBlocked('NATIVE_BINDING: missing or different provider/identity')
        proofs = source.get('files', {})
        if set(proofs) != set(SOURCE_FILES):
            raise RuntimeBlocked('EVIDENCE_MISSING: native source proofs absent')
        for name, proof in proofs.items():
            if file_proof(directory / name) != proof:
                raise RuntimeBlocked(f'EVIDENCE_CHANGED: {name}')


def _message_without_type_prefix(text):
    """'RuntimeBlocked: <msg>' -> '<msg>'; a text without that shape stays as is."""
    name, separator, message = text.partition(': ')
    if separator and name.isidentifier():
        return message
    return text


def _refuse_when_declined(answered):
    """Apply refuse() to a bridge answer that is a typed refusal envelope.

    native-capture and native-reconcile answer a success with their payload
    ({'binding'/'source'}) rather than an ok flag, so only an explicit
    {"ok": false, 'kind', 'refusal'} envelope is a refusal; the exception
    family still travels with the refusal, exactly like refuse().
    """
    if answered.get('ok') is False:
        refuse(answered)


@dataclass(frozen=True)
class GoAdapter:
    """A host whose execution implementation lives in the Go driver binary.

    execute() is the probe/doctor path: it writes the same native-request.json
    contract the workers use, runs `formal-gates harness native <dir>` as its
    own process and translates native-error.json back into the exception the
    retired in-process implementation used to raise (the workers path spawns
    the same command through the supervisor and reads the same error record).
    The four lifecycle operations go through the bridge like every other Go
    verification; refusals keep refuse()'s exception-family mapping.
    """
    name: str

    def entry_argv(self, binary, directory):
        return [binary, 'harness', 'native', str(directory)]

    def execute(self, request, directory, probe=False):
        directory = Path(directory)
        atomic(directory / 'native-request.json', {**request, 'probe': bool(probe)})
        completed = subprocess.run(self.entry_argv(driver_bridge().binary, directory),
                                   capture_output=True)
        if completed.returncode == 0:
            return json.loads(completed.stdout.decode('utf-8'))
        record = directory / 'native-error.json'
        if not record.exists():
            # The command failed without landing its error record; raise the
            # raw text it did print rather than guessing a typed cause.
            raise HarnessError(completed.stderr.decode('utf-8', 'replace').strip()
                               or 'native execution failed without an error record')
        detail = read(record)
        message = _message_without_type_prefix(detail.get('error', ''))
        if detail.get('error_kind') == 'SESSION_UNAVAILABLE':
            raise SessionUnavailable(message, identity=detail.get('identity', ''),
                                     method=detail.get('method', 'session/resume'),
                                     code=detail.get('native_code'))
        raise RuntimeBlocked(message)

    def capture(self):
        answered = driver_bridge().call('native-capture', {'provider': self.name})
        _refuse_when_declined(answered)
        return answered['binding']

    def validate_binding(self, binding):
        refuse(driver_bridge().call('native-verify-binding',
                                    {'provider': self.name, 'binding': binding}))

    def verify_source(self, directory, source):
        refuse(driver_bridge().call('native-verify-source',
                                    {'provider': self.name, 'directory': str(directory), 'source': source}))

    def reconcile(self, directory, process):
        answered = driver_bridge().call('native-reconcile',
                                        {'provider': self.name, 'directory': str(directory), 'process': process})
        _refuse_when_declined(answered)
        return answered['source']

    def transport_files(self):
        # The transport mechanics live inside the Go driver binary, so only the
        # host-side plugin belongs to this package's transport inventory.
        return ('native_dsh.mjs',)


# This lists *execution* implementations, not another installation/alias list.
_ADAPTERS = {a.name: a for a in (
    GoAdapter('dsh'), NativeAdapter('zcode', '.native_zcode'),
)}

def adapter_for(provider):
    try: return _ADAPTERS[provider]
    except KeyError as exc:
        raise RuntimeBlocked(f'UNSUPPORTED_NATIVE_HOST: {provider}; execution adapters: {", ".join(_ADAPTERS)}') from exc

def execution_hosts():
    return tuple(_ADAPTERS)

def normalize_provider(bridge, value):
    normalized = value.strip().lower()
    if normalized == 'fixture' or normalized in _ADAPTERS:
        return normalized
    for host in bridge.call('host-descriptors', {}):
        if normalized in [str(x).strip().lower() for x in (host['ID'], host['InstallName'], *(host.get('Aliases') or []))]:
            if host['Installable']: return host['InstallName']
    raise HarnessError(f'Unknown host: {value}')

def host_catalog(bridge):
    hosts = [{'id':h['ID'],'provider':h['InstallName'],'name':h['ManifestName'],
              'entry_integration':True,'execution_adapter':h['InstallName'] in _ADAPTERS}
             for h in bridge.call('host-descriptors', {}) if h['Installable']]
    listed = {h['provider'] for h in hosts}
    hosts += [{'id':name,'provider':name,'name':name,'entry_integration':False,'execution_adapter':True}
              for name in _ADAPTERS if name not in listed]
    return {'hosts': hosts,
            'note':'Registration is not live readiness. Use doctor --provider HOST; unavailable capabilities fail before run creation.'}
