// Minimal RFB/VNC client panel for Womprat.
// Uses the vendored Piclaw remote-display-decoder.wasm for framebuffer rectangles.

const enc = new TextEncoder();
const dec = new TextDecoder();

function u16(b, o) { return (b[o] << 8) | b[o+1]; }
function i16(b, o) { const v = u16(b,o); return v & 0x8000 ? v - 0x10000 : v; }
function u32(b, o) { return (b[o]<<24) | (b[o+1]<<16) | (b[o+2]<<8) | b[o+3]; }
function put16(a,o,v){ a[o]=v>>8; a[o+1]=v; }
function put32(a,o,v){ a[o]=v>>>24; a[o+1]=v>>>16; a[o+2]=v>>>8; a[o+3]=v; }

class Reader {
  constructor() { this.buf = new Uint8Array(0); this.waiters = []; }
  push(data) {
    const merged = new Uint8Array(this.buf.length + data.length);
    merged.set(this.buf); merged.set(data, this.buf.length); this.buf = merged; this.pump();
  }
  read(n) { return new Promise(resolve => { this.waiters.push({n, resolve}); this.pump(); }); }
  pump() {
    while (this.waiters.length && this.buf.length >= this.waiters[0].n) {
      const {n, resolve} = this.waiters.shift();
      const out = this.buf.slice(0,n); this.buf = this.buf.slice(n); resolve(out);
    }
  }
}

