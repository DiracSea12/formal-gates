"""Small execution registry, separate from the graph and existing host catalog.

One entry owns launch/probe, native decoding and recovery for one transport.
No adapter receives a Flow, SQLite connection, approval capability or run state.
"""
from __future__ import annotations
from dataclasses import dataclass
import importlib
from pathlib import Path
from .infra import DogfoodError, RuntimeBlocked, file_proof

SOURCE_FILES = ('native-source.json', 'native-events.jsonl', 'answer.raw.txt', 'answer.json')

@dataclass(frozen=True)
class NativeAdapter:
    name: str
    module: str

    def implementation(self):
        return importlib.import_module(self.module, __package__)

    def capture(self):
        return self.implementation().capture_binding()

    def validate_binding(self, binding):
        if binding.get('provider') != self.name:
            raise DogfoodError('HOST_BINDING: provider differs')
        for name, proof in binding.get('files', {}).items():
            if file_proof(Path(name)) != proof:
                raise RuntimeBlocked(f'HOST_CHANGED: bound native executable/script changed: {name}')

    def execute(self, request, directory, probe=False):
        if request.get('binding'): self.validate_binding(request['binding'])
        return self.implementation().execute(request, directory, probe=probe)

    def reconcile(self, directory, process):
        return self.implementation().reconcile_completed_output(directory, process)

    def verify_source(self, directory, source):
        if source.get('provider') != self.name or not source.get('identity'):
            raise RuntimeBlocked('NATIVE_BINDING: missing or different provider/identity')
        proofs = source.get('files', {})
        if set(proofs) != set(SOURCE_FILES):
            raise RuntimeBlocked('EVIDENCE_MISSING: native source proofs absent')
        for name, proof in proofs.items():
            if file_proof(directory / name) != proof:
                raise RuntimeBlocked(f'EVIDENCE_CHANGED: {name}')


# This lists *execution* implementations, not another installation/alias list.
_ADAPTERS = {a.name: a for a in (
    NativeAdapter('dsh', '.native_dsh'), NativeAdapter('zcode', '.native_zcode'),
)}

def adapter_for(provider):
    try: return _ADAPTERS[provider]
    except KeyError as exc:
        raise RuntimeBlocked(f'UNSUPPORTED_NATIVE_HOST: {provider}; execution adapters: {", ".join(_ADAPTERS)}') from exc

def execution_hosts():
    return tuple(_ADAPTERS)

def normalize_provider(bridge, value):
    if value == 'fixture': return value
    normalized = value.strip().lower()
    for host in bridge.call('host-descriptors', {}):
        if normalized in [str(x).strip().lower() for x in (host['ID'], host['InstallName'], *(host.get('Aliases') or []))]:
            if host['Installable']: return host['InstallName']
    raise DogfoodError(f'Unknown host: {value}')

def host_catalog(bridge):
    return {'hosts': [{'id':h['ID'],'provider':h['InstallName'],'name':h['ManifestName'],
                       'entry_integration':h['Installable'],
                       'execution_adapter':h['InstallName'] in _ADAPTERS}
                     for h in bridge.call('host-descriptors', {}) if h['Installable']],
            'note':'Registration is not live readiness. Use doctor --provider HOST; unavailable capabilities fail before run creation.'}
