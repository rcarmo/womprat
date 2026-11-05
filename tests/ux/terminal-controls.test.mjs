import { test, expect } from 'bun:test';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../../cmd/womprat/frontend/index.html', import.meta.url), 'utf8');
function extractFunction(name, nextName) {
  const begin = source.indexOf(`function ${name}(`);
  const end = source.indexOf(`async function ${nextName}(`, begin);
  if (begin < 0 || end < 0) throw new Error(`cannot extract ${name}`);
  return Function(`${source.slice(begin, end)}; return ${name}`)();
}
const handleTerminalCtrlC = extractFunction('handleTerminalCtrlC', 'loadTerminalFont');

test('Ctrl+C copies a terminal selection without sending ETX', async () => {
  const writes = [];
  const inputs = [];
  const term = {
    hasSelection: () => true,
    getSelection: () => 'selected text',
    input: (...args) => inputs.push(args),
  };
  const result = handleTerminalCtrlC(term, { writeText: text => writes.push(text) });
  await Promise.resolve();
  expect(result).toBe(false);
  expect(writes).toEqual(['selected text']);
  expect(inputs).toEqual([]);
});

test('Ctrl+C sends ETX when the terminal has no selection', () => {
  const writes = [];
  const inputs = [];
  const term = {
    hasSelection: () => false,
    getSelection: () => '',
    input: (...args) => inputs.push(args),
  };
  const result = handleTerminalCtrlC(term, { writeText: text => writes.push(text) });
  expect(result).toBe(false);
  expect(writes).toEqual([]);
  expect(inputs).toEqual([['\x03', true]]);
});

test('terminal close removes its window listener and timer', () => {
  const closeBegin=source.indexOf('window.closeTab = function(id, e)');
  const closeEnd=source.indexOf('function clampInt(',closeBegin);
  const closeBody=source.slice(closeBegin,closeEnd);
  expect(closeBody).toContain("window.removeEventListener('resize', terminalSession.resizeHandler)");
  expect(closeBody).toContain('clearTimeout(terminalSession.scrollbarFadeTimer)');
  const openBegin=source.indexOf('const resizeHandler = () =>');
  const openEnd=source.indexOf('// Fade the scrollbar',openBegin);
  const openBody=source.slice(openBegin,openEnd);
  expect(openBody).toContain("window.addEventListener('resize', resizeHandler)");
  expect(openBody).toContain('resizeHandler, scrollbarFadeTimer: null');
});

test('clipboard write rejection is contained', async () => {
  const warnings = [];
  const originalWarn = console.warn;
  console.warn = (...args) => warnings.push(args);
  try {
    const term = { hasSelection: () => true, getSelection: () => 'text', input() {} };
    handleTerminalCtrlC(term, { writeText: () => Promise.reject(new Error('denied')) });
    await Bun.sleep(0);
    expect(warnings.length).toBe(1);
  } finally {
    console.warn = originalWarn;
  }
});

function zoomHotkeys(type, session) {
 const events=new Map(),native=[];
 const state={activeTab:'tab',tabs:[{id:'tab',type}]};
 const begin=source.indexOf('function adjustActiveContentZoom(');
 const end=source.indexOf('function installShellControlHandlers()',begin);
 const keys=source.indexOf('function installBrowserHotkeys(');
 const keysEnd=source.indexOf('let lastURLBarNavigation',keys);
 const window=type==='browser'?{womprat_browserZoom:step=>native.push(step)}:{};
 const helpers=Function('state','terminalSessions','terminalAppearance','window','document',`let shellContentZoom=1;function applyShellContentZoom(){shellContentZoom=Math.max(.5,Math.min(3,shellContentZoom))};${source.slice(begin,end)};${source.slice(keys,keysEnd)};installBrowserHotkeys();return {shellZoom:()=>shellContentZoom}`)(
  state,new Map(session?[['tab',session]]:[]),{fontSize:14},window,{addEventListener:(name,fn)=>events.set(name,fn)});
 return {helpers,native,key:(key,code,shiftKey=false,ctrlKey=true,extra={})=>{
  const event={key,code,shiftKey,ctrlKey,altKey:false,metaKey:false,isComposing:false,preventDefault(){this.prevented=true},stopPropagation(){this.stopped=true},...extra};events.get('keydown')(event);return event;
 }};
}

