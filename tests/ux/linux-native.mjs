// Native GTK development acceptance. Requires the guittest-only authenticated
// query endpoint; never shipped or enabled by a release build.
import {readFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
const log=readFileSync(process.argv[2],'utf8');
const url=log.match(/^WOMPRAT_GUI_TEST_URL=(.+)$/m)?.[1];
const token=log.match(/^WOMPRAT_TOKEN=(.+)$/m)?.[1];
if(!url||!token)throw Error('native GUI acceptance metadata missing');
async function query(script,tab=''){
 const response=await fetch(new URL('query',url),{method:'POST',headers:{'X-Session-Token':token,'Content-Type':'application/json'},body:JSON.stringify({script,tab}),signal:AbortSignal.timeout(10000)});
 if(!response.ok)throw Error('native query failed '+response.status);return (await response.json()).result;
}
const assert=(ok,message)=>{if(!ok)throw Error(message);console.log('PASS '+message)};
async function wait(script){for(let i=0;i<60;i++){if(await query(script)==='true')return;await Bun.sleep(100)}throw Error('native wait timed out: '+script)}
await wait('String(location.href.startsWith("http://127.0.0.1:") && typeof window.womprat_newBrowser === "function" && typeof window.showBrowserTab === "function")');
const origin=await query('location.href');assert(origin.startsWith('http://127.0.0.1:'),'shell remains on local origin');
const baseline=Number(await query('String(document.querySelectorAll("#tab-list .tab").length)'));
await query('womprat_newBrowser("https://example.com/one");"requested"');
await wait(`String(document.querySelectorAll("#tab-list .tab").length>${baseline})`);
let tabs=JSON.parse(await query('JSON.stringify(Array.from(document.querySelectorAll("#tab-list .tab")).map(t=>t.dataset.tabId))'));
const first=tabs.at(-1);
await query('womprat_newBrowser("https://example.com/two");"requested"');
await wait(`String(document.querySelectorAll("#tab-list .tab").length>${baseline+1})`);
tabs=JSON.parse(await query('JSON.stringify(Array.from(document.querySelectorAll("#tab-list .tab")).map(t=>t.dataset.tabId))'));
const second=tabs.at(-1);assert(first!==second,'independent native browser IDs');
assert(await query('location.href')===origin,'content navigation does not replace shell');
await query('window.__tabNodes=Array.from(document.querySelectorAll("#tab-list .tab"));"saved"');
await query(`womprat_switchTab(${JSON.stringify(first)});"switched"`);
await wait(`String(document.querySelector('#tab-list .tab.active')?.dataset.tabId===${JSON.stringify(first)})`);
assert(await query('String(window.__tabNodes.every((node,i)=>node===document.querySelectorAll("#tab-list .tab")[i]))')==='true','switch preserves shell tab nodes');
await query(`window.closeTab(${JSON.stringify(first)});"closed"`);
await wait(`String(!Array.from(document.querySelectorAll('#tab-list .tab')).some(t=>t.dataset.tabId===${JSON.stringify(first)}))`);
assert(await query('location.href')===origin,'close keeps shell alive');
if(process.env.WOMPRAT_GUI_FIXTURES==='1'){
 const page=new URL('page?name=first',url).href;
 const count=Number(await query('String(document.querySelectorAll("#tab-list .tab").length)'));
 await query(`womprat_newBrowser(${JSON.stringify(page)});"open"`);
 await wait(`String(document.querySelectorAll("#tab-list .tab").length>${count})`);
 const id=JSON.parse(await query('JSON.stringify(Array.from(document.querySelectorAll("#tab-list .tab")).map(t=>t.dataset.tabId))')).at(-1);
 for(let i=0;i<60;i++){if(await query('document.title',id)==='Native first')break;await Bun.sleep(100)}
 assert(await query('document.title',id)==='Native first','separate content view loads fixture through proxy');
 await wait(`String(document.querySelector('[data-tab-id="${id}"] img')?.src.startsWith('data:image/png;base64,'))`);
 assert(await query(`String(document.querySelector('[data-tab-id="${id}"] img').src.length<=2048)`)==='true','native favicon reaches shell as bounded inline pixels');
 const native=(...args)=>{const r=spawnSync('xdotool',args,{encoding:'utf8'});if(r.status!==0)throw Error('Native input: '+r.stderr);return r.stdout.trim()};
 const window=native('search','--onlyvisible','--name','^Womprat$').split('\n').at(-1);
 native('windowfocus','--sync',window);
 await query('document.body.tabIndex=0;document.body.focus();"focused"',id);
 const initialWidth=Number(await query('String(innerWidth)',id));
 native('key','--clearmodifiers','ctrl+plus');
 for(let i=0;i<30&&Number(await query('String(innerWidth)',id))>=initialWidth;i++)await Bun.sleep(100);
 assert(Number(await query('String(innerWidth)',id))<initialWidth,'native content zoom changes viewport');
 native('key','--clearmodifiers','ctrl+0');
 for(let i=0;i<30&&Math.abs(Number(await query('String(innerWidth)',id))-initialWidth)>1;i++)await Bun.sleep(100);
 assert(Math.abs(Number(await query('String(innerWidth)',id))-initialWidth)<=1,'native zoom reset restores viewport');
 native('windowsize',window,'1000','700');
 for(let i=0;i<30&&Number(await query('String(innerWidth)',id))>=initialWidth;i++)await Bun.sleep(100);
 assert(Number(await query('String(innerWidth)',id))<initialWidth,'native content resizes with GTK host');
 native('key','--clearmodifiers','ctrl+l');
 await wait('String(document.activeElement?.id==="url-input")');
 assert(await query('String(document.activeElement?.id==="url-input")')==='true','native Ctrl+L transfers focus to shell address bar');
 native('key','--clearmodifiers','ctrl+shift+minus');
 for(let i=0;i<30&&Number(await query('String(innerWidth)',id))<=1000;i++)await Bun.sleep(100);
 assert(Number(await query('String(innerWidth)',id))>1000,'shifted zoom-out from address bar changes native browser viewport');
 native('key','--clearmodifiers','ctrl+shift+0');
 for(let i=0;i<30&&Math.abs(Number(await query('String(innerWidth)',id))-1000)>1;i++)await Bun.sleep(100);
 assert(Math.abs(Number(await query('String(innerWidth)',id))-1000)<=1,'shifted reset from address bar restores native browser zoom');
 await query('document.body.focus();"focused"',id);
 native('key','--clearmodifiers','ctrl+shift+minus');
 for(let i=0;i<30&&Number(await query('String(innerWidth)',id))<=1000;i++)await Bun.sleep(100);
 assert(Number(await query('String(innerWidth)',id))>1000,'shifted zoom-out works with content focus');
 native('key','--clearmodifiers','ctrl+shift+0');
 for(let i=0;i<30&&Math.abs(Number(await query('String(innerWidth)',id))-1000)>1;i++)await Bun.sleep(100);
 assert(Math.abs(Number(await query('String(innerWidth)',id))-1000)<=1,'shifted reset works with content focus');
 await query('document.getElementById("next").click();"click"',id);
 for(let i=0;i<60;i++){if(await query('document.title',id)==='Native next')break;await Bun.sleep(100)}
 assert(await query('document.title',id)==='Native next','in-page link navigation stays in content tab');
 await query('womprat_browserBack();"back"');
 for(let i=0;i<60;i++){if(await query('document.title',id)==='Native first')break;await Bun.sleep(100)}
 assert(await query('document.title',id)==='Native first','native back returns to prior document');
 await query('womprat_browserForward();"forward"');
 for(let i=0;i<60;i++){if(await query('document.title',id)==='Native next')break;await Bun.sleep(100)}
 assert(await query('document.title',id)==='Native next','native forward restores subsequent document');
 await query('window.__reloadMarker=true;"marker"',id);
 await query('womprat_browserReload();"reload"');
 let reloaded=false;for(let i=0;i<60;i++){if(await query('String(window.__reloadMarker===undefined)',id)==='true'){reloaded=true;break}await Bun.sleep(100)}
 assert(reloaded,'native reload replaces document state');
 assert(await query('location.href')===origin,'successful content keeps authenticated shell');
 const shellCSP=await fetch(origin);assert(!shellCSP.headers.get('content-security-policy').includes('img-src \'self\' data: blob: http:'),'shell resources cannot bypass content proxy');
 assert(await query('typeof womprat_getTabs',id)==='undefined','content cannot invoke privileged shell bridge');
 const beforeForbidden=await query('location.href',id);
 await query('location.href="file:///etc/passwd";"blocked"',id);await Bun.sleep(200);
 assert(await query('location.href',id)===beforeForbidden,'native policy blocks file navigation');
 const prePopup=Number(await query('String(document.querySelectorAll("#tab-list .tab").length)'));
 await query('document.getElementById("popup").click();"popup"',id);
 await wait(`String(document.querySelectorAll("#tab-list .tab").length>${prePopup})`);
 assert(await query('location.href')===origin,'native popup creates a content tab without replacing shell');
 await query(`womprat_switchTab(${JSON.stringify(id)});"switch"`);
 await wait(`String(document.querySelector('#tab-list .tab.active')?.dataset.tabId===${JSON.stringify(id)})`);
 const order=JSON.parse(await query('JSON.stringify(Array.from(document.querySelectorAll("#tab-list .tab")).map(t=>t.dataset.tabId))'));
 await query(`document.querySelector('[data-tab-id="${id}"]').dispatchEvent(new Event('click'));"focus"`);
 // Native reorder takes the same stable insert-before IDs used by the shell.
 await query(`womprat_reorderTab(${JSON.stringify(id)},${JSON.stringify(order[0])});"reorder"`);
 const response=await query('womprat_getTabs().then(value=>window.__nativeTabs=JSON.parse(value));"request"');
 await wait(`String(window.__nativeTabs?.tabs?.[0]?.id===${JSON.stringify(id)})`);
 assert(response==='request','native reorder updates authoritative stable-ID order');
 await query(`womprat_switchTab(${JSON.stringify(second)});"switch"`);
 await wait(`String(document.querySelector('#tab-list .tab.active')?.dataset.tabId===${JSON.stringify(second)})`);
 await query('document.title="Inactive native title";"title"',id);
 await wait(`String(document.querySelector('[data-tab-id="${id}"] .tab-title')?.textContent==='Inactive native title')`);
 assert(await query(`String(document.querySelector('#tab-list .tab.active')?.dataset.tabId===${JSON.stringify(second)})`)==='true','inactive title updates do not activate content tab');
 await query(`womprat_switchTab(${JSON.stringify(id)});"switch"`);
 await wait(`String(document.querySelector('#tab-list .tab.active')?.dataset.tabId===${JSON.stringify(id)})`);
 await query('document.getElementById("download").click();"download"',id);
 let completed=false;for(let i=0;i<80;i++){const r=await fetch(new URL('/api/download/status',origin),{headers:{'X-Session-Token':token}});const status=await r.json();if(status.status==='complete'){completed=true;break}if(status.status==='error')throw Error('managed download: '+status.error);await Bun.sleep(100)}
 assert(completed,'managed download receives native HttpOnly cookie');
 const status=await (await fetch(new URL('/api/download/status',origin),{headers:{'X-Session-Token':token}})).json();
 assert(readFileSync(status.path,'utf8')==='native managed download fixture','download content is complete');
 const tls=log.match(/^WOMPRAT_GUI_TLS_URL=(.+)$/m)?.[1];if(!tls)throw Error('TLS fixture missing');
 const beforeTLS=Number(await query('String(document.querySelectorAll("#tab-list .tab").length)'));
 await query(`womprat_newBrowser(${JSON.stringify(tls+'/page?name=tls')});"tls"`);
 await wait(`String(document.querySelectorAll("#tab-list .tab").length>${beforeTLS})`);
 await wait('String(!document.getElementById("nav-status-proceed").hidden)');
 const tlsID=JSON.parse(await query('JSON.stringify(Array.from(document.querySelectorAll("#tab-list .tab")).map(t=>t.dataset.tabId))')).at(-1);
 await query('document.getElementById("nav-status-proceed").click();"consent"');
 let loaded=false;for(let i=0;i<60;i++){if(await query('document.title',tlsID)==='Native tls'){loaded=true;break}await Bun.sleep(100)}
 assert(loaded,'native certificate error requires explicit consent before load');
 const browser=await fetch(new URL('/api/settings/browser',origin),{headers:{'X-Session-Token':token}});
 assert(browser.ok&&(await browser.json()).cookies.some(c=>c.domain==='127.0.0.1'),'native HttpOnly cookie appears in settings');
 const clear=await fetch(new URL('/api/settings/browser/clear-cookies',origin),{method:'POST',headers:{'X-Session-Token':token,'Content-Type':'application/json'},body:JSON.stringify({domain:'127.0.0.1'})});
 assert(clear.ok,'native cookie-domain deletion completes');
 const after=await fetch(new URL('/api/settings/browser',origin),{headers:{'X-Session-Token':token}});
 assert(!(await after.json()).cookies.some(c=>c.domain==='127.0.0.1'),'native domain cookies removed');
 // A saved browser tab is registered before selection; native switch lazily
 // recreates its content view instead of treating it as a new shell document.
 const restored='browser-native-restored';
 await query(`womprat_registerLocalTab(${JSON.stringify(JSON.stringify({id:restored,type:'browser',title:'Restored fixture',url:page}))});"register"`);
 await query(`womprat_getTabs().then(value=>{const t=JSON.parse(value).tabs.find(t=>t.id===${JSON.stringify(restored)});window.showBrowserTab(t.id,t.url)});"restore"`);
 let restoredPage=false;for(let i=0;i<60;i++){if(await query('document.title',restored)==='Native first'){restoredPage=true;break}await Bun.sleep(100)}
 assert(restoredPage,'registered saved tab lazily restores native content');
 await query('const link=document.createElement("a");link.href="settings:";document.body.append(link);link.click();"settings"',restored);
 await wait('String(document.querySelector("#tab-list .tab.active")?.dataset.tabId==="settings")');
 assert(await query('location.href')===origin,'special settings URL dispatches to shell without navigating content');
 for(const scheme of ['vnc','rdp']){
  await query(`womprat_switchTab(${JSON.stringify(restored)});"switch"`);
  await wait(`String(document.querySelector('#tab-list .tab.active')?.dataset.tabId===${JSON.stringify(restored)})`);
  await query(`(()=>{const a=document.createElement('a');a.href='${scheme}://127.0.0.1:1';document.body.append(a);a.click();return "remote"})()`,restored);
  await wait(`String(!!document.querySelector('.${scheme}-panel'))`);
  assert(await query(`String(!!document.querySelector('.${scheme}-panel'))`)==='true',`native ${scheme} link creates shell remote-display panel`);
 }
 await query(`womprat_switchTab(${JSON.stringify(restored)});"switch"`);
 await wait(`String(document.querySelector('#tab-list .tab.active')?.dataset.tabId===${JSON.stringify(restored)})`);
 await query('const open=Terminal.prototype.open;Terminal.prototype.open=function(el){el.__zoomTestTerminal=this;return open.call(this,el)};"observe terminal"');
 await query(`(()=>{const a=document.createElement('a');a.href='ssh://test@127.0.0.1:1';document.body.append(a);a.click();return "ssh"})()`,restored);
 await wait('String(!!document.querySelector(".term-container"))');
 assert(await query('String(!!document.querySelector(".term-container"))')==='true','native ssh link creates shell terminal panel');
 await wait('String(!!document.querySelector(".term-panel.active .term-container")?.__zoomTestTerminal)');
 const fontScript='String(document.querySelector(".term-panel.active .term-container").__zoomTestTerminal.options.fontSize)';
 const fontSize=parseFloat(await query(fontScript));
 await query('document.querySelector(".term-panel.active .xterm-helper-textarea").focus();"focus"');
 native('key','--clearmodifiers','ctrl+shift+plus');
 for(let i=0;i<30&&parseFloat(await query(fontScript))<=fontSize;i++)await Bun.sleep(100);
 assert(parseFloat(await query(fontScript))>fontSize,'terminal Ctrl+Shift+plus increases font size: '+await query('JSON.stringify({active:document.querySelector("#tab-list .tab.active")?.dataset.tabId,focus:document.activeElement?.className,size:'+fontScript+'})'));
 native('key','--clearmodifiers','ctrl+shift+minus');
 for(let i=0;i<30&&parseFloat(await query(fontScript))!==fontSize;i++)await Bun.sleep(100);
 assert(parseFloat(await query(fontScript))===fontSize,'terminal Ctrl+Shift+minus decreases font size');
 native('key','--clearmodifiers','ctrl+shift+plus');native('key','--clearmodifiers','ctrl+shift+0');
 for(let i=0;i<30&&parseFloat(await query(fontScript))!==fontSize;i++)await Bun.sleep(100);
 assert(parseFloat(await query(fontScript))===fontSize,'terminal Ctrl+Shift+0 resets configured font size');
 await query(`womprat_switchTab(${JSON.stringify(restored)});"switch"`);
 await wait(`String(document.querySelector('#tab-list .tab.active')?.dataset.tabId===${JSON.stringify(restored)})`);
 await query('document.title="Closing fixture";location.href="'+new URL('page?name=closing',url).href+'";"navigating"',restored);
 await query(`window.closeTab(${JSON.stringify(restored)});"close"`);
 await wait(`String(!document.querySelector('[data-tab-id="${restored}"]'))`);
 assert(await query('location.href')===origin,'close during navigation leaves shell and other tabs alive');
 console.log('Fixture direct dialing is guittest-only. Live tailnet/TLS/download parity still requires acceptance.');
}else console.log('Native offline smoke only: successful tailnet navigation/download/TLS parity is not established.');
