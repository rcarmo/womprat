// Focused keyboard integration using the real shell and xterm in Chromium.
// Native browser viewport zoom is verified separately in Fedora acceptance.
import {chromium} from 'playwright';
import {spawn} from 'node:child_process';
import {mkdtempSync,rmSync,readFileSync} from 'node:fs';
import {join} from 'node:path';
import {buildDir,runDir,profilePage} from './profile.mjs';
const home=mkdtempSync(join(runDir,'zoom-home-'));
const child=spawn(process.env.WOMPRAT_BIN||join(buildDir,'dist/womprat-linux-debug'),[],{env:{...process.env,HOME:home,XDG_CONFIG_HOME:join(home,'.config'),WOMPRAT_HEADLESS:'1',WOMPRAT_DIRECT:'1'},stdio:['ignore','pipe','pipe']});
let log='',browser,stopProfile=async()=>{};child.stdout.on('data',d=>log+=d);child.stderr.on('data',d=>log+=d);
const assert=(ok,text)=>{if(!ok)throw Error(text);console.log('PASS '+text)};
try{
 for(let i=0;i<100&&!log.includes('WOMPRAT_SHELL_URL=');i++)await Bun.sleep(100);
 const url=log.match(/^WOMPRAT_SHELL_URL=(.+)$/m)?.[1];if(!url)throw Error('Missing shell URL');
 browser=await chromium.launch({headless:true});const page=await browser.newPage();
 stopProfile=await profilePage(page,'zoom-renderer');
 const status=page.waitForResponse(r=>r.url().endsWith('/api/auth/status'));
 await page.goto(url);await status;await page.waitForFunction(()=>typeof window.openTerminal==='function');
 await page.waitForFunction(()=>!document.getElementById('setup').classList.contains('hidden'));
 await page.evaluate(()=>document.getElementById('setup').classList.add('hidden'));
 await page.evaluate(()=>{
  const open=Terminal.prototype.open;Terminal.prototype.open=function(el){el.__zoomTestTerminal=this;return open.call(this,el)};
  window.openTerminal('127.0.0.1','Zoom','test',1);
 });
 await page.waitForFunction(()=>!!document.querySelector('.term-panel.active .term-container')?.__zoomTestTerminal);
 const terminal=page.locator('.term-panel.active .term-container');
 const size=()=>terminal.evaluate(el=>el.__zoomTestTerminal.options.fontSize);
 const initial=await size();await page.locator('.term-panel.active .xterm-helper-textarea').focus();
 await page.keyboard.press('Control+Shift+Equal');assert(await size()===initial+1,'terminal Ctrl+Shift+plus zooms in');
 await page.keyboard.press('Control+Shift+Minus');assert(await size()===initial,'terminal Ctrl+Shift+minus zooms out');
 await page.keyboard.press('Control+Shift+Equal');await page.keyboard.press('Control+Shift+Digit0');assert(await size()===initial,'terminal shifted reset restores configured font');
 await page.keyboard.press('Control+Minus');assert(await size()===initial,'terminal plain Ctrl remains reserved');
 await page.evaluate(()=>window.openSettings());
 const iframe=page.frameLocator('#panel-settings iframe');await iframe.locator('body').click({position:{x:10,y:10}});
 const zoom=()=>page.evaluate(()=>parseFloat(getComputedStyle(document.getElementById('panel-settings')).zoom));
 await page.keyboard.press('Control+Shift+Equal');assert(await zoom()>1,'settings iframe zooms from focused document');
 await page.keyboard.press('Control+Shift+Digit0');assert(await zoom()===1,'settings iframe zoom resets');
 // The headless shell has no native view; record the authenticated shell's
 // binding calls. Actual view size is covered by the native fixture workload.
 await page.evaluate(()=>{
  window.__zoomCalls=[];window.womprat_browserZoom=step=>window.__zoomCalls.push(step);
  window.showBrowserTab('zoom-browser','https://zoom.invalid/',{skipNative:true});
 });
 await page.locator('#url-input').focus();
 await page.keyboard.press('Control+Shift+Equal');await page.keyboard.press('Control+Shift+Minus');await page.keyboard.press('Control+Shift+Digit0');
 assert(JSON.stringify(await page.evaluate(()=>window.__zoomCalls))==='[1,-1,0]','address focus routes zoom to native browser binding');
 // Run the actual injected page bridge in a separate document, then deliver
 // its messages to the shell as the native event callback does.
 const bridge=readFileSync(new URL('../../cmd/womprat/browser_bridge.go',import.meta.url),'utf8').match(/const browserTitleReporterJS = `([\s\S]*?)`/)[1];
 const content=await browser.newPage();const messages=[];
 await content.exposeFunction('nativeMessage',async raw=>{const m=JSON.parse(raw);if(m.wompratKey){messages.push(m.wompratArg);await page.evaluate(m=>window.wompratBrowserHotkey(m.wompratKey,m.wompratArg),m)}});
 await content.setContent('<title>Zoom bridge</title><input id="focus">');
 await content.evaluate(()=>window.chrome={webview:{postMessage:raw=>window.nativeMessage(raw)}});
 await content.evaluate(bridge);await content.locator('#focus').focus();
 for(const chord of ['Control+Shift+Equal','Control+Shift+Minus','Control+Shift+Digit0'])await content.keyboard.press(chord);
 for(let i=0;i<50&&messages.length<3;i++)await Bun.sleep(20);
 assert(JSON.stringify(messages)==='["in","out","reset"]','focused browser content forwards zoom-in/out/reset once');
 console.log('PASS zoom keyboard integration');
}finally{
 await stopProfile();await browser?.close();
 child.kill('SIGTERM');await Promise.race([new Promise(r=>child.once('exit',r)),Bun.sleep(2000)]);if(child.exitCode===null)child.kill('SIGKILL');
 rmSync(home,{recursive:true,force:true});
}
