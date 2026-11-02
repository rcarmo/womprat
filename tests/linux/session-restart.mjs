// Exercise the exact shipping AppImage across fresh processes. No test bridge,
// direct networking, user-state copy, or sandbox override is used.
import {spawn} from 'node:child_process';
import {mkdirSync,openSync,closeSync,readFileSync,realpathSync,lstatSync} from 'node:fs';
import {join} from 'node:path';
const [image,closeWindow]=process.argv.slice(2);
const root=process.env.WOMPRAT_RUN_DIR;
if(!image||!closeWindow||!root)throw Error('AppImage, close-window helper and owned run directory required');
const projectRoot=process.env.PROJECT_TMP_ROOT;
if(!projectRoot||!realpathSync(root).startsWith(realpathSync(projectRoot)+'/runs/')||lstatSync(image).isSymbolicLink()||!lstatSync(image).isFile()||!realpathSync(image).startsWith(realpathSync(projectRoot)+'/runs/'))throw Error('AppImage and restart state must be project-run-owned');
const home=join(root,'restart-home');mkdirSync(home,{recursive:true});
const saved=[{type:'browser',title:'Restore first',url:'https://restore-first.invalid/'},{type:'browser',title:'Restore second',url:'https://restore-second.invalid/'}];
let child,exit,metadata;
const assert=(ok,text)=>{if(!ok)throw Error(text);console.log('PASS '+text)};
function native(...args){const result=Bun.spawnSync(['xdotool',...args],{stdout:'pipe',stderr:'pipe'});if(result.exitCode!==0)throw Error('xdotool '+args[0]+': '+result.stderr.toString());return result.stdout.toString().trim()}
async function api(path,body){const r=await fetch(new URL(path,metadata.url),{headers:{'X-Session-Token':metadata.token,'Content-Type':'application/json'},...(body===undefined?{}:{method:'POST',body:JSON.stringify(body)}),signal:AbortSignal.timeout(10000)});if(!r.ok)throw Error(path+': '+r.status);return r.json()}
async function waitFor(fn,label){for(let i=0;i<200;i++){if(await fn())return;await Bun.sleep(100)}throw Error('Timed out: '+label)}
async function start(label){
 const log=join(root,'restart-'+label+'.log'),fd=openSync(log,'w',0o600);
 child=spawn('dbus-run-session',['--',image],{detached:true,stdio:['ignore',fd,fd],env:{...process.env,HOME:home,XDG_CONFIG_HOME:join(home,'.config'),XDG_DATA_HOME:join(home,'.local/share'),APPIMAGE_EXTRACT_AND_RUN:'1'}});closeSync(fd);
 exit=new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',(code,signal)=>resolve({code,signal}))});
 await waitFor(async()=>{if(child.exitCode!==null)throw Error('AppImage exited: '+readFileSync(log,'utf8').slice(-2000));const s=readFileSync(log,'utf8'),url=s.match(/^WOMPRAT_SHELL_URL=(.+)$/m)?.[1],token=s.match(/^WOMPRAT_TOKEN=(.+)$/m)?.[1];if(url&&token){metadata={url,token};return true}return false},'shipping GUI metadata');
 assert(!readFileSync(log,'utf8').includes('WOMPRAT_GUI_TEST_URL'),'restart uses shipping GUI without query bridge');
 // The locked shell deliberately does not emit a post-hydration ready marker.
 await Bun.sleep(1000);
}
async function stop(){
 const window=native('search','--onlyvisible','--name','^Womprat$').split('\n').at(-1);
 const close=Bun.spawnSync([closeWindow,window],{stdout:'pipe',stderr:'pipe'});if(close.exitCode!==0)throw Error('GTK close failed');
 const ended=await Promise.race([exit,Bun.sleep(10000).then(()=>{throw Error('AppImage shutdown timeout')})]);
 assert(ended.code===0,'restart process closes normally');child=null;
}
try{
 await start('seed');
 await waitFor(async()=>!(await api('/api/auth/status')).locked,'initial unlocked shell');
 await api('/api/settings/save-tabs',{tabs:saved});
 await api('/api/settings/appearance',{fontSize:14,terminalFont:'fira-code',theme:'dark',restoreTabs:true,autoConnect:false});
 await stop();
 await start('enabled');
 await waitFor(async()=>(await api('/api/about')).tabCount===2,'startup restoration');
 assert((await api('/api/settings/config')).openTabs.map(t=>t.url).join()===saved.map(t=>t.url).join(),'shipping restart restores saved order without erasing snapshot');
 await api('/api/settings/appearance',{fontSize:14,terminalFont:'fira-code',theme:'dark',restoreTabs:false,autoConnect:false});
 await stop();
 await start('disabled');
 await Bun.sleep(1200);
 assert((await api('/api/about')).tabCount===0,'disabled restore keeps native session empty');
 assert((await api('/api/settings/config')).openTabs.length===2,'disabled restore retains saved Recent tabs');
 await api('/api/settings/appearance',{fontSize:14,terminalFont:'fira-code',theme:'dark',restoreTabs:true,autoConnect:false});
 await api('/api/settings/master-password',{password:'restart-fixture-password'});
 await stop();
 await start('locked');
 assert((await api('/api/auth/status')).locked,'master-locked restart stays locked');
 const protectedResponse=await fetch(new URL('/api/settings/config',metadata.url),{headers:{'X-Session-Token':metadata.token}});
 assert(protectedResponse.status===423,'master-locked restart protects saved tabs');
 const window=native('search','--onlyvisible','--name','^Womprat$').split('\n').at(-1);
 native('windowfocus','--sync',window);
 // The real unlock form focuses its password field, then Tab reaches Unlock.
 native('type','--clearmodifiers','restart-fixture-password');native('key','Tab','Return');
 await waitFor(async()=>!(await api('/api/auth/status')).locked,'native unlock form');
 await waitFor(async()=>(await api('/api/about')).tabCount===2,'post-unlock restoration');
 assert((await api('/api/settings/config')).openTabs.length===2,'unlock restores saved tabs once');
 await stop();
 console.log('PASS exact AppImage restart: enabled, disabled and master-locked sessions');
}finally{
 if(child){try{process.kill(-child.pid,'SIGTERM')}catch{};await Promise.race([exit,Bun.sleep(2000)]);try{process.kill(-child.pid,'SIGKILL')}catch{}}
}
