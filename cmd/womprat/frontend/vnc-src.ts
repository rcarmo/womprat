import { loadRemoteDisplayWasmDecoder } from './vnc-piclaw/remote-display-decoder.js';
import { VncRemoteDisplayProtocol } from './vnc-piclaw/remote-display-vnc.js';
import {
  buildVncWheelPointerEvents,
  encodeVncKeyEvent,
  encodeVncPointerEvent,
  mapClientToFramebufferPoint,
  resolveVncKeysymFromKeyboardEvent,
  resolveVncPointerPressMask,
} from './vnc-piclaw/vnc-input.js';

const textEncoder = new TextEncoder();

function clientCutText(text: string): Uint8Array {
  const payload = textEncoder.encode(String(text || ''));
  const out = new Uint8Array(8 + payload.length);
  out[0] = 6;
  out[4] = (payload.length >>> 24) & 0xff;
  out[5] = (payload.length >>> 16) & 0xff;
  out[6] = (payload.length >>> 8) & 0xff;
  out[7] = payload.length & 0xff;
  out.set(payload, 8);
  return out;
}

function wsBase(): string {
  return window.location.protocol === 'https:' ? 'wss:' : 'ws:';
}

function setStatus(root: HTMLElement, text: string): void {
  const el = root.querySelector('[data-vnc-status]');
  if (el) el.textContent = text;
}

function setBusy(root: HTMLElement, busy: boolean): void {
  root.toggleAttribute('data-busy', busy);
}

function query<T extends HTMLElement>(root: HTMLElement, selector: string): T {
  const el = root.querySelector(selector) as T | null;
  if (!el) throw new Error(`Missing VNC element: ${selector}`);
  return el;
}

class WompratVncViewer {
  root: HTMLElement;
  target: string;
  password: string | null;
  protocol: any;
  ws: WebSocket | null = null;
  canvas: HTMLCanvasElement;
  viewport: HTMLElement;
  ctx: CanvasRenderingContext2D;
  framebuffer: ImageData | null = null;
  buttons = 0;
  activeKeys = new Set<number>();

  constructor(root: HTMLElement, target: string, password: string | null = null) {
    this.root = root;
    this.target = target;
    this.password = password;
    this.canvas = query<HTMLCanvasElement>(root, 'canvas');
    this.viewport = query<HTMLElement>(root, '[data-vnc-viewport]');
    const ctx = this.canvas.getContext('2d', { alpha: false });
    if (!ctx) throw new Error('Canvas 2D context is unavailable.');
    this.ctx = ctx;
    this.installInput();
    this.installClipboard();
    this.installResize();
  }

  async init(): Promise<void> {
    setStatus(this.root, 'Loading VNC decoder…');
    const pipeline = await loadRemoteDisplayWasmDecoder();
    this.protocol = new VncRemoteDisplayProtocol({ shared: true, password: this.password, pipeline });
  }

  connect(): void {
    const url = new URL(`${wsBase()}//${window.location.host}/api/vnc/ws`);
    url.searchParams.set('target', this.target);
    setBusy(this.root, true);
    setStatus(this.root, `Connecting to ${this.target}…`);
    this.ws = new WebSocket(url.toString());
    this.ws.binaryType = 'arraybuffer';
    this.ws.onopen = () => setStatus(this.root, `Negotiating VNC for ${this.target}…`);
    this.ws.onerror = () => setStatus(this.root, 'VNC connection error.');
    this.ws.onclose = (event) => {
      setBusy(this.root, false);
      setStatus(this.root, event.reason ? `Disconnected: ${event.reason}` : 'Disconnected.');
    };
    this.ws.onmessage = (event) => this.receive(new Uint8Array(event.data));
  }

  send(bytes: Uint8Array): void {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(bytes);
  }

  receive(bytes?: Uint8Array): void {
    try {
      const result = this.protocol.receive(bytes);
      for (const out of result.outgoing || []) this.send(out);
      for (const event of result.events || []) this.handleEvent(event);
    } catch (error: any) {
      setBusy(this.root, false);
      setStatus(this.root, `VNC error: ${error?.message || error}`);
      try { this.ws?.close(); } catch { /* ignore */ }
    }
  }

  handleEvent(event: any): void {
    switch (event.type) {
      case 'protocol-version':
        setStatus(this.root, `RFB ${event.server} → ${event.client}`);
        break;
      case 'security-selected':
        setStatus(this.root, `Security: ${event.label || event.securityType}`);
        break;
      case 'security-result':
        setStatus(this.root, 'Authenticated. Initializing display…');
        break;
      case 'display-init':
        this.resizeFramebuffer(event.width, event.height);
        setBusy(this.root, false);
        setStatus(this.root, `${event.name || this.target} · ${event.width}×${event.height}`);
        break;
      case 'framebuffer-update':
        this.drawFramebufferUpdate(event);
        break;
      case 'clipboard': {
        const input = this.root.querySelector('[data-vnc-clipboard]') as HTMLInputElement | null;
        if (input) input.value = event.text || '';
        setStatus(this.root, 'Remote clipboard updated.');
        break;
      }
      case 'bell':
        setStatus(this.root, 'Remote bell.');
        break;
    }
  }

