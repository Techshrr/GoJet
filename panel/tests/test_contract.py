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
if __name__=='__main__':unittest.main()
