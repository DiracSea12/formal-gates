"""Small real-process regressions. No model calls or formal-run writes."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

from formal_gates_dogfood.cli import make_demo, package_root, run_graph
from formal_gates_dogfood.infra import Bridge, DogfoodError, Store, read, run_lock
from formal_gates_dogfood.native_dsh import observe

class Boundaries(unittest.TestCase):
    def setUp(self):
        root=Path(__file__).resolve().parents[3]
        self.environment=patch.dict(os.environ,{
            "FORMAL_GATES_DOGFOOD_ROOT":str(root),
            "FORMAL_GATES_DOGFOOD_BINARY":str(root/".dogfood/bin/formal-gates")})
        self.environment.start()
        self.addCleanup(self.environment.stop)
    def test_artifact_integrity(self):
        with tempfile.TemporaryDirectory() as directory:
            store=Store(Path(directory),{},Bridge('unused'))
            ref=store.put({'result':'PASS'});self.assertEqual(store.get(ref),{'result':'PASS'})
            (Path(directory)/'artifacts'/f'{ref}.json').write_text('{}')
            with self.assertRaises(DogfoodError):store.get(ref)
    def test_lock(self):
        with tempfile.TemporaryDirectory() as directory:
            with run_lock(Path(directory)):
                with self.assertRaises(DogfoodError):
                    with run_lock(Path(directory)):pass
    def test_bad_native_completion_is_not_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            (root/'native-source.json').write_text(json.dumps({'sessionId':'test','turn':1}))
            (root/'native-events.jsonl').write_text(json.dumps({'sessionId':'test','event':{'type':'turn/end','data':{'turn':1,'reason':{'kind':'failed'}}}})+'\n')
            with self.assertRaises(DogfoodError):observe(root,'test')
    def test_controller_death_reattaches_same_process(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            command="from pathlib import Path;import time; p=Path('count');p.write_text(str(int(p.read_text())+1) if p.exists() else '1');time.sleep(1.5)"
            request={'kind':'check','argv':[sys.executable,'-c',command],'cwd':str(root),'stdin':'','environment':{}}
            (root/'request-input.json').write_text(json.dumps(request))
            code="from pathlib import Path;import json,sys;from formal_gates_dogfood.infra import Store,Bridge;root=Path(sys.argv[1]);s=Store(root,{},Bridge('unused'));print(json.dumps(s.job('operation',json.loads((root/'request-input.json').read_text()))))"
            log=(root/'observer.log').open('wb')
            first=subprocess.Popen([sys.executable,'-c',code,str(root)],stdout=log,stderr=log)
            deadline=time.monotonic()+20
            while not (root/'count').exists() and time.monotonic()<deadline:time.sleep(.05)
            try:
                self.assertTrue((root/'count').exists(),'job never started')
                first.kill();first.wait(timeout=5)
                second=subprocess.run([sys.executable,'-c',code,str(root)],text=True,capture_output=True,timeout=20)
                self.assertEqual(second.returncode,0,second.stderr)
                self.assertEqual(json.loads(second.stdout)['exit_code'],0)
                self.assertEqual((root/'count').read_text(),'1')
            finally:
                if first.poll() is None:first.kill();first.wait()
                log.close()
    def test_real_fixture_loop_and_stale_answer(self):
        directory=make_demo(package_root(),native=False)
        first=run_graph(directory)
        self.assertEqual(first['status'],'WAITING_USER')
        with self.assertRaises(DogfoodError):run_graph(directory,{'decision':'approve','revision':'wrong'})
        with self.assertRaises(DogfoodError):run_graph(directory,{'decision':'approve','revision':first['revision'],'goto':'seal'})
        # Test-only fixture authorization; no actual user decision is fabricated.
        for _ in range(2):
            first=run_graph(directory,{'decision':'approve','revision':first['revision']},first['interrupts'][0]['id'])
        self.assertEqual(first['status'],'DEMO_COMPLETE',first)
        self.assertEqual(first['round'],1)
        events=[json.loads(line) for line in (directory/'events.jsonl').read_text().splitlines()]
        cases=[e['check'] for e in events if e['event']=='check-complete']
        self.assertEqual(cases.count('addition'),2)
        self.assertEqual(cases.count('unchanged'),1)
        jobs=len(list((directory/'jobs').iterdir()))
        again=run_graph(directory)
        self.assertEqual(again['candidate'],first['candidate'])
        self.assertEqual(len(list((directory/'jobs').iterdir())),jobs)

if __name__=='__main__':unittest.main()
