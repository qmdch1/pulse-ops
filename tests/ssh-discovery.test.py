import importlib.util,pathlib,tempfile,unittest
path=pathlib.Path(__file__).resolve().parents[1]/'scripts/discover-ssh.py'
spec=importlib.util.spec_from_file_location('ssh_discovery',path);module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
class SSHTests(unittest.TestCase):
 def test_concrete_hosts_only_and_no_command_execution(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'config';p.write_text('Host *\n User default\nHost server-a\n HostName 192.0.2.10\n Port 2222\n IdentityFile /do/not/read\n ProxyCommand touch /do/not/run\nHost *.internal !skip\n User ignored\nHost server-b\n HostName 192.0.2.11\n ProxyJump server-a\n')
   result=module.discover(p,'Test');self.assertEqual(len(result),2);self.assertEqual(result[0]['port'],'2222');self.assertEqual(result[0]['user'],'default');self.assertEqual(result[1]['proxyJump'],'server-a');self.assertNotIn('IdentityFile',str(result));self.assertNotIn('ProxyCommand',str(result))
 def test_include_and_cycle(self):
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d);(root/'config').write_text('Include extra\n');(root/'extra').write_text('Include config\nHost included\n HostName 192.0.2.9\n');self.assertEqual(module.discover(root/'config','Test')[0]['alias'],'included')
if __name__=='__main__':unittest.main()
