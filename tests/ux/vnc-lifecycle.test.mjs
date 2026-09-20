import { test, expect } from 'bun:test';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../../cmd/womprat/frontend/vnc.js', import.meta.url), 'utf8');
const classStart = source.indexOf('class WompratVncViewer');
const classEnd = source.indexOf('async function startVNC', classStart);
const classSource = source.slice(classStart, classEnd);
function method(name, next) {
  const begin = classSource.indexOf(`  ${name}`);
  const end = classSource.indexOf(`  ${next}`, begin);
  if (begin < 0 || end < 0) throw new Error(`cannot extract ${name}`);
  return Function('WebSocket','window','setBusy','setStatus','wsBase',`return ({${classSource.slice(begin,end)}})[${JSON.stringify(name.split('(')[0].replace('async ',''))}]`)(MockSocket, {location:{host:'127.0.0.1'},__SESSION_TOKEN:'token'},()=>{},()=>{},()=> 'ws:');
}
const sockets=[];
class MockSocket {
  static OPEN=1;
  constructor(url){this.url=url;this.readyState=1;sockets.push(this)}
  close(){}
}
const connect=method('connect(generation = this.connectionGeneration) {','send(bytes) {');

test('stale VNC socket callbacks cannot mutate the replacement session',()=>{
  sockets.length=0;
  let status=0,controls=0;
  const viewer={disposed:false,connectionGeneration:1,target:'vnc://host:5900',root:{dataset:{},hasAttribute:()=>false},setSessionControlsEnabled(){controls++},receive(){status++}};
  connect.call(viewer,1);
  const old=sockets[0];
  viewer.connectionGeneration=2;
  connect.call(viewer,2);
  old.onclose({reason:'old closed'});
  old.onmessage({data:new ArrayBuffer(0)});
  expect(controls).toBe(0);
  expect(status).toBe(0);
});

test('VNC lifecycle source invalidates async init on dispose',()=>{
  expect(classSource).toContain('if (this.disposed || generation !== this.connectionGeneration) return false;');
  expect(classSource).toContain('this.connectionGeneration++;');
  expect(source).toContain('if (await viewer.init(generation)) viewer.connect(generation);');
});
