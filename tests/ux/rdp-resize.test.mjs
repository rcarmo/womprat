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
