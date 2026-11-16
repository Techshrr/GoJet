// Acceptance harness only: both editions run the same production UI and backend.
import {execFileSync} from 'node:child_process';
export function runtime(command,args,options={}){
 if(command!=='docker'||!process.env.NATIVE_TEST)return execFileSync(command,args,options);
 if(args[0]!=='exec')throw Error('Native test attempted a Docker lifecycle operation');
 const at=args.indexOf('linketic-ci');if(at<0)throw Error('Unexpected test runtime');
 const argv=args.slice(at+1);if(!argv[0].startsWith('/'))argv[0]='/usr/bin/'+argv[0];
 const pid=execFileSync('systemctl',['show','linketic.service','-p','MainPID','--value'],{encoding:'utf8'}).trim();
 if(!/^[1-9][0-9]*$/.test(pid))throw Error('Native service is not running');
 return execFileSync('sudo',['nsenter','--target',pid,'--mount','--root','--wd','--',...argv],options);
}