class VNCClient {
  constructor(root, target) {
    this.root = root; this.target = target; this.reader = new Reader();
    this.canvas = root.querySelector('canvas'); this.ctx = this.canvas.getContext('2d');
    this.status = root.querySelector('[data-vnc-status]'); this.clipboard = root.querySelector('[data-vnc-clipboard]');
    this.scale = 1; this.buttons = 0;
    this.installInput();
    this.installClipboard();
  }
  setStatus(s) { this.status.textContent = s; }
  async loadDecoder() {
    const mod = await WebAssembly.instantiateStreaming(fetch('/vendor/remote-display-decoder.wasm'), {});
    this.wasm = mod.instance.exports;
  }
  async connect() {
    await this.loadDecoder();
    const qs = new URLSearchParams({ target: this.target });
    const wsURL = `ws://${location.host}/api/vnc/ws?${qs}`;
    this.ws = new WebSocket(wsURL); this.ws.binaryType = 'arraybuffer';
    this.ws.onmessage = e => this.reader.push(new Uint8Array(e.data));
    this.ws.onclose = e => this.setStatus(`Disconnected (${e.code || ''})`);
    await new Promise((res, rej) => { this.ws.onopen = res; this.ws.onerror = rej; });
    this.setStatus(`Connected to ${this.target}; negotiating RFB…`);
    await this.handshake();
    this.requestUpdate(false);
  }
  send(bytes) { if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(bytes); }
  async handshake() {
    const proto = await this.reader.read(12);
    this.send(proto); // mirror server version
    const nSec = (await this.reader.read(1))[0];
    if (!nSec) throw new Error(dec.decode(await this.reader.read(u32(await this.reader.read(4),0))));
    const sec = await this.reader.read(nSec);
    if (!sec.includes(1)) throw new Error('VNC server requires authentication; password auth is not implemented yet.');
    this.send(new Uint8Array([1])); // None
    const result = u32(await this.reader.read(4),0);
    if (result !== 0) throw new Error('VNC security handshake failed');
    this.send(new Uint8Array([1])); // shared flag
    const head = await this.reader.read(24);
    this.width = u16(head,0); this.height = u16(head,2);
    const nameLen = u32(head,20); this.name = dec.decode(await this.reader.read(nameLen));
    this.canvas.width = this.width; this.canvas.height = this.height;
    this.image = this.ctx.createImageData(this.width, this.height);
    this.wasm.initFramebuffer(this.width, this.height);
    this.setStatus(`${this.name || this.target} (${this.width}×${this.height})`);
    this.setEncodings([0,1,2,5,16,-223]);
    this.loop();
  }
  setEncodings(encs) {
    const b = new Uint8Array(4 + encs.length*4); b[0]=2; put16(b,2,encs.length);
    encs.forEach((e,i)=>put32(b,4+i*4,e|0)); this.send(b);
  }
  requestUpdate(incremental=true) {
    const b = new Uint8Array(10); b[0]=3; b[1]=incremental?1:0; put16(b,4,this.width); put16(b,6,this.height); this.send(b);
  }
  async loop() {
    try {
      for (;;) {
        const t = (await this.reader.read(1))[0];
        if (t === 0) await this.framebufferUpdate();
        else if (t === 2) this.setStatus('Bell');
        else if (t === 3) await this.serverCutText();
      }
    } catch (e) { this.setStatus(`VNC error: ${e.message || e}`); }
  }
  async framebufferUpdate() {
    await this.reader.read(1); const n = u16(await this.reader.read(2),0);
    for (let i=0;i<n;i++) {
      const h = await this.reader.read(12); const x=u16(h,0), y=u16(h,2), w=u16(h,4), ht=u16(h,6), type=u32(h,8)|0;
      if (type === 0) { // raw, 32bpp
        const data = await this.reader.read(w*ht*4); this.drawRaw(x,y,w,ht,data);
      } else {
        this.setStatus(`Unsupported VNC encoding ${type}; request raw update`);
      }
    }
    this.ctx.putImageData(this.image,0,0);
    this.requestUpdate(true);
  }
  drawRaw(x,y,w,h,data) {
    for (let row=0; row<h; row++) for (let col=0; col<w; col++) {
      const si=(row*w+col)*4, di=((y+row)*this.width+(x+col))*4;
      // RFB true-color servers commonly send B,G,R,x on little-endian 32bpp.
      this.image.data[di]=data[si+2]; this.image.data[di+1]=data[si+1]; this.image.data[di+2]=data[si]; this.image.data[di+3]=255;
    }
  }
  async serverCutText() { await this.reader.read(3); const len=u32(await this.reader.read(4),0); const text=dec.decode(await this.reader.read(len)); this.clipboard.value=text; }
  clientCutText(text) { const data=enc.encode(text); const b=new Uint8Array(8+data.length); b[0]=6; put32(b,4,data.length); b.set(data,8); this.send(b); }
  installClipboard() { this.root.querySelector('[data-vnc-send-clipboard]').onclick=()=>this.clientCutText(this.clipboard.value||''); }
  installInput() {
    const c = this.canvas; c.tabIndex = 0;
    const pos = e => { const r=c.getBoundingClientRect(); return {x:Math.max(0,Math.min(this.width-1,Math.round((e.clientX-r.left)*this.width/r.width))), y:Math.max(0,Math.min(this.height-1,Math.round((e.clientY-r.top)*this.height/r.height)))}; };
    const ptr = e => { const p=pos(e); const b=new Uint8Array(6); b[0]=5; b[1]=this.buttons; put16(b,2,p.x); put16(b,4,p.y); this.send(b); };
    c.addEventListener('mousemove', ptr); c.addEventListener('mousedown', e=>{this.buttons|=1<<(e.button||0); ptr(e); c.focus();}); c.addEventListener('mouseup', e=>{this.buttons&=~(1<<(e.button||0)); ptr(e);});
    c.addEventListener('keydown', e=>this.key(e,true)); c.addEventListener('keyup', e=>this.key(e,false));
  }
  key(e, down) { const sym=this.keysym(e); if (!sym) return; e.preventDefault(); const b=new Uint8Array(8); b[0]=4; b[1]=down?1:0; put32(b,4,sym); this.send(b); }
  keysym(e) {
    const map={Backspace:0xff08,Tab:0xff09,Enter:0xff0d,Escape:0xff1b,Insert:0xff63,Delete:0xffff,Home:0xff50,End:0xff57,PageUp:0xff55,PageDown:0xff56,ArrowLeft:0xff51,ArrowUp:0xff52,ArrowRight:0xff53,ArrowDown:0xff54};
    if (map[e.key]) return map[e.key]; if (e.key.length===1) return e.key.charCodeAt(0); return 0;
  }
}

export async function startVNC(root, target) {
  const client = new VNCClient(root, target); try { await client.connect(); } catch(e) { client.setStatus(`VNC error: ${e.message || e}`); }
}
