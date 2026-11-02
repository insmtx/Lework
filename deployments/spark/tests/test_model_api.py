import http.client
import json
from pathlib import Path
import sys
import tempfile
import threading
import unittest
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from maintenance_state import StateStore
from model_control import ModelController, UnixHTTP
import power_agent
from test_model_control import Driver
import test_model_control as fixture


class ModelAPITests(unittest.TestCase):
    def test_optin_model_route_and_busy_maintenance_guard(self):
        with tempfile.TemporaryDirectory() as d:
            store=StateStore(Path(d)/'maintenance','h');store.initialize();store.change(1,'maintenance',True)
            driver=Driver();old=driver.admission
            driver.admission=lambda:dict(old(),maintenance_revision=2)
            jobs=[]
            c=ModelController(fixture.ModelControlTests().config(),Path(d)/'model.json',store.read,driver,
                              identity=lambda:('h','b','master'),now=lambda:100,launch=jobs.append,sleep=lambda _:None)
            server=power_agent.make_server(str(Path(d)/'sock'),lambda:{},maintenance=store,
                                            identity=lambda:('h','b','master'),model=c)
            thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
            def request(method,path,body=None):
                conn=UnixHTTP(server.server_address)
                conn.request(method,path,json.dumps(body) if body else None,{'Content-Type':'application/json'})
                r=conn.getresponse();data=json.loads(r.read());code=r.status;conn.close();return code,data
            try:
                req=fixture.ModelControlTests().request();req['maintenance_revision']=2
                code,result=request('POST','/model',req)
                self.assertEqual(code,200);self.assertTrue(result['busy'])
                resume=dict(host_id='h',boot_id='b',expected_revision=2,operation_id='maintenance',active=False)
                self.assertEqual(request('POST','/maintenance',resume)[0],409)
                self.assertTrue(store.read()['active'])
                self.assertEqual(request('POST','/model',req)[0],409)
                self.assertEqual(request('POST','/model',dict(req,command='kill'))[0],409)
                jobs[0]()
                self.assertEqual(request('GET','/model')[1]['state'],'ready')
                self.assertEqual(request('POST','/maintenance',resume)[0],200)
                self.assertEqual(driver.calls,['start'])
            finally:
                server.shutdown();server.server_close();thread.join()


if __name__=='__main__':unittest.main()