test('terminal zoom requires Shift, handles layout/keypad aliases and resets configured size',()=>{
 let fits=0;
 const session={fontSize:14,term:{options:{fontSize:14}},fit:{fit:()=>fits++}};
 const h=zoomHotkeys('terminal',session);
 for(const [key,code] of [['-','Minus'],['=','Equal'],['0','Digit0']])expect(h.key(key,code).prevented).not.toBe(true);
 expect(session.fontSize).toBe(14);expect(fits).toBe(0);
 expect(h.key('+','Equal',true).prevented).toBe(true);expect(session.fontSize).toBe(15);
 h.key('_','Minus',true);expect(session.fontSize).toBe(14);
 h.key('+','NumpadAdd',true);expect(session.fontSize).toBe(15);
 h.key(')','Digit0',true);expect(session.fontSize).toBe(14);
 h.key('-','NumpadSubtract',true);expect(session.fontSize).toBe(13);
 h.key('0','Numpad0',true);expect(session.fontSize).toBe(14);
 for(let i=0;i<50;i++)h.key('+','Equal',true);expect(session.fontSize).toBe(30);
 for(let i=0;i<50;i++)h.key('_','Minus',true);expect(session.fontSize).toBe(8);
 expect(h.key('+','Equal',true,true,{altKey:true}).prevented).not.toBe(true);
});

test('browser zoom routes native view from shell focus, including shifted reset',()=>{
 const h=zoomHotkeys('browser');
 h.key('+','Equal',true);h.key('_','Minus',true);h.key(')','Digit0',true);
 h.key('=','Equal');h.key('-','Minus');h.key('0','Digit0');
 h.key('+','NumpadAdd');h.key('-','NumpadSubtract');h.key('0','Numpad0');
 expect(h.native).toEqual([1,-1,0,1,-1,0,1,-1,0]);
 expect(h.helpers.shellZoom()).toBe(1);
 expect(h.key('+','Equal',false,true,{altKey:true}).prevented).not.toBe(true);
 expect(h.key('+','Equal',false,true,{isComposing:true}).prevented).not.toBe(true);
});

test('shell content zoom and reset remain available outside native browser tabs',()=>{
 for(const type of ['settings','vnc','rdp']){
  const h=zoomHotkeys(type);h.key('+','Equal',true);expect(h.helpers.shellZoom()).toBe(1.1);
  h.key('_','Minus',true);expect(h.helpers.shellZoom()).toBe(1);
  h.key('-','NumpadSubtract');expect(h.helpers.shellZoom()).toBe(.9);
  h.key(')','Digit0',true);expect(h.helpers.shellZoom()).toBe(1);
 }
});

test('injected content bridge consumes zoom once and forwards it to native host',()=>{
 const bridge=readFileSync(new URL('../../cmd/womprat/browser_bridge.go',import.meta.url),'utf8').match(/const browserTitleReporterJS = `([\s\S]*?)`/)[1];
 const events=new Map(),messages=[];
 const document={title:'Fixture',head:{},addEventListener:(name,fn)=>events.set(name,fn),querySelectorAll:()=>[]};
 const window={chrome:{webview:{postMessage:raw=>messages.push(JSON.parse(raw))}},addEventListener(){},open(){}};
 Function('window','document','location','history','MutationObserver',bridge)(window,document,{href:'https://example.test/',hostname:'example.test',origin:'https://example.test'},{pushState(){},replaceState(){}},class{observe(){}});
 for(const [key,code] of [['+','Equal'],['_','Minus'],[')','Digit0'],['+','NumpadAdd'],['-','NumpadSubtract'],['0','Numpad0']]){
  const e={key,code,ctrlKey:true,shiftKey:true,preventDefault(){this.prevented=true},stopImmediatePropagation(){this.stopped=true}};
  events.get('keydown')(e);expect(e.prevented&&e.stopped).toBe(true);
 }
 expect(messages.filter(m=>m.wompratKey).map(m=>m.wompratArg)).toEqual(['in','out','reset','in','out','reset']);
});
