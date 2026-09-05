import { test, expect } from 'bun:test';
import { readFileSync } from 'node:fs';

// Exercise the shipped wrapper without a server or WASM decoder.
const source = readFileSync(new URL('../../cmd/womprat/frontend/rdp.js', import.meta.url), 'utf8');
const start = source.indexOf('resizeToViewport(){');
const end = source.indexOf('installControls(){', start);
const resize = Function('WebSocket', `return ({${source.slice(start,end)}}).resizeToViewport`)({OPEN:1});
function fixture() {
 const sent=[];
 const view={width:900,height:600};
 const wrapper={ viewport:{getBoundingClientRect:()=>view}, canvas:{width:1280,height:720}, fitCanvas(){}, client:{connected:true,socket:{readyState:1},serverCapabilities:{displayControlReady:true},sendResizeRequest:(...args)=>sent.push(args)} };
 return {sent,view,wrapper};
}

test('hidden RDP tab never sends a 1x1 resize',async()=>{
 const {sent,view,wrapper}=fixture();
 view.width=0;view.height=0;
 resize.call(wrapper);
 await Bun.sleep(200);
 expect(sent).toEqual([]);
});
test('hiding tab cancels a pending visible resize',async()=>{
 const {sent,view,wrapper}=fixture();
 resize.call(wrapper);
 view.width=0;view.height=0;
 resize.call(wrapper);
 await Bun.sleep(200);
 expect(sent).toEqual([]);
});
test('visible viewport sends its exact dimensions',async()=>{
 const {sent,wrapper}=fixture();
 resize.call(wrapper);
 await Bun.sleep(200);
 expect(sent).toEqual([[900,600]]);
});

test('disposing RDP stops observer, timer and pointer cache',async()=>{
 const begin=source.indexOf('dispose(){this.disposed=true;');
 const finish=source.indexOf('reconnect(){',begin);
 const dispose=Function(`return ({${source.slice(begin,finish)}}).dispose`)();
 let disconnected=0,removed=0,observerStopped=0;
 const wrapper={disconnect(){disconnected++},pointer:{remove(){removed++}},resizeObserver:{disconnect(){observerStopped++}}};
 dispose.call(wrapper);
 expect(wrapper.disposed).toBe(true);
 expect([disconnected,removed,observerStopped]).toEqual([1,1,1]);
});

test('startup does not construct a viewer after its panel is removed',async()=>{
 const begin=source.indexOf('async function n2(');
 const finish=source.indexOf('function h2(',begin);
 let constructed=0;
 const start=Function('F1','H1','E',`${source.slice(begin,finish)};return n2`)(async()=>true,class {constructor(){constructed++}},()=>{});
 await start({isConnected:false},'rdp://test');
 expect(constructed).toBe(0);
});

test('RDP text bridge errors reach message parser',()=>{
 const begin=source.indexOf('this.socket.onmessage=(z)=>');
 const finish=source.indexOf(',this.socket.onerror=',begin);
 const install=Function(source.slice(begin,finish));
 let received;
 const client={socket:{},handleMessage(data){received=new TextDecoder().decode(data)}};
 install.call(client);
 const message=JSON.stringify({type:'error',message:'authentication failed'});
 client.socket.onmessage({data:message});
 expect(received).toBe(message);
});
