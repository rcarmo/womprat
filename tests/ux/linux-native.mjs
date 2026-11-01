// Native GTK development acceptance. Requires the guittest-only authenticated
// query endpoint; never shipped or enabled by a release build.
import {readFileSync} from 'node:fs';
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
 console.log('Fixture direct dialing is guittest-only. Live tailnet/TLS/download parity still requires acceptance.');
}else console.log('Native offline smoke only: successful tailnet navigation/download/TLS parity is not established.');
