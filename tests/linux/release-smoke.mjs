import { readFileSync } from 'node:fs';
import { createServer, connect } from 'node:net';
import {spawnSync} from 'node:child_process';
const log=readFileSync(process.argv[2],'utf8');
const url=log.match(/^WOMPRAT_SHELL_URL=(.+)$/m)?.[1],token=log.match(/^WOMPRAT_TOKEN=(.+)$/m)?.[1];
if(!url||!token||log.includes('WOMPRAT_GUI_TEST_URL'))throw Error('Not a shipping GUI build');
const shell=await fetch(url);if(!shell.ok||!(await shell.text()).includes('address-suggestions'))throw Error('Embedded shell unavailable');
if((await fetch(new URL('/api/settings/browser/history',url))).status!==403)throw Error('Unauthenticated history accepted');
if((await fetch(url,{headers:{Host:'foreign.example'}})).status!==403)throw Error('Foreign Host accepted');
let reached=false;const canary=createServer(c=>{reached=true;c.destroy()});await new Promise(r=>canary.listen(0,'127.0.0.1',r));
try{
 const port=canary.address().port;
 const result=await new Promise((resolve,reject)=>{
  const c=connect(1080,'127.0.0.1');let stage=0,buffer=Buffer.alloc(0);c.setTimeout(5000,()=>c.destroy(Error('SOCKS timeout')));c.on('error',reject);c.on('connect',()=>c.write(Buffer.from([5,1,0])));
  c.on('data',data=>{buffer=Buffer.concat([buffer,data]);if(stage===0&&buffer.length>=2){if(buffer[1]!==0){reject(Error('SOCKS greeting failed'));c.destroy();return}buffer=buffer.subarray(2);stage=1;c.write(Buffer.from([5,1,0,1,127,0,0,1,port>>8,port&255]))}if(stage===1&&buffer.length>=2){resolve(buffer[1]);c.destroy()}});
 });
 if(result===0||reached)throw Error('Release direct-dial override escaped to host canary');
 // Exercise actual browser UI, not only the SOCKS server. A local destination
 // must still use the content proxy; WebKit localhost bypass would hit canary.
 for(const key of ['ctrl+t','ctrl+l'])if(spawnSync('xdotool',['key','--clearmodifiers',key]).status!==0)throw Error('Native keyboard input failed');
 if(spawnSync('xdotool',['type','--clearmodifiers',`http://127.0.0.1:${port}/canary`]).status!==0)throw Error('Address input failed');
 if(spawnSync('xdotool',['key','Return']).status!==0)throw Error('Address submit failed');
 await Bun.sleep(2000);
 if(reached)throw Error('Native browser bypassed tailnet proxy for loopback canary');
}finally{await new Promise(r=>canary.close(r))}
console.log('PASS exact AppImage: shell/API guards and debug-env override rejected');
