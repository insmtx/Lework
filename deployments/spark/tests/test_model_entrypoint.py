import importlib.util
from pathlib import Path
import types
import unittest

ROOT=Path(__file__).resolve().parents[1]


class EntryPointTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec=importlib.util.spec_from_file_location('model_entrypoint',ROOT/'model_entrypoint.py')
        cls.m=importlib.util.module_from_spec(spec);spec.loader.exec_module(cls.m)

    def test_installs_before_builder_freezes_stack_and_preserves_application(self):
        apps=[]
        class App:
            def __init__(self, **kwargs):
                self.frozen=False;self.options=kwargs;apps.append(self)
            def add_middleware(self,kind,**kwargs):
                if self.frozen:raise RuntimeError('Cannot add middleware after an application has started')
                self.kind,self.kwargs=kind,kwargs
        module=types.SimpleNamespace(FastAPI=App)
        def build(*args,**kwargs):
            app=module.FastAPI(lifespan='preserved')
            app.frozen=True
            return app
        module.build_app=build
        gate=object();observed=[]
        self.m.install_gate(module,gate,lambda:observed.append(True))
        app=module.build_app(None)
        self.assertIs(app,apps[0])
        self.assertIsInstance(app,App)
        self.assertEqual(app.options,{'lifespan':'preserved'})
        self.assertEqual(app.kind.__name__,'AdmissionMiddleware')
        self.assertIs(app.kwargs['gate'],gate)
        self.assertIs(module.FastAPI,App)
        self.assertEqual(observed,[True])
        with self.assertRaises(ValueError):module.build_app(None)

    def test_failed_or_bypassed_construction_never_publishes_observation(self):
        class App:
            def add_middleware(self,*args,**kwargs):pass
        for mode in ('failure','bypass','multiple'):
            module=types.SimpleNamespace(FastAPI=App)
            def build(*args,**kwargs):
                if mode=='failure':raise RuntimeError('build failed')
                if mode=='bypass':return App()
                module.FastAPI();return module.FastAPI()
            module.build_app=build
            observed=[]
            self.m.install_gate(module,object(),lambda:observed.append(True))
            with self.assertRaises((RuntimeError,ValueError)):module.build_app(None)
            self.assertEqual(observed,[])
            self.assertIs(module.FastAPI,App)

    def test_requires_single_api_process_and_production_mode(self):
        self.assertEqual(self.m.validate_args(['vllm','serve','/model'],{}),['serve','/model','--api-server-count','1'])
        for args,env in [(['serve','/m','--api-server-count','2'],{}),
                         (['serve','/m','--api-server-count=2'],{}),
                         (['serve','/m'],{'VLLM_SERVER_DEV_MODE':'1'}),(['chat','/m'],{}),
                         (['serve','/m'],{'VLLM_RUST_FRONTEND_PATH':'/rust'}),
                         (['serve','/m','--data-parallel-multi-port-external-lb'],{}),
                         (['serve','/m','--config=/tmp/options.yaml'],{})]:
            with self.assertRaises(ValueError):self.m.validate_args(args,env)


if __name__=='__main__':unittest.main()
