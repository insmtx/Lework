import importlib
import multiprocessing
from pathlib import Path
import sys
import tempfile
import time
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))

def pause_in_process(path, started, done):
    from maintenance_state import StateStore
    started.set()
    StateStore(path,'host').change(1,'parallel',True)
    done.set()


class MaintenanceOperationsTests(unittest.TestCase):
    def test_rollback_holds_state_lock_until_consumer_changes_finish(self):
        rollback=importlib.import_module('maintenance_rollback')
        from maintenance_state import StateStore
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'maintenance.json'
            store=StateStore(path,'host');store.initialize()
            ctx=multiprocessing.get_context('spawn');started=ctx.Event();done=ctx.Event()
            process=ctx.Process(target=pause_in_process,args=(path,started,done))
            def change_consumers():
                process.start()
                self.assertTrue(started.wait(3))
                self.assertFalse(done.wait(.2),'concurrent pause completed while rollback was removing its consumers')
                self.assertFalse(store.read()['active'])
                return 0
            try:
                self.assertEqual(rollback.run_rollback(store,change_consumers),0)
            finally:
                if process.pid:
                    process.join(3)
                    if process.is_alive():process.terminate();process.join()
            self.assertTrue(done.is_set())
            with self.assertRaises(ValueError):
                rollback.run_rollback(store,lambda:self.fail('active maintenance rolled back'))

    def test_shared_config_rechecks_new_and_zero_replica_consumers(self):
        deployment=importlib.import_module('maintenance_deployment')
        volume={'configMap':{'name':'leros-worker-config'}}
        allowed={'kind':'Deployment','metadata':{'uid':'target','name':'leros-worker-o1-w1'},'spec':{'template':{'spec':{'volumes':[volume]}}}}
        deployment.validate_consumers([allowed],'target')
        for other in [
            {'kind':'Deployment','metadata':{'uid':'new','name':'another-worker'},'spec':{'replicas':0,'template':{'spec':{'volumes':[volume]}}}},
            {'kind':'Job','metadata':{'uid':'job','name':'new-job'},'spec':{'template':{'spec':{'volumes':[volume]}}}},
            {'kind':'CronJob','metadata':{'uid':'cron','name':'future-worker'},'spec':{'jobTemplate':{'spec':{'template':{'spec':{'volumes':[volume]}}}}}},
        ]:
            with self.assertRaises(ValueError):deployment.validate_consumers([allowed,other],'target')

    def test_only_actual_target_replicaset_and_pods_are_allowed(self):
        deployment=importlib.import_module('maintenance_deployment')
        volume={'configMap':{'name':'leros-worker-config'}}
        rs={'kind':'ReplicaSet','metadata':{'uid':'rs','name':'worker-rs','ownerReferences':[{'kind':'Deployment','uid':'target','controller':True}]},'spec':{'template':{'spec':{'volumes':[volume]}}}}
        pod={'kind':'Pod','metadata':{'uid':'pod','name':'worker-pod','ownerReferences':[{'kind':'ReplicaSet','uid':'rs','controller':True}]},'spec':{'nodeName':'master','volumes':[volume]}}
        deployment.validate_consumers([rs,pod],'target')
        pod['metadata']['ownerReferences'][0]['uid']='unrelated'
        with self.assertRaises(ValueError):deployment.validate_consumers([rs,pod],'target')
