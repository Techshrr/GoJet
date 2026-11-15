import copy, sys, unittest
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from common import origin,validate
class Contract(unittest.TestCase):
 def test_origins(self):
  self.assertEqual(origin('https://www.linketic.com'),'https://www.linketic.com')
  for bad in ['http://www.linketic.com','https://www.linketic.com/','https://user@www.linketic.com','https://www.linketic.com/x','https://www.linketic.com\n','https://www.linketic.com:99999']:
   with self.assertRaises(ValueError):origin(bad)
 def test_reject_unbounded_request(self):
  for bad in [None,{},[],{'admin_email':'a@b.c','shell':'rm'}]:
   with self.assertRaises(ValueError):validate(bad)
 def test_extra_field_is_not_reported_as_missing(self):
  d=dict.fromkeys(['admin_email','admin_name','admin_password','smtp_host','smtp_port','smtp_from','smtp_user','smtp_password','turnstile_site_key','turnstile_secret','turnstile_token','webrisk_key'],'')
  d['cf-turnstile-response']='private-test-token'
  with self.assertRaisesRegex(ValueError,'不支持的字段') as caught:validate(d)
  self.assertNotIn('private-test-token',str(caught.exception))
  del d['admin_email']
  with self.assertRaisesRegex(ValueError,'字段不完整'):validate(d)
if __name__=='__main__':unittest.main()
