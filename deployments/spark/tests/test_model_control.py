import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest import mock

ROOT=Path(__file__).resolve().parents[1]


class Driver:
    def __init__(self):
        self.running=False; self.calls=[]; self.inflight=0; self.healthy=True; self.engine_idle=True
    def inspect(self): return {'running':self.running,'exit_code':0,'oom_killed':False,'status':'running' if self.running else 'exited'}
    def health(self): return self.healthy
    def idle(self): return self.engine_idle
    def admission(self): return {'protocol_version':1,'host_id':'h','boot_id':'b','model_id':'qwen','container_id':'a'*64,'observed_at':100,'known':True,'maintenance_active':True,'maintenance_revision':9,'in_flight':self.inflight}
    def start(self): self.calls.append('start'); self.running=True
    def stop(self): self.calls.append('stop'); self.running=False


class ModelControlTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec=importlib.util.spec_from_file_location('model_control',ROOT/'model_control.py')
        cls.m=importlib.util.module_from_spec(spec);spec.loader.exec_module(cls.m)

    def config(self):
        return {'version':1,'host_id':'h','model_id':'qwen','label':'Qwen','scope':'local',
                'container_id':'a'*64,'image_id':'sha256:'+'b'*64,
                'container_port':8888,'served_model':'qwen'}

    def controller(self, directory, driver=None, launch=None):
        driver=driver or Driver()
        gate=lambda:{'active':True,'revision':9}
        return self.m.ModelController(self.config(),Path(directory)/'model-operation.json',gate,driver,
                                      identity=lambda:('h','b','master'),now=lambda:100,
                                      sleep=lambda _:None,launch=launch or (lambda fn:fn()),attempts=2),driver

    def request(self,action='start',revision=1,op='op1'):
        return {'host_id':'h','boot_id':'b','expected_revision':revision,'maintenance_revision':9,'operation_id':op,'action':action}

    def test_start_health_then_graceful_stop_and_persistent_revision(self):
        with tempfile.TemporaryDirectory() as d:
            c,driver=self.controller(d)
            self.assertEqual(c.observe()['state'],'stopped')
            self.assertEqual(c.begin(self.request())['state'],'ready')
            r=c.begin(self.request('stop',2,'op2'))
            self.assertEqual(r['state'],'stopped');self.assertEqual(r['revision'],3)
            self.assertEqual(driver.calls,['start','stop'])
            c2,_=self.controller(d,driver)
            self.assertEqual(c2.observe()['revision'],3)

    def test_wrong_binding_revision_and_unpaused_gate_never_mutate(self):
        with tempfile.TemporaryDirectory() as d:
            c,driver=self.controller(d)
            for patch in [{'host_id':'remote'},{'boot_id':'old'},{'expected_revision':2},
                          {'maintenance_revision':8},{'action':'shutdown'},{'command':'kill'},
                          {'expected_revision':True}]:
                with self.assertRaises(ValueError):c.begin(dict(self.request(),**patch))
            c.read_maintenance=lambda:{'active':False,'revision':9}
            with self.assertRaises(ValueError):c.begin(self.request())
            self.assertEqual(driver.calls,[])

    def test_busy_and_duplicate_request_do_not_reissue_mutation(self):
        jobs=[]
        with tempfile.TemporaryDirectory() as d:
            c,driver=self.controller(d,launch=jobs.append)
            r=c.begin(self.request());self.assertTrue(r['busy'])
            with self.assertRaises(ValueError):c.begin(self.request())
            c2,_=self.controller(d,driver)
            self.assertTrue(c2.observe()['busy'])
            with self.assertRaises(ValueError):c2.begin(self.request('start',2,'op2'))
            jobs[0]()
            self.assertEqual(driver.calls,['start'])

    def test_drain_timeout_never_stops_running_model(self):
        driver=Driver();driver.running=True;driver.inflight=1
        with tempfile.TemporaryDirectory() as d:
            c,_=self.controller(d,driver)
            r=c.begin(self.request('stop'))
            self.assertEqual(r['state'],'error');self.assertEqual(r['error_code'],'model_drain_timeout')
            self.assertEqual(driver.calls,[]);self.assertTrue(driver.running)

    def test_unknown_gate_or_changed_maintenance_cannot_stop(self):
        for key,value in [('known',False),('maintenance_revision',8),('boot_id','old'),('in_flight',True)]:
            driver=Driver();driver.running=True
            old=driver.admission
            driver.admission=lambda old=old,key=key,value=value:dict(old(),**{key:value})
            with tempfile.TemporaryDirectory() as d:
                c,_=self.controller(d,driver)
                try:
                    r=c.begin(self.request('stop'))
                    self.assertEqual(r['state'],'error')
                except ValueError:
                    pass  # 就绪观测已拒绝时，不进入执行阶段。
                self.assertEqual(driver.calls,[])

    def test_start_timeout_does_not_report_ready_or_stop_automatically(self):
        driver=Driver();driver.healthy=False
        with tempfile.TemporaryDirectory() as d:
            c,_=self.controller(d,driver)
            r=c.begin(self.request())
            self.assertEqual(r['state'],'error');self.assertEqual(driver.calls,['start'])

    def test_disconnected_http_request_does_not_hide_running_engine_work(self):
        driver=Driver();driver.running=True;driver.engine_idle=False
        with tempfile.TemporaryDirectory() as d:
            c,_=self.controller(d,driver)
            r=c.begin(self.request('stop'))
            self.assertEqual(r['state'],'error')
            self.assertEqual(driver.calls,[])

    def test_stop_timeout_keeps_maintenance_barrier_while_daemon_may_still_stop(self):
        driver=Driver();driver.running=True
        def stop():
            driver.calls.append('stop')
            raise TimeoutError('daemon stop still in progress')
        driver.stop=stop
        with tempfile.TemporaryDirectory() as d:
            c,_=self.controller(d,driver)
            r=c.begin(self.request('stop'))
            self.assertTrue(r['busy'])
            self.assertEqual(r['state'],'error')
            self.assertEqual(r['error_code'],'model_stop_unconfirmed')
            c2,_=self.controller(d,driver)
            self.assertTrue(c2.observe()['busy'])

    def test_docker_adapter_checks_exact_identity_then_never_uses_force_kill(self):
        config=self.config()
        obj={'Id':config['container_id'],'Image':config['image_id'],
             'Config':{'Labels':{'io.lework.model-control':'v1'}},
             'State':{'Running':True,'ExitCode':0,'OOMKilled':False,'Paused':False,'Restarting':False,'Dead':False,'Status':'running'}}
        driver=self.m.DockerModel(config)
        calls=[]
        def transport(conn,method,path,expected=(200,)):
            calls.append((conn.path,method,path))
            return obj if method=='GET' else None
        with mock.patch.object(self.m,'request_json',side_effect=transport):
            driver.stop()
            self.assertEqual(calls[-1],('/var/run/docker.sock','POST','/containers/'+'a'*64+'/stop?t=-1'))
            obj['Id']='other';calls.clear()
            with self.assertRaises(ValueError):driver.start()
            self.assertTrue(all(method=='GET' for _,method,_ in calls))

    def test_health_uses_only_bound_model_unix_socket_not_lan_or_bridge(self):
        config=self.config()
        obj={'Id':config['container_id'],'Image':config['image_id'],
             'Config':{'Labels':{'io.lework.model-control':'v1'}},
             'State':{'Running':True,'ExitCode':0,'OOMKilled':False,'Paused':False,'Restarting':False,'Dead':False,'Status':'running'}}
        calls=[]
        def transport(conn,method,path,expected=(200,)):
            calls.append((conn.path,method,path))
            if conn.path=='/var/run/docker.sock':return obj
            return None if path=='/health' else {'data':[{'id':'qwen'}]}
        with mock.patch.object(self.m,'request_json',side_effect=transport):
            driver=self.m.DockerModel(config)
            self.assertTrue(driver.health())
            self.assertEqual(calls[-2:],[('/run/leros-model/admission.sock','GET','/health'),
                                         ('/run/leros-model/admission.sock','GET','/models')])
            obj['Image']='wrong';calls.clear()
            self.assertFalse(driver.health())
            self.assertTrue(all(c[0]=='/var/run/docker.sock' for c in calls))

    def test_configuration_rejects_remote_or_arbitrary_action_targets(self):
        for patch in [{'scope':'distributed'},{'container_port':True},{'container_port':0},{'container_id':'name'},{'health_url':'https://example.com'},
                      {'health_url':'http://127.0.0.1:80/path'},{'command':'poweroff'}, {'version':True}]:
            with self.assertRaises(ValueError):self.m.validate_config(dict(self.config(),**patch))


if __name__=='__main__':unittest.main()