  resizeFramebuffer(width: number, height: number): void {
    const w = Math.max(1, Math.floor(Number(width || 0)));
    const h = Math.max(1, Math.floor(Number(height || 0)));
    this.canvas.width = w;
    this.canvas.height = h;
    this.framebuffer = this.ctx.createImageData(w, h);
    this.fitCanvas();
  }

  fitCanvas(): void {
    const w = Math.max(1, this.canvas.width || 1);
    const h = Math.max(1, this.canvas.height || 1);
    const bounds = this.viewport.getBoundingClientRect();
    const scale = Math.min(bounds.width / w, bounds.height / h, 1) || 1;
    this.canvas.style.width = `${Math.max(1, Math.floor(w * scale))}px`;
    this.canvas.style.height = `${Math.max(1, Math.floor(h * scale))}px`;
  }

  drawFramebufferUpdate(event: any): void {
    if (event.framebuffer && event.width && event.height) {
      this.resizeFramebuffer(event.width, event.height);
      this.ctx.putImageData(new ImageData(new Uint8ClampedArray(event.framebuffer), event.width, event.height), 0, 0);
      return;
    }
    if (!this.framebuffer) this.resizeFramebuffer(event.width || this.canvas.width, event.height || this.canvas.height);
    if (!this.framebuffer) return;
    for (const rect of event.rects || []) {
      if (rect.kind === 'resize') {
        this.resizeFramebuffer(rect.width, rect.height);
      } else if (rect.kind === 'rgba' && rect.rgba) {
        this.blitRgba(rect.x, rect.y, rect.width, rect.height, rect.rgba);
      } else if (rect.kind === 'copy') {
        const copy = this.ctx.getImageData(rect.srcX, rect.srcY, rect.width, rect.height);
        this.ctx.putImageData(copy, rect.x, rect.y);
        this.framebuffer = this.ctx.getImageData(0, 0, this.canvas.width, this.canvas.height);
      }
    }
    this.ctx.putImageData(this.framebuffer, 0, 0);
  }

  blitRgba(x: number, y: number, width: number, height: number, rgba: Uint8ClampedArray): void {
    if (!this.framebuffer) return;
    const fb = this.framebuffer;
    for (let row = 0; row < height; row += 1) {
      const src = row * width * 4;
      const dst = ((y + row) * fb.width + x) * 4;
      fb.data.set(rgba.subarray(src, src + width * 4), dst);
    }
  }

  point(event: PointerEvent | WheelEvent): { x: number; y: number } {
    return mapClientToFramebufferPoint(event.clientX, event.clientY, this.canvas.getBoundingClientRect(), this.canvas.width, this.canvas.height);
  }

  installResize(): void {
    const ro = new ResizeObserver(() => this.fitCanvas());
    ro.observe(this.viewport);
  }

  installClipboard(): void {
    query<HTMLButtonElement>(this.root, '[data-vnc-send-clipboard]').addEventListener('click', () => {
      const input = this.root.querySelector('[data-vnc-clipboard]') as HTMLInputElement | null;
      this.send(clientCutText(input?.value || ''));
      setStatus(this.root, 'Clipboard sent to remote.');
    });
  }

  installInput(): void {
    this.canvas.tabIndex = 0;
    this.canvas.addEventListener('contextmenu', (event) => event.preventDefault());
    this.canvas.addEventListener('pointerdown', (event) => {
      this.canvas.focus();
      try { this.canvas.setPointerCapture(event.pointerId); } catch { /* ignore */ }
      this.buttons |= resolveVncPointerPressMask(event);
      const p = this.point(event);
      this.send(encodeVncPointerEvent(this.buttons, p.x, p.y));
      event.preventDefault();
    });
    const release = (event: PointerEvent) => {
      this.buttons &= ~resolveVncPointerPressMask(event);
      const p = this.point(event);
      this.send(encodeVncPointerEvent(this.buttons, p.x, p.y));
      event.preventDefault();
    };
    this.canvas.addEventListener('pointerup', release);
    this.canvas.addEventListener('pointercancel', release);
    this.canvas.addEventListener('pointermove', (event) => {
      const p = this.point(event);
      this.send(encodeVncPointerEvent(this.buttons, p.x, p.y));
    });
    this.canvas.addEventListener('wheel', (event) => {
      const p = this.point(event);
      for (const msg of buildVncWheelPointerEvents(event.deltaY, p.x, p.y, this.buttons)) this.send(msg);
      event.preventDefault();
    }, { passive: false });
    this.canvas.addEventListener('keydown', (event) => {
      const keysym = resolveVncKeysymFromKeyboardEvent(event);
      if (!keysym) return;
      if (!event.repeat || !this.activeKeys.has(keysym)) {
        this.activeKeys.add(keysym);
        this.send(encodeVncKeyEvent(true, keysym));
      }
      event.preventDefault();
    });
    this.canvas.addEventListener('keyup', (event) => {
      const keysym = resolveVncKeysymFromKeyboardEvent(event);
      if (!keysym) return;
      this.activeKeys.delete(keysym);
      this.send(encodeVncKeyEvent(false, keysym));
      event.preventDefault();
    });
  }
}

export async function startVNC(root: HTMLElement, target: string): Promise<void> {
  const password = root.getAttribute('data-vnc-password') || null;
  const viewer = new WompratVncViewer(root, target, password);
  await viewer.init();
  viewer.connect();
}
