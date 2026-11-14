"""Exercise the actual HTTP handler with isolated filesystem and no root/network effects."""
import hashlib, http.client, importlib.util, json, sys, tempfile, threading, unittest
from pathlib import Path
from unittest.mock import patch
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import common
class WebSecurity(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  cls.tmp=tempfile.TemporaryDirectory();cls.root=Path(cls.tmp.name);cls.inbox=cls.root/'inbox';cls.status=cls.root/'status';cls.inbox.mkdir();cls.status.mkdir()
  common.atomic(cls.status/'entry.json',{'origin':'https://www.linketic.com','code_sha256':hashlib.sha256(b'long-installation-code').hexdigest()})
  common.atomic(cls.status/'progress.json',{'phase':'ready','step':'ready','completed':[]})
  cls.p1=patch.object(common,'PUBLIC',cls.status);cls.p2=patch.object(common,'INBOX',cls.inbox);cls.p1.start();cls.p2.start()
  spec=importlib.util.spec_from_file_location('panel_serve_test',Path(__file__).resolve().parents[1]/'serve.py');cls.module=importlib.util.module_from_spec(spec);spec.loader.exec_module(cls.module)
  cls.server=cls.module.ThreadingHTTPServer(('127.0.0.1',0),cls.module.Handler);threading.Thread(target=cls.server.serve_forever,daemon=True).start()
 @classmethod
 def tearDownClass(cls):cls.server.shutdown();cls.server.server_close();cls.p1.stop();cls.p2.stop();cls.tmp.cleanup()
 def request(self,path,data=None,headers=None):
  c=http.client.HTTPConnection('127.0.0.1',self.server.server_port);h={'Content-Type':'application/json','Origin':'https://www.linketic.com',**(headers or {})};c.request('POST' if data is not None else 'GET',path,json.dumps(data) if data is not None else None,h);r=c.getresponse();out=(r.status,dict(r.getheaders()),r.read());c.close();return out
 def test_auth_and_origin_and_csrf(self):
  self.assertEqual(self.request('/install/api/status')[0],401)
  self.assertEqual(self.request('/install/api/login',{'code':'long-installation-code'},{'Origin':'https://evil.example'})[0],403)
  self.assertEqual(self.request('/install/api/login',{'code':'wrong'})[0],401)
  code,h,raw=self.request('/install/api/login',{'code':'long-installation-code'});self.assertEqual(code,200);self.assertIn('Secure',h['Set-Cookie']);self.assertIn('HttpOnly',h['Set-Cookie']);self.assertIn('SameSite=Strict',h['Set-Cookie'])
  cookie=h['Set-Cookie'].split(';')[0];csrf=json.loads(raw)['csrf']
  self.assertEqual(self.request('/install/api/status',headers={'Cookie':cookie})[0],200)
  self.assertEqual(self.request('/install/api/configure',{},headers={'Cookie':cookie})[0],403)
  self.assertEqual(self.request('/install/api/configure',{},headers={'Cookie':cookie,'X-CSRF-Token':csrf})[0],400)
  self.assertFalse((self.inbox/'request.json').exists())
 def test_z_completed_denies_all_mutations(self):
  common.atomic(self.status/'complete.json',{'complete':True})
  for path in ['login','configure','retry']:
   self.assertEqual(self.request('/install/api/'+path,{'code':'long-installation-code'})[0],410)
  code,_,body=self.request('/install/');self.assertEqual(code,200);self.assertIn('安装已完成'.encode(),body)
if __name__=='__main__':unittest.main()
