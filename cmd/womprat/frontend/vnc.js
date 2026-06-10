// node_modules/@assemblyscript/loader/index.js
var ID_OFFSET = -8;
var SIZE_OFFSET = -4;
var ARRAYBUFFER_ID = 1;
var STRING_ID = 2;
var ARRAYBUFFERVIEW = 1 << 0;
var ARRAY = 1 << 1;
var STATICARRAY = 1 << 2;
var VAL_ALIGN_OFFSET = 6;
var VAL_SIGNED = 1 << 11;
var VAL_FLOAT = 1 << 12;
var VAL_MANAGED = 1 << 14;
var ARRAYBUFFERVIEW_BUFFER_OFFSET = 0;
var ARRAYBUFFERVIEW_DATASTART_OFFSET = 4;
var ARRAYBUFFERVIEW_BYTELENGTH_OFFSET = 8;
var ARRAYBUFFERVIEW_SIZE = 12;
var ARRAY_LENGTH_OFFSET = 12;
var ARRAY_SIZE = 16;
var E_NO_EXPORT_TABLE = "Operation requires compiling with --exportTable";
var E_NO_EXPORT_RUNTIME = "Operation requires compiling with --exportRuntime";
var F_NO_EXPORT_RUNTIME = () => {
  throw Error(E_NO_EXPORT_RUNTIME);
};
var BIGINT = typeof BigUint64Array !== "undefined";
var THIS = Symbol();
var STRING_SMALLSIZE = 192;
var STRING_CHUNKSIZE = 1024;
var utf16 = new TextDecoder("utf-16le", { fatal: true });
Object.hasOwn = Object.hasOwn || function(obj, prop) {
  return Object.prototype.hasOwnProperty.call(obj, prop);
};
function getStringImpl(buffer, ptr) {
  let len = new Uint32Array(buffer)[ptr + SIZE_OFFSET >>> 2] >>> 1;
  const wtf16 = new Uint16Array(buffer, ptr, len);
  if (len <= STRING_SMALLSIZE)
    return String.fromCharCode(...wtf16);
  try {
    return utf16.decode(wtf16);
  } catch {
    let str = "", off = 0;
    while (len - off > STRING_CHUNKSIZE) {
      str += String.fromCharCode(...wtf16.subarray(off, off += STRING_CHUNKSIZE));
    }
    return str + String.fromCharCode(...wtf16.subarray(off));
  }
}
function preInstantiate(imports) {
  const extendedExports = {};
  function getString(memory, ptr) {
    if (!memory)
      return "<yet unknown>";
    return getStringImpl(memory.buffer, ptr);
  }
  const env = imports.env = imports.env || {};
  env.abort = env.abort || function abort(msg, file, line, colm) {
    const memory = extendedExports.memory || env.memory;
    throw Error(`abort: ${getString(memory, msg)} at ${getString(memory, file)}:${line}:${colm}`);
  };
  env.trace = env.trace || function trace(msg, n, ...args) {
    const memory = extendedExports.memory || env.memory;
    console.log(`trace: ${getString(memory, msg)}${n ? " " : ""}${args.slice(0, n).join(", ")}`);
  };
  env.seed = env.seed || Date.now;
  imports.Math = imports.Math || Math;
  imports.Date = imports.Date || Date;
  return extendedExports;
}
function postInstantiate(extendedExports, instance) {
  const exports = instance.exports;
  const memory = exports.memory;
  const table = exports.table;
  const __new = exports.__new || F_NO_EXPORT_RUNTIME;
  const __pin = exports.__pin || F_NO_EXPORT_RUNTIME;
  const __unpin = exports.__unpin || F_NO_EXPORT_RUNTIME;
  const __collect = exports.__collect || F_NO_EXPORT_RUNTIME;
  const __rtti_base = exports.__rtti_base;
  const getTypeinfoCount = __rtti_base ? (arr) => arr[__rtti_base >>> 2] : F_NO_EXPORT_RUNTIME;
  extendedExports.__new = __new;
  extendedExports.__pin = __pin;
  extendedExports.__unpin = __unpin;
  extendedExports.__collect = __collect;
  function getTypeinfo(id) {
    const U32 = new Uint32Array(memory.buffer);
    if ((id >>>= 0) >= getTypeinfoCount(U32))
      throw Error(`invalid id: ${id}`);
    return U32[(__rtti_base + 4 >>> 2) + id];
  }
  function getArrayInfo(id) {
    const info = getTypeinfo(id);
    if (!(info & (ARRAYBUFFERVIEW | ARRAY | STATICARRAY)))
      throw Error(`not an array: ${id}, flags=${info}`);
    return info;
  }
  function getValueAlign(info) {
    return 31 - Math.clz32(info >>> VAL_ALIGN_OFFSET & 31);
  }
  function __newString(str) {
    if (str == null)
      return 0;
    const length = str.length;
    const ptr = __new(length << 1, STRING_ID);
    const U16 = new Uint16Array(memory.buffer);
    for (let i = 0, p = ptr >>> 1;i < length; ++i)
      U16[p + i] = str.charCodeAt(i);
    return ptr;
  }
  extendedExports.__newString = __newString;
  function __newArrayBuffer(buf) {
    if (buf == null)
      return 0;
    const bufview = new Uint8Array(buf);
    const ptr = __new(bufview.length, ARRAYBUFFER_ID);
    const U8 = new Uint8Array(memory.buffer);
    U8.set(bufview, ptr);
    return ptr;
  }
  extendedExports.__newArrayBuffer = __newArrayBuffer;
  function __getString(ptr) {
    if (!ptr)
      return null;
    const buffer = memory.buffer;
    const id = new Uint32Array(buffer)[ptr + ID_OFFSET >>> 2];
    if (id !== STRING_ID)
      throw Error(`not a string: ${ptr}`);
    return getStringImpl(buffer, ptr);
  }
  extendedExports.__getString = __getString;
  function getView(alignLog2, signed, float) {
    const buffer = memory.buffer;
    if (float) {
      switch (alignLog2) {
        case 2:
          return new Float32Array(buffer);
        case 3:
          return new Float64Array(buffer);
      }
    } else {
      switch (alignLog2) {
        case 0:
          return new (signed ? Int8Array : Uint8Array)(buffer);
        case 1:
          return new (signed ? Int16Array : Uint16Array)(buffer);
        case 2:
          return new (signed ? Int32Array : Uint32Array)(buffer);
        case 3:
          return new (signed ? BigInt64Array : BigUint64Array)(buffer);
      }
    }
    throw Error(`unsupported align: ${alignLog2}`);
  }
  function __newArray(id, valuesOrCapacity = 0) {
    const input = valuesOrCapacity;
    const info = getArrayInfo(id);
    const align = getValueAlign(info);
    const isArrayLike = typeof input !== "number";
    const length = isArrayLike ? input.length : input;
    const buf = __new(length << align, info & STATICARRAY ? id : ARRAYBUFFER_ID);
    let result;
    if (info & STATICARRAY) {
      result = buf;
    } else {
      __pin(buf);
      const arr = __new(info & ARRAY ? ARRAY_SIZE : ARRAYBUFFERVIEW_SIZE, id);
      __unpin(buf);
      const U32 = new Uint32Array(memory.buffer);
      U32[arr + ARRAYBUFFERVIEW_BUFFER_OFFSET >>> 2] = buf;
      U32[arr + ARRAYBUFFERVIEW_DATASTART_OFFSET >>> 2] = buf;
      U32[arr + ARRAYBUFFERVIEW_BYTELENGTH_OFFSET >>> 2] = length << align;
      if (info & ARRAY)
        U32[arr + ARRAY_LENGTH_OFFSET >>> 2] = length;
      result = arr;
    }
    if (isArrayLike) {
      const view = getView(align, info & VAL_SIGNED, info & VAL_FLOAT);
      const start = buf >>> align;
      if (info & VAL_MANAGED) {
        for (let i = 0;i < length; ++i) {
          view[start + i] = input[i];
        }
      } else {
        view.set(input, start);
      }
    }
    return result;
  }
  extendedExports.__newArray = __newArray;
  function __getArrayView(arr) {
    const U32 = new Uint32Array(memory.buffer);
    const id = U32[arr + ID_OFFSET >>> 2];
    const info = getArrayInfo(id);
    const align = getValueAlign(info);
    let buf = info & STATICARRAY ? arr : U32[arr + ARRAYBUFFERVIEW_DATASTART_OFFSET >>> 2];
    const length = info & ARRAY ? U32[arr + ARRAY_LENGTH_OFFSET >>> 2] : U32[buf + SIZE_OFFSET >>> 2] >>> align;
    return getView(align, info & VAL_SIGNED, info & VAL_FLOAT).subarray(buf >>>= align, buf + length);
  }
  extendedExports.__getArrayView = __getArrayView;
  function __getArray(arr) {
    const input = __getArrayView(arr);
    const len = input.length;
    const out = new Array(len);
    for (let i = 0;i < len; i++)
      out[i] = input[i];
    return out;
  }
  extendedExports.__getArray = __getArray;
  function __getArrayBuffer(ptr) {
    const buffer = memory.buffer;
    const length = new Uint32Array(buffer)[ptr + SIZE_OFFSET >>> 2];
    return buffer.slice(ptr, ptr + length);
  }
  extendedExports.__getArrayBuffer = __getArrayBuffer;
  function __getFunction(ptr) {
    if (!table)
      throw Error(E_NO_EXPORT_TABLE);
    const index = new Uint32Array(memory.buffer)[ptr >>> 2];
    return table.get(index);
  }
  extendedExports.__getFunction = __getFunction;
  function getTypedArray(Type, alignLog2, ptr) {
    return new Type(getTypedArrayView(Type, alignLog2, ptr));
  }
  function getTypedArrayView(Type, alignLog2, ptr) {
    const buffer = memory.buffer;
    const U32 = new Uint32Array(buffer);
    return new Type(buffer, U32[ptr + ARRAYBUFFERVIEW_DATASTART_OFFSET >>> 2], U32[ptr + ARRAYBUFFERVIEW_BYTELENGTH_OFFSET >>> 2] >>> alignLog2);
  }
  function attachTypedArrayFunctions(ctor, name, align) {
    extendedExports[`__get${name}`] = getTypedArray.bind(null, ctor, align);
    extendedExports[`__get${name}View`] = getTypedArrayView.bind(null, ctor, align);
  }
  [
    Int8Array,
    Uint8Array,
    Uint8ClampedArray,
    Int16Array,
    Uint16Array,
    Int32Array,
    Uint32Array,
    Float32Array,
    Float64Array
  ].forEach((ctor) => {
    attachTypedArrayFunctions(ctor, ctor.name, 31 - Math.clz32(ctor.BYTES_PER_ELEMENT));
  });
  if (BIGINT) {
    [BigUint64Array, BigInt64Array].forEach((ctor) => {
      attachTypedArrayFunctions(ctor, ctor.name.slice(3), 3);
    });
  }
  extendedExports.memory = extendedExports.memory || memory;
  extendedExports.table = extendedExports.table || table;
  return demangle(exports, extendedExports);
}
function isResponse(src) {
  return typeof Response !== "undefined" && src instanceof Response;
}
function isModule(src) {
  return src instanceof WebAssembly.Module;
}
async function instantiate(source, imports = {}) {
  if (isResponse(source = await source))
    return instantiateStreaming(source, imports);
  const module = isModule(source) ? source : await WebAssembly.compile(source);
  const extended = preInstantiate(imports);
  const instance = await WebAssembly.instantiate(module, imports);
  const exports = postInstantiate(extended, instance);
  return { module, instance, exports };
}
async function instantiateStreaming(source, imports = {}) {
  if (!WebAssembly.instantiateStreaming) {
    return instantiate(isResponse(source = await source) ? source.arrayBuffer() : source, imports);
  }
  const extended = preInstantiate(imports);
  const result = await WebAssembly.instantiateStreaming(source, imports);
  const exports = postInstantiate(extended, result.instance);
  return { ...result, exports };
}
function demangle(exports, extendedExports = {}) {
  const setArgumentsLength = exports["__argumentsLength"] ? (length) => {
    exports["__argumentsLength"].value = length;
  } : exports["__setArgumentsLength"] || exports["__setargc"] || (() => {});
  for (let internalName of Object.keys(exports)) {
    const elem = exports[internalName];
    let parts = internalName.split(".");
    let curr = extendedExports;
    while (parts.length > 1) {
      let part = parts.shift();
      if (!Object.hasOwn(curr, part))
        curr[part] = {};
      curr = curr[part];
    }
    let name = parts[0];
    let hash = name.indexOf("#");
    if (hash >= 0) {
      const className = name.substring(0, hash);
      const classElem = curr[className];
      if (typeof classElem === "undefined" || !classElem.prototype) {
        const ctor = function(...args) {
          return ctor.wrap(ctor.prototype.constructor(0, ...args));
        };
        ctor.prototype = {
          valueOf() {
            return this[THIS];
          }
        };
        ctor.wrap = function(thisValue) {
          return Object.create(ctor.prototype, { [THIS]: { value: thisValue, writable: false } });
        };
        if (classElem)
          Object.getOwnPropertyNames(classElem).forEach((name2) => Object.defineProperty(ctor, name2, Object.getOwnPropertyDescriptor(classElem, name2)));
        curr[className] = ctor;
      }
      name = name.substring(hash + 1);
      curr = curr[className].prototype;
      if (/^(get|set):/.test(name)) {
        if (!Object.hasOwn(curr, name = name.substring(4))) {
          let getter = exports[internalName.replace("set:", "get:")];
          let setter = exports[internalName.replace("get:", "set:")];
          Object.defineProperty(curr, name, {
            get() {
              return getter(this[THIS]);
            },
            set(value) {
              setter(this[THIS], value);
            },
            enumerable: true
          });
        }
      } else {
        if (name === "constructor") {
          (curr[name] = function(...args) {
            setArgumentsLength(args.length);
            return elem(...args);
          }).original = elem;
        } else {
          (curr[name] = function(...args) {
            setArgumentsLength(args.length);
            return elem(this[THIS], ...args);
          }).original = elem;
        }
      }
    } else {
      if (/^(get|set):/.test(name)) {
        if (!Object.hasOwn(curr, name = name.substring(4))) {
          Object.defineProperty(curr, name, {
            get: exports[internalName.replace("set:", "get:")],
            set: exports[internalName.replace("get:", "set:")],
            enumerable: true
          });
        }
      } else if (typeof elem === "function" && elem !== setArgumentsLength) {
        (curr[name] = (...args) => {
          setArgumentsLength(args.length);
          return elem(...args);
        }).original = elem;
      } else {
        curr[name] = elem;
      }
    }
  }
  return extendedExports;
}

// cmd/womprat/frontend/vnc-piclaw/remote-display-gc.ts
function collectAssemblyScriptGarbageBestEffort(runtime) {
  try {
    runtime?.__collect?.();
    return true;
  } catch (_error) {
    return false;
  }
}

// cmd/womprat/frontend/vnc-piclaw/remote-display-decoder.ts
var REMOTE_DISPLAY_DECODER_WASM_URL = "/vendor/remote-display-decoder.wasm";
var pipelinePromise = null;
function normalizeInput(bytes) {
  if (bytes instanceof ArrayBuffer)
    return bytes;
  if (bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength) {
    return bytes.buffer;
  }
  return bytes.slice().buffer;
}
async function loadRemoteDisplayWasmDecoder() {
  if (pipelinePromise)
    return pipelinePromise;
  pipelinePromise = (async () => {
    try {
      let callProcess = function(fnName, data, x, y, w, h, pf) {
        const input = normalizeInput(data);
        const ptr = ex.__pin(ex.__newArrayBuffer(input));
        try {
          return ex[fnName](ptr, x, y, w, h, pf.bitsPerPixel, pf.bigEndian ? 1 : 0, pf.trueColor ? 1 : 0, pf.redMax, pf.greenMax, pf.blueMax, pf.redShift, pf.greenShift, pf.blueShift);
        } finally {
          ex.__unpin(ptr);
          collectAssemblyScriptGarbageBestEffort(ex);
        }
      };
      const response = await fetch(REMOTE_DISPLAY_DECODER_WASM_URL, { credentials: "same-origin" });
      if (!response.ok)
        throw new Error(`HTTP ${response.status}`);
      const instantiated = typeof instantiateStreaming === "function" ? await instantiateStreaming(response, {}) : await instantiate(await response.arrayBuffer(), {});
      const ex = instantiated.exports;
      for (const fn of [
        "initFramebuffer",
        "getFramebufferPtr",
        "getFramebufferLen",
        "getFramebufferWidth",
        "getFramebufferHeight",
        "processRawRect",
        "processCopyRect",
        "processRreRect",
        "processHextileRect",
        "processZrleTileData",
        "decodeRawRectToRgba"
      ]) {
        if (typeof ex[fn] !== "function")
          throw new Error(`${fn} export is missing.`);
      }
      return {
        initFramebuffer(width, height) {
          ex.initFramebuffer(width, height);
        },
        getFramebuffer() {
          const ptr = ex.getFramebufferPtr();
          const len = ex.getFramebufferLen();
          return new Uint8ClampedArray(new Uint8Array(ex.memory.buffer, ptr, len).slice().buffer);
        },
        getFramebufferWidth() {
          return ex.getFramebufferWidth();
        },
        getFramebufferHeight() {
          return ex.getFramebufferHeight();
        },
        processRawRect(data, x, y, w, h, pf) {
          return callProcess("processRawRect", data, x, y, w, h, pf);
        },
        processCopyRect(dstX, dstY, w, h, srcX, srcY) {
          return ex.processCopyRect(dstX, dstY, w, h, srcX, srcY);
        },
        processRreRect(data, x, y, w, h, pf) {
          return callProcess("processRreRect", data, x, y, w, h, pf);
        },
        processHextileRect(data, x, y, w, h, pf) {
          return callProcess("processHextileRect", data, x, y, w, h, pf);
        },
        processZrleTileData(decompressed, x, y, w, h, pf) {
          return callProcess("processZrleTileData", decompressed, x, y, w, h, pf);
        },
        decodeRawRectToRgba(data, width, height, pf) {
          const input = normalizeInput(data);
          const inputPtr = ex.__pin(ex.__newArrayBuffer(input));
          try {
            const outputPtr = ex.__pin(ex.decodeRawRectToRgba(inputPtr, width, height, pf.bitsPerPixel, pf.bigEndian ? 1 : 0, pf.trueColor ? 1 : 0, pf.redMax, pf.greenMax, pf.blueMax, pf.redShift, pf.greenShift, pf.blueShift));
            try {
              return new Uint8ClampedArray(ex.__getArrayBuffer(outputPtr));
            } finally {
              ex.__unpin(outputPtr);
            }
          } finally {
            ex.__unpin(inputPtr);
            collectAssemblyScriptGarbageBestEffort(ex);
          }
        }
      };
    } catch (error) {
      console.warn("[remote-display] Failed to load WASM pipeline, using JS fallback.", error);
      return null;
    }
  })();
  return pipelinePromise;
}

// node_modules/fflate/esm/browser.js
var u8 = Uint8Array;
var u16 = Uint16Array;
var i32 = Int32Array;
var fleb = new u8([0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0, 0, 0, 0]);
var fdeb = new u8([0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 0, 0]);
var clim = new u8([16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15]);
var freb = function(eb, start) {
  var b = new u16(31);
  for (var i = 0;i < 31; ++i) {
    b[i] = start += 1 << eb[i - 1];
  }
  var r = new i32(b[30]);
  for (var i = 1;i < 30; ++i) {
    for (var j = b[i];j < b[i + 1]; ++j) {
      r[j] = j - b[i] << 5 | i;
    }
  }
  return { b, r };
};
var _a = freb(fleb, 2);
var fl = _a.b;
var revfl = _a.r;
fl[28] = 258, revfl[258] = 28;
var _b = freb(fdeb, 0);
var fd = _b.b;
var revfd = _b.r;
var rev = new u16(32768);
for (i = 0;i < 32768; ++i) {
  x = (i & 43690) >> 1 | (i & 21845) << 1;
  x = (x & 52428) >> 2 | (x & 13107) << 2;
  x = (x & 61680) >> 4 | (x & 3855) << 4;
  rev[i] = ((x & 65280) >> 8 | (x & 255) << 8) >> 1;
}
var x;
var i;
var hMap = function(cd, mb, r) {
  var s = cd.length;
  var i2 = 0;
  var l = new u16(mb);
  for (;i2 < s; ++i2) {
    if (cd[i2])
      ++l[cd[i2] - 1];
  }
  var le = new u16(mb);
  for (i2 = 1;i2 < mb; ++i2) {
    le[i2] = le[i2 - 1] + l[i2 - 1] << 1;
  }
  var co;
  if (r) {
    co = new u16(1 << mb);
    var rvb = 15 - mb;
    for (i2 = 0;i2 < s; ++i2) {
      if (cd[i2]) {
        var sv = i2 << 4 | cd[i2];
        var r_1 = mb - cd[i2];
        var v = le[cd[i2] - 1]++ << r_1;
        for (var m = v | (1 << r_1) - 1;v <= m; ++v) {
          co[rev[v] >> rvb] = sv;
        }
      }
    }
  } else {
    co = new u16(s);
    for (i2 = 0;i2 < s; ++i2) {
      if (cd[i2]) {
        co[i2] = rev[le[cd[i2] - 1]++] >> 15 - cd[i2];
      }
    }
  }
  return co;
};
var flt = new u8(288);
for (i = 0;i < 144; ++i)
  flt[i] = 8;
var i;
for (i = 144;i < 256; ++i)
  flt[i] = 9;
var i;
for (i = 256;i < 280; ++i)
  flt[i] = 7;
var i;
for (i = 280;i < 288; ++i)
  flt[i] = 8;
var i;
var fdt = new u8(32);
for (i = 0;i < 32; ++i)
  fdt[i] = 5;
var i;
var flm = /* @__PURE__ */ hMap(flt, 9, 0);
var flrm = /* @__PURE__ */ hMap(flt, 9, 1);
var fdm = /* @__PURE__ */ hMap(fdt, 5, 0);
var fdrm = /* @__PURE__ */ hMap(fdt, 5, 1);
var max = function(a) {
  var m = a[0];
  for (var i2 = 1;i2 < a.length; ++i2) {
    if (a[i2] > m)
      m = a[i2];
  }
  return m;
};
var bits = function(d, p, m) {
  var o = p / 8 | 0;
  return (d[o] | d[o + 1] << 8) >> (p & 7) & m;
};
var bits16 = function(d, p) {
  var o = p / 8 | 0;
  return (d[o] | d[o + 1] << 8 | d[o + 2] << 16) >> (p & 7);
};
var shft = function(p) {
  return (p + 7) / 8 | 0;
};
var slc = function(v, s, e) {
  if (s == null || s < 0)
    s = 0;
  if (e == null || e > v.length)
    e = v.length;
  return new u8(v.subarray(s, e));
};
var ec = [
  "unexpected EOF",
  "invalid block type",
  "invalid length/literal",
  "invalid distance",
  "stream finished",
  "no stream handler",
  ,
  "no callback",
  "invalid UTF-8 data",
  "extra field too long",
  "date not in range 1980-2099",
  "filename too long",
  "stream finishing",
  "invalid zip data"
];
var err = function(ind, msg, nt) {
  var e = new Error(msg || ec[ind]);
  e.code = ind;
  if (Error.captureStackTrace)
    Error.captureStackTrace(e, err);
  if (!nt)
    throw e;
  return e;
};
var inflt = function(dat, st, buf, dict) {
  var sl = dat.length, dl = dict ? dict.length : 0;
  if (!sl || st.f && !st.l)
    return buf || new u8(0);
  var noBuf = !buf;
  var resize = noBuf || st.i != 2;
  var noSt = st.i;
  if (noBuf)
    buf = new u8(sl * 3);
  var cbuf = function(l2) {
    var bl = buf.length;
    if (l2 > bl) {
      var nbuf = new u8(Math.max(bl * 2, l2));
      nbuf.set(buf);
      buf = nbuf;
    }
  };
  var final = st.f || 0, pos = st.p || 0, bt = st.b || 0, lm = st.l, dm = st.d, lbt = st.m, dbt = st.n;
  var tbts = sl * 8;
  do {
    if (!lm) {
      final = bits(dat, pos, 1);
      var type = bits(dat, pos + 1, 3);
      pos += 3;
      if (!type) {
        var s = shft(pos) + 4, l = dat[s - 4] | dat[s - 3] << 8, t = s + l;
        if (t > sl) {
          if (noSt)
            err(0);
          break;
        }
        if (resize)
          cbuf(bt + l);
        buf.set(dat.subarray(s, t), bt);
        st.b = bt += l, st.p = pos = t * 8, st.f = final;
        continue;
      } else if (type == 1)
        lm = flrm, dm = fdrm, lbt = 9, dbt = 5;
      else if (type == 2) {
        var hLit = bits(dat, pos, 31) + 257, hcLen = bits(dat, pos + 10, 15) + 4;
        var tl = hLit + bits(dat, pos + 5, 31) + 1;
        pos += 14;
        var ldt = new u8(tl);
        var clt = new u8(19);
        for (var i2 = 0;i2 < hcLen; ++i2) {
          clt[clim[i2]] = bits(dat, pos + i2 * 3, 7);
        }
        pos += hcLen * 3;
        var clb = max(clt), clbmsk = (1 << clb) - 1;
        var clm = hMap(clt, clb, 1);
        for (var i2 = 0;i2 < tl; ) {
          var r = clm[bits(dat, pos, clbmsk)];
          pos += r & 15;
          var s = r >> 4;
          if (s < 16) {
            ldt[i2++] = s;
          } else {
            var c = 0, n = 0;
            if (s == 16)
              n = 3 + bits(dat, pos, 3), pos += 2, c = ldt[i2 - 1];
            else if (s == 17)
              n = 3 + bits(dat, pos, 7), pos += 3;
            else if (s == 18)
              n = 11 + bits(dat, pos, 127), pos += 7;
            while (n--)
              ldt[i2++] = c;
          }
        }
        var lt = ldt.subarray(0, hLit), dt = ldt.subarray(hLit);
        lbt = max(lt);
        dbt = max(dt);
        lm = hMap(lt, lbt, 1);
        dm = hMap(dt, dbt, 1);
      } else
        err(1);
      if (pos > tbts) {
        if (noSt)
          err(0);
        break;
      }
    }
    if (resize)
      cbuf(bt + 131072);
    var lms = (1 << lbt) - 1, dms = (1 << dbt) - 1;
    var lpos = pos;
    for (;; lpos = pos) {
      var c = lm[bits16(dat, pos) & lms], sym = c >> 4;
      pos += c & 15;
      if (pos > tbts) {
        if (noSt)
          err(0);
        break;
      }
      if (!c)
        err(2);
      if (sym < 256)
        buf[bt++] = sym;
      else if (sym == 256) {
        lpos = pos, lm = null;
        break;
      } else {
        var add = sym - 254;
        if (sym > 264) {
          var i2 = sym - 257, b = fleb[i2];
          add = bits(dat, pos, (1 << b) - 1) + fl[i2];
          pos += b;
        }
        var d = dm[bits16(dat, pos) & dms], dsym = d >> 4;
        if (!d)
          err(3);
        pos += d & 15;
        var dt = fd[dsym];
        if (dsym > 3) {
          var b = fdeb[dsym];
          dt += bits16(dat, pos) & (1 << b) - 1, pos += b;
        }
        if (pos > tbts) {
          if (noSt)
            err(0);
          break;
        }
        if (resize)
          cbuf(bt + 131072);
        var end = bt + add;
        if (bt < dt) {
          var shift = dl - dt, dend = Math.min(dt, end);
          if (shift + bt < 0)
            err(3);
          for (;bt < dend; ++bt)
            buf[bt] = dict[shift + bt];
        }
        for (;bt < end; ++bt)
          buf[bt] = buf[bt - dt];
      }
    }
    st.l = lm, st.p = lpos, st.b = bt, st.f = final;
    if (lm)
      final = 1, st.m = lbt, st.d = dm, st.n = dbt;
  } while (!final);
  return bt != buf.length && noBuf ? slc(buf, 0, bt) : buf.subarray(0, bt);
};
var wbits = function(d, p, v) {
  v <<= p & 7;
  var o = p / 8 | 0;
  d[o] |= v;
  d[o + 1] |= v >> 8;
};
var wbits16 = function(d, p, v) {
  v <<= p & 7;
  var o = p / 8 | 0;
  d[o] |= v;
  d[o + 1] |= v >> 8;
  d[o + 2] |= v >> 16;
};
var hTree = function(d, mb) {
  var t = [];
  for (var i2 = 0;i2 < d.length; ++i2) {
    if (d[i2])
      t.push({ s: i2, f: d[i2] });
  }
  var s = t.length;
  var t2 = t.slice();
  if (!s)
    return { t: et, l: 0 };
  if (s == 1) {
    var v = new u8(t[0].s + 1);
    v[t[0].s] = 1;
    return { t: v, l: 1 };
  }
  t.sort(function(a, b) {
    return a.f - b.f;
  });
  t.push({ s: -1, f: 25001 });
  var l = t[0], r = t[1], i0 = 0, i1 = 1, i22 = 2;
  t[0] = { s: -1, f: l.f + r.f, l, r };
  while (i1 != s - 1) {
    l = t[t[i0].f < t[i22].f ? i0++ : i22++];
    r = t[i0 != i1 && t[i0].f < t[i22].f ? i0++ : i22++];
    t[i1++] = { s: -1, f: l.f + r.f, l, r };
  }
  var maxSym = t2[0].s;
  for (var i2 = 1;i2 < s; ++i2) {
    if (t2[i2].s > maxSym)
      maxSym = t2[i2].s;
  }
  var tr = new u16(maxSym + 1);
  var mbt = ln(t[i1 - 1], tr, 0);
  if (mbt > mb) {
    var i2 = 0, dt = 0;
    var lft = mbt - mb, cst = 1 << lft;
    t2.sort(function(a, b) {
      return tr[b.s] - tr[a.s] || a.f - b.f;
    });
    for (;i2 < s; ++i2) {
      var i2_1 = t2[i2].s;
      if (tr[i2_1] > mb) {
        dt += cst - (1 << mbt - tr[i2_1]);
        tr[i2_1] = mb;
      } else
        break;
    }
    dt >>= lft;
    while (dt > 0) {
      var i2_2 = t2[i2].s;
      if (tr[i2_2] < mb)
        dt -= 1 << mb - tr[i2_2]++ - 1;
      else
        ++i2;
    }
    for (;i2 >= 0 && dt; --i2) {
      var i2_3 = t2[i2].s;
      if (tr[i2_3] == mb) {
        --tr[i2_3];
        ++dt;
      }
    }
    mbt = mb;
  }
  return { t: new u8(tr), l: mbt };
};
var ln = function(n, l, d) {
  return n.s == -1 ? Math.max(ln(n.l, l, d + 1), ln(n.r, l, d + 1)) : l[n.s] = d;
};
var lc = function(c) {
  var s = c.length;
  while (s && !c[--s])
    ;
  var cl = new u16(++s);
  var cli = 0, cln = c[0], cls = 1;
  var w = function(v) {
    cl[cli++] = v;
  };
  for (var i2 = 1;i2 <= s; ++i2) {
    if (c[i2] == cln && i2 != s)
      ++cls;
    else {
      if (!cln && cls > 2) {
        for (;cls > 138; cls -= 138)
          w(32754);
        if (cls > 2) {
          w(cls > 10 ? cls - 11 << 5 | 28690 : cls - 3 << 5 | 12305);
          cls = 0;
        }
      } else if (cls > 3) {
        w(cln), --cls;
        for (;cls > 6; cls -= 6)
          w(8304);
        if (cls > 2)
          w(cls - 3 << 5 | 8208), cls = 0;
      }
      while (cls--)
        w(cln);
      cls = 1;
      cln = c[i2];
    }
  }
  return { c: cl.subarray(0, cli), n: s };
};
var clen = function(cf, cl) {
  var l = 0;
  for (var i2 = 0;i2 < cl.length; ++i2)
    l += cf[i2] * cl[i2];
  return l;
};
var wfblk = function(out, pos, dat) {
  var s = dat.length;
  var o = shft(pos + 2);
  out[o] = s & 255;
  out[o + 1] = s >> 8;
  out[o + 2] = out[o] ^ 255;
  out[o + 3] = out[o + 1] ^ 255;
  for (var i2 = 0;i2 < s; ++i2)
    out[o + i2 + 4] = dat[i2];
  return (o + 4 + s) * 8;
};
var wblk = function(dat, out, final, syms, lf, df, eb, li, bs, bl, p) {
  wbits(out, p++, final);
  ++lf[256];
  var _a2 = hTree(lf, 15), dlt = _a2.t, mlb = _a2.l;
  var _b2 = hTree(df, 15), ddt = _b2.t, mdb = _b2.l;
  var _c = lc(dlt), lclt = _c.c, nlc = _c.n;
  var _d = lc(ddt), lcdt = _d.c, ndc = _d.n;
  var lcfreq = new u16(19);
  for (var i2 = 0;i2 < lclt.length; ++i2)
    ++lcfreq[lclt[i2] & 31];
  for (var i2 = 0;i2 < lcdt.length; ++i2)
    ++lcfreq[lcdt[i2] & 31];
  var _e = hTree(lcfreq, 7), lct = _e.t, mlcb = _e.l;
  var nlcc = 19;
  for (;nlcc > 4 && !lct[clim[nlcc - 1]]; --nlcc)
    ;
  var flen = bl + 5 << 3;
  var ftlen = clen(lf, flt) + clen(df, fdt) + eb;
  var dtlen = clen(lf, dlt) + clen(df, ddt) + eb + 14 + 3 * nlcc + clen(lcfreq, lct) + 2 * lcfreq[16] + 3 * lcfreq[17] + 7 * lcfreq[18];
  if (bs >= 0 && flen <= ftlen && flen <= dtlen)
    return wfblk(out, p, dat.subarray(bs, bs + bl));
  var lm, ll, dm, dl;
  wbits(out, p, 1 + (dtlen < ftlen)), p += 2;
  if (dtlen < ftlen) {
    lm = hMap(dlt, mlb, 0), ll = dlt, dm = hMap(ddt, mdb, 0), dl = ddt;
    var llm = hMap(lct, mlcb, 0);
    wbits(out, p, nlc - 257);
    wbits(out, p + 5, ndc - 1);
    wbits(out, p + 10, nlcc - 4);
    p += 14;
    for (var i2 = 0;i2 < nlcc; ++i2)
      wbits(out, p + 3 * i2, lct[clim[i2]]);
    p += 3 * nlcc;
    var lcts = [lclt, lcdt];
    for (var it = 0;it < 2; ++it) {
      var clct = lcts[it];
      for (var i2 = 0;i2 < clct.length; ++i2) {
        var len = clct[i2] & 31;
        wbits(out, p, llm[len]), p += lct[len];
        if (len > 15)
          wbits(out, p, clct[i2] >> 5 & 127), p += clct[i2] >> 12;
      }
    }
  } else {
    lm = flm, ll = flt, dm = fdm, dl = fdt;
  }
  for (var i2 = 0;i2 < li; ++i2) {
    var sym = syms[i2];
    if (sym > 255) {
      var len = sym >> 18 & 31;
      wbits16(out, p, lm[len + 257]), p += ll[len + 257];
      if (len > 7)
        wbits(out, p, sym >> 23 & 31), p += fleb[len];
      var dst = sym & 31;
      wbits16(out, p, dm[dst]), p += dl[dst];
      if (dst > 3)
        wbits16(out, p, sym >> 5 & 8191), p += fdeb[dst];
    } else {
      wbits16(out, p, lm[sym]), p += ll[sym];
    }
  }
  wbits16(out, p, lm[256]);
  return p + ll[256];
};
var deo = /* @__PURE__ */ new i32([65540, 131080, 131088, 131104, 262176, 1048704, 1048832, 2114560, 2117632]);
var et = /* @__PURE__ */ new u8(0);
var dflt = function(dat, lvl, plvl, pre, post, st) {
  var s = st.z || dat.length;
  var o = new u8(pre + s + 5 * (1 + Math.ceil(s / 7000)) + post);
  var w = o.subarray(pre, o.length - post);
  var lst = st.l;
  var pos = (st.r || 0) & 7;
  if (lvl) {
    if (pos)
      w[0] = st.r >> 3;
    var opt = deo[lvl - 1];
    var n = opt >> 13, c = opt & 8191;
    var msk_1 = (1 << plvl) - 1;
    var prev = st.p || new u16(32768), head = st.h || new u16(msk_1 + 1);
    var bs1_1 = Math.ceil(plvl / 3), bs2_1 = 2 * bs1_1;
    var hsh = function(i3) {
      return (dat[i3] ^ dat[i3 + 1] << bs1_1 ^ dat[i3 + 2] << bs2_1) & msk_1;
    };
    var syms = new i32(25000);
    var lf = new u16(288), df = new u16(32);
    var lc_1 = 0, eb = 0, i2 = st.i || 0, li = 0, wi = st.w || 0, bs = 0;
    for (;i2 + 2 < s; ++i2) {
      var hv = hsh(i2);
      var imod = i2 & 32767, pimod = head[hv];
      prev[imod] = pimod;
      head[hv] = imod;
      if (wi <= i2) {
        var rem = s - i2;
        if ((lc_1 > 7000 || li > 24576) && (rem > 423 || !lst)) {
          pos = wblk(dat, w, 0, syms, lf, df, eb, li, bs, i2 - bs, pos);
          li = lc_1 = eb = 0, bs = i2;
          for (var j = 0;j < 286; ++j)
            lf[j] = 0;
          for (var j = 0;j < 30; ++j)
            df[j] = 0;
        }
        var l = 2, d = 0, ch_1 = c, dif = imod - pimod & 32767;
        if (rem > 2 && hv == hsh(i2 - dif)) {
          var maxn = Math.min(n, rem) - 1;
          var maxd = Math.min(32767, i2);
          var ml = Math.min(258, rem);
          while (dif <= maxd && --ch_1 && imod != pimod) {
            if (dat[i2 + l] == dat[i2 + l - dif]) {
              var nl = 0;
              for (;nl < ml && dat[i2 + nl] == dat[i2 + nl - dif]; ++nl)
                ;
              if (nl > l) {
                l = nl, d = dif;
                if (nl > maxn)
                  break;
                var mmd = Math.min(dif, nl - 2);
                var md = 0;
                for (var j = 0;j < mmd; ++j) {
                  var ti = i2 - dif + j & 32767;
                  var pti = prev[ti];
                  var cd = ti - pti & 32767;
                  if (cd > md)
                    md = cd, pimod = ti;
                }
              }
            }
            imod = pimod, pimod = prev[imod];
            dif += imod - pimod & 32767;
          }
        }
        if (d) {
          syms[li++] = 268435456 | revfl[l] << 18 | revfd[d];
          var lin = revfl[l] & 31, din = revfd[d] & 31;
          eb += fleb[lin] + fdeb[din];
          ++lf[257 + lin];
          ++df[din];
          wi = i2 + l;
          ++lc_1;
        } else {
          syms[li++] = dat[i2];
          ++lf[dat[i2]];
        }
      }
    }
    for (i2 = Math.max(i2, wi);i2 < s; ++i2) {
      syms[li++] = dat[i2];
      ++lf[dat[i2]];
    }
    pos = wblk(dat, w, lst, syms, lf, df, eb, li, bs, i2 - bs, pos);
    if (!lst) {
      st.r = pos & 7 | w[pos / 8 | 0] << 3;
      pos -= 7;
      st.h = head, st.p = prev, st.i = i2, st.w = wi;
    }
  } else {
    for (var i2 = st.w || 0;i2 < s + lst; i2 += 65535) {
      var e = i2 + 65535;
      if (e >= s) {
        w[pos / 8 | 0] = lst;
        e = s;
      }
      pos = wfblk(w, pos + 1, dat.subarray(i2, e));
    }
    st.i = s;
  }
  return slc(o, 0, pre + shft(pos) + post);
};
var adler = function() {
  var a = 1, b = 0;
  return {
    p: function(d) {
      var n = a, m = b;
      var l = d.length | 0;
      for (var i2 = 0;i2 != l; ) {
        var e = Math.min(i2 + 2655, l);
        for (;i2 < e; ++i2)
          m += n += d[i2];
        n = (n & 65535) + 15 * (n >> 16), m = (m & 65535) + 15 * (m >> 16);
      }
      a = n, b = m;
    },
    d: function() {
      a %= 65521, b %= 65521;
      return (a & 255) << 24 | (a & 65280) << 8 | (b & 255) << 8 | b >> 8;
    }
  };
};
var dopt = function(dat, opt, pre, post, st) {
  if (!st) {
    st = { l: 1 };
    if (opt.dictionary) {
      var dict = opt.dictionary.subarray(-32768);
      var newDat = new u8(dict.length + dat.length);
      newDat.set(dict);
      newDat.set(dat, dict.length);
      dat = newDat;
      st.w = dict.length;
    }
  }
  return dflt(dat, opt.level == null ? 6 : opt.level, opt.mem == null ? st.l ? Math.ceil(Math.max(8, Math.min(13, Math.log(dat.length))) * 1.5) : 20 : 12 + opt.mem, pre, post, st);
};
var wbytes = function(d, b, v) {
  for (;v; ++b)
    d[b] = v, v >>>= 8;
};
var zlh = function(c, o) {
  var lv = o.level, fl2 = lv == 0 ? 0 : lv < 6 ? 1 : lv == 9 ? 3 : 2;
  c[0] = 120, c[1] = fl2 << 6 | (o.dictionary && 32);
  c[1] |= 31 - (c[0] << 8 | c[1]) % 31;
  if (o.dictionary) {
    var h = adler();
    h.p(o.dictionary);
    wbytes(c, 2, h.d());
  }
};
var zls = function(d, dict) {
  if ((d[0] & 15) != 8 || d[0] >> 4 > 7 || (d[0] << 8 | d[1]) % 31)
    err(6, "invalid zlib data");
  if ((d[1] >> 5 & 1) == +!dict)
    err(6, "invalid zlib data: " + (d[1] & 32 ? "need" : "unexpected") + " dictionary");
  return (d[1] >> 3 & 4) + 2;
};
var Inflate = /* @__PURE__ */ function() {
  function Inflate2(opts, cb) {
    if (typeof opts == "function")
      cb = opts, opts = {};
    this.ondata = cb;
    var dict = opts && opts.dictionary && opts.dictionary.subarray(-32768);
    this.s = { i: 0, b: dict ? dict.length : 0 };
    this.o = new u8(32768);
    this.p = new u8(0);
    if (dict)
      this.o.set(dict);
  }
  Inflate2.prototype.e = function(c) {
    if (!this.ondata)
      err(5);
    if (this.d)
      err(4);
    if (!this.p.length)
      this.p = c;
    else if (c.length) {
      var n = new u8(this.p.length + c.length);
      n.set(this.p), n.set(c, this.p.length), this.p = n;
    }
  };
  Inflate2.prototype.c = function(final) {
    this.s.i = +(this.d = final || false);
    var bts = this.s.b;
    var dt = inflt(this.p, this.s, this.o);
    this.ondata(slc(dt, bts, this.s.b), this.d);
    this.o = slc(dt, this.s.b - 32768), this.s.b = this.o.length;
    this.p = slc(this.p, this.s.p / 8 | 0), this.s.p &= 7;
  };
  Inflate2.prototype.push = function(chunk, final) {
    this.e(chunk), this.c(final);
  };
  return Inflate2;
}();
function zlibSync(data, opts) {
  if (!opts)
    opts = {};
  var a = adler();
  a.p(data);
  var d = dopt(data, opts, opts.dictionary ? 6 : 2, 4);
  return zlh(d, opts), wbytes(d, d.length - 4, a.d()), d;
}
var Unzlib = /* @__PURE__ */ function() {
  function Unzlib2(opts, cb) {
    Inflate.call(this, opts, cb);
    this.v = opts && opts.dictionary ? 2 : 1;
  }
  Unzlib2.prototype.push = function(chunk, final) {
    Inflate.prototype.e.call(this, chunk);
    if (this.v) {
      if (this.p.length < 6 && !final)
        return;
      this.p = this.p.subarray(zls(this.p, this.v - 1)), this.v = 0;
    }
    if (final) {
      if (this.p.length < 4)
        err(6, "invalid zlib data");
      this.p = this.p.subarray(0, -4);
    }
    Inflate.prototype.c.call(this, final);
  };
  return Unzlib2;
}();
var td = typeof TextDecoder != "undefined" && /* @__PURE__ */ new TextDecoder;
var tds = 0;
try {
  td.decode(et, { stream: true });
  tds = 1;
} catch (e) {}

// cmd/womprat/frontend/vnc-piclaw/vnc-auth.ts
var IP_TABLE = [
  58,
  50,
  42,
  34,
  26,
  18,
  10,
  2,
  60,
  52,
  44,
  36,
  28,
  20,
  12,
  4,
  62,
  54,
  46,
  38,
  30,
  22,
  14,
  6,
  64,
  56,
  48,
  40,
  32,
  24,
  16,
  8,
  57,
  49,
  41,
  33,
  25,
  17,
  9,
  1,
  59,
  51,
  43,
  35,
  27,
  19,
  11,
  3,
  61,
  53,
  45,
  37,
  29,
  21,
  13,
  5,
  63,
  55,
  47,
  39,
  31,
  23,
  15,
  7
];
var FP_TABLE = [
  40,
  8,
  48,
  16,
  56,
  24,
  64,
  32,
  39,
  7,
  47,
  15,
  55,
  23,
  63,
  31,
  38,
  6,
  46,
  14,
  54,
  22,
  62,
  30,
  37,
  5,
  45,
  13,
  53,
  21,
  61,
  29,
  36,
  4,
  44,
  12,
  52,
  20,
  60,
  28,
  35,
  3,
  43,
  11,
  51,
  19,
  59,
  27,
  34,
  2,
  42,
  10,
  50,
  18,
  58,
  26,
  33,
  1,
  41,
  9,
  49,
  17,
  57,
  25
];
var E_TABLE = [
  32,
  1,
  2,
  3,
  4,
  5,
  4,
  5,
  6,
  7,
  8,
  9,
  8,
  9,
  10,
  11,
  12,
  13,
  12,
  13,
  14,
  15,
  16,
  17,
  16,
  17,
  18,
  19,
  20,
  21,
  20,
  21,
  22,
  23,
  24,
  25,
  24,
  25,
  26,
  27,
  28,
  29,
  28,
  29,
  30,
  31,
  32,
  1
];
var P_TABLE = [
  16,
  7,
  20,
  21,
  29,
  12,
  28,
  17,
  1,
  15,
  23,
  26,
  5,
  18,
  31,
  10,
  2,
  8,
  24,
  14,
  32,
  27,
  3,
  9,
  19,
  13,
  30,
  6,
  22,
  11,
  4,
  25
];
var PC1_TABLE = [
  57,
  49,
  41,
  33,
  25,
  17,
  9,
  1,
  58,
  50,
  42,
  34,
  26,
  18,
  10,
  2,
  59,
  51,
  43,
  35,
  27,
  19,
  11,
  3,
  60,
  52,
  44,
  36,
  63,
  55,
  47,
  39,
  31,
  23,
  15,
  7,
  62,
  54,
  46,
  38,
  30,
  22,
  14,
  6,
  61,
  53,
  45,
  37,
  29,
  21,
  13,
  5,
  28,
  20,
  12,
  4
];
var PC2_TABLE = [
  14,
  17,
  11,
  24,
  1,
  5,
  3,
  28,
  15,
  6,
  21,
  10,
  23,
  19,
  12,
  4,
  26,
  8,
  16,
  7,
  27,
  20,
  13,
  2,
  41,
  52,
  31,
  37,
  47,
  55,
  30,
  40,
  51,
  45,
  33,
  48,
  44,
  49,
  39,
  56,
  34,
  53,
  46,
  42,
  50,
  36,
  29,
  32
];
var ROTATIONS = [1, 1, 2, 2, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2, 2, 1];
var S_BOXES = [
  [
    [14, 4, 13, 1, 2, 15, 11, 8, 3, 10, 6, 12, 5, 9, 0, 7],
    [0, 15, 7, 4, 14, 2, 13, 1, 10, 6, 12, 11, 9, 5, 3, 8],
    [4, 1, 14, 8, 13, 6, 2, 11, 15, 12, 9, 7, 3, 10, 5, 0],
    [15, 12, 8, 2, 4, 9, 1, 7, 5, 11, 3, 14, 10, 0, 6, 13]
  ],
  [
    [15, 1, 8, 14, 6, 11, 3, 4, 9, 7, 2, 13, 12, 0, 5, 10],
    [3, 13, 4, 7, 15, 2, 8, 14, 12, 0, 1, 10, 6, 9, 11, 5],
    [0, 14, 7, 11, 10, 4, 13, 1, 5, 8, 12, 6, 9, 3, 2, 15],
    [13, 8, 10, 1, 3, 15, 4, 2, 11, 6, 7, 12, 0, 5, 14, 9]
  ],
  [
    [10, 0, 9, 14, 6, 3, 15, 5, 1, 13, 12, 7, 11, 4, 2, 8],
    [13, 7, 0, 9, 3, 4, 6, 10, 2, 8, 5, 14, 12, 11, 15, 1],
    [13, 6, 4, 9, 8, 15, 3, 0, 11, 1, 2, 12, 5, 10, 14, 7],
    [1, 10, 13, 0, 6, 9, 8, 7, 4, 15, 14, 3, 11, 5, 2, 12]
  ],
  [
    [7, 13, 14, 3, 0, 6, 9, 10, 1, 2, 8, 5, 11, 12, 4, 15],
    [13, 8, 11, 5, 6, 15, 0, 3, 4, 7, 2, 12, 1, 10, 14, 9],
    [10, 6, 9, 0, 12, 11, 7, 13, 15, 1, 3, 14, 5, 2, 8, 4],
    [3, 15, 0, 6, 10, 1, 13, 8, 9, 4, 5, 11, 12, 7, 2, 14]
  ],
  [
    [2, 12, 4, 1, 7, 10, 11, 6, 8, 5, 3, 15, 13, 0, 14, 9],
    [14, 11, 2, 12, 4, 7, 13, 1, 5, 0, 15, 10, 3, 9, 8, 6],
    [4, 2, 1, 11, 10, 13, 7, 8, 15, 9, 12, 5, 6, 3, 0, 14],
    [11, 8, 12, 7, 1, 14, 2, 13, 6, 15, 0, 9, 10, 4, 5, 3]
  ],
  [
    [12, 1, 10, 15, 9, 2, 6, 8, 0, 13, 3, 4, 14, 7, 5, 11],
    [10, 15, 4, 2, 7, 12, 9, 5, 6, 1, 13, 14, 0, 11, 3, 8],
    [9, 14, 15, 5, 2, 8, 12, 3, 7, 0, 4, 10, 1, 13, 11, 6],
    [4, 3, 2, 12, 9, 5, 15, 10, 11, 14, 1, 7, 6, 0, 8, 13]
  ],
  [
    [4, 11, 2, 14, 15, 0, 8, 13, 3, 12, 9, 7, 5, 10, 6, 1],
    [13, 0, 11, 7, 4, 9, 1, 10, 14, 3, 5, 12, 2, 15, 8, 6],
    [1, 4, 11, 13, 12, 3, 7, 14, 10, 15, 6, 8, 0, 5, 9, 2],
    [6, 11, 13, 8, 1, 4, 10, 7, 9, 5, 0, 15, 14, 2, 3, 12]
  ],
  [
    [13, 2, 8, 4, 6, 15, 11, 1, 10, 9, 3, 14, 5, 0, 12, 7],
    [1, 15, 13, 8, 10, 3, 7, 4, 12, 5, 6, 11, 0, 14, 9, 2],
    [7, 11, 4, 1, 9, 12, 14, 2, 0, 6, 10, 13, 15, 3, 5, 8],
    [2, 1, 14, 7, 4, 10, 8, 13, 15, 12, 9, 0, 3, 5, 6, 11]
  ]
];
var REVERSED_BITS = new Uint8Array(256);
for (let value = 0;value < 256; value += 1) {
  let reversed = 0;
  for (let bit = 0;bit < 8; bit += 1) {
    reversed = reversed << 1 | value >> bit & 1;
  }
  REVERSED_BITS[value] = reversed;
}
function toUint8Array(bytes) {
  if (bytes instanceof Uint8Array)
    return bytes;
  if (bytes instanceof ArrayBuffer)
    return new Uint8Array(bytes);
  if (ArrayBuffer.isView(bytes))
    return new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  return new Uint8Array(0);
}
function bytesToBigInt(bytes) {
  let value = 0n;
  const source = toUint8Array(bytes);
  for (const byte of source) {
    value = value << 8n | BigInt(byte);
  }
  return value;
}
function bigIntToBytes(value, length) {
  const out = new Uint8Array(length);
  let remaining = BigInt(value);
  for (let index = length - 1;index >= 0; index -= 1) {
    out[index] = Number(remaining & 0xffn);
    remaining >>= 8n;
  }
  return out;
}
function permuteBits(input, table, inputBitLength) {
  let output = 0n;
  for (const position of table) {
    const bit = BigInt(input) >> BigInt(inputBitLength - position) & 1n;
    output = output << 1n | bit;
  }
  return output;
}
function rotateLeft28(value, amount) {
  const width = 28n;
  const mask = (1n << width) - 1n;
  const shift = BigInt(amount % 28);
  return (value << shift | value >> width - shift) & mask;
}
function buildDesSubkeys(keyBytes) {
  const key56 = permuteBits(bytesToBigInt(keyBytes), PC1_TABLE, 64);
  let left = key56 >> 28n & 0x0fffffffn;
  let right = key56 & 0x0fffffffn;
  const subkeys = [];
  for (const rotation of ROTATIONS) {
    left = rotateLeft28(left, rotation);
    right = rotateLeft28(right, rotation);
    const combined = left << 28n | right;
    subkeys.push(permuteBits(combined, PC2_TABLE, 56));
  }
  return subkeys;
}
function applySBoxes(value48) {
  let output = 0n;
  for (let index = 0;index < 8; index += 1) {
    const shift = BigInt((7 - index) * 6);
    const chunk = Number(value48 >> shift & 0x3fn);
    const row = (chunk & 32) >> 4 | chunk & 1;
    const column = chunk >> 1 & 15;
    output = output << 4n | BigInt(S_BOXES[index][row][column]);
  }
  return output;
}
function desFeistel(right32, subkey48) {
  const expanded = permuteBits(right32, E_TABLE, 32) ^ BigInt(subkey48);
  const substituted = applySBoxes(expanded);
  return permuteBits(substituted, P_TABLE, 32);
}
function encryptDesBlock(blockBytes, keyBytes) {
  const subkeys = buildDesSubkeys(keyBytes);
  const initial = permuteBits(bytesToBigInt(blockBytes), IP_TABLE, 64);
  let left = initial >> 32n & 0xffffffffn;
  let right = initial & 0xffffffffn;
  for (const subkey of subkeys) {
    const nextLeft = right;
    const nextRight = (left ^ desFeistel(right, subkey)) & 0xffffffffn;
    left = nextLeft;
    right = nextRight;
  }
  const preoutput = right << 32n | left;
  return bigIntToBytes(permuteBits(preoutput, FP_TABLE, 64), 8);
}
function buildVncPasswordKey(password) {
  const text = String(password ?? "");
  const key = new Uint8Array(8);
  for (let index = 0;index < 8; index += 1) {
    const codeUnit = index < text.length ? text.charCodeAt(index) & 255 : 0;
    key[index] = REVERSED_BITS[codeUnit];
  }
  return key;
}
function buildVncPasswordAuthResponse(password, challenge) {
  const challengeBytes = toUint8Array(challenge);
  if (challengeBytes.byteLength !== 16) {
    throw new Error(`Invalid VNC auth challenge length ${challengeBytes.byteLength}; expected 16 bytes.`);
  }
  const key = buildVncPasswordKey(password);
  const response = new Uint8Array(16);
  response.set(encryptDesBlock(challengeBytes.slice(0, 8), key), 0);
  response.set(encryptDesBlock(challengeBytes.slice(8, 16), key), 8);
  return response;
}

// cmd/womprat/frontend/vnc-piclaw/remote-display-vnc.ts
var PROTOCOL = "vnc";
function toEncodingValue(value) {
  return Number(value);
}
function normalizeEncodings(encodings) {
  const raw = Array.isArray(encodings) ? encodings : typeof encodings === "string" ? encodings.split(",").map((item) => item.trim()).filter((item) => item.length > 0) : [];
  const values = [];
  const seen = new Set;
  for (const item of raw) {
    const value = toEncodingValue(item);
    if (!Number.isFinite(value))
      continue;
    const normalized = Number(value);
    if (!seen.has(normalized)) {
      values.push(normalized);
      seen.add(normalized);
    }
  }
  if (values.length > 0)
    return values;
  return [5, 2, 1, 0, -223];
}
function toUint8Array2(chunk) {
  if (chunk instanceof Uint8Array)
    return chunk;
  if (chunk instanceof ArrayBuffer)
    return new Uint8Array(chunk);
  if (ArrayBuffer.isView(chunk))
    return new Uint8Array(chunk.buffer, chunk.byteOffset, chunk.byteLength);
  return new Uint8Array(0);
}
function concatBytes(a, b) {
  const left = toUint8Array2(a);
  const right = toUint8Array2(b);
  if (!left.byteLength)
    return new Uint8Array(right);
  if (!right.byteLength)
    return new Uint8Array(left);
  const merged = new Uint8Array(left.byteLength + right.byteLength);
  merged.set(left, 0);
  merged.set(right, left.byteLength);
  return merged;
}
function concatByteChunks(chunks) {
  let total = 0;
  for (const chunk of chunks || [])
    total += chunk?.byteLength || 0;
  const merged = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks || []) {
    const bytes = toUint8Array2(chunk);
    merged.set(bytes, offset);
    offset += bytes.byteLength;
  }
  return merged;
}
function createZrleInflater() {
  return (compressed) => {
    const payload = toUint8Array2(compressed);
    try {
      const chunks = [];
      const inflator = new Unzlib((chunk) => {
        chunks.push(new Uint8Array(chunk));
      });
      inflator.push(payload, true);
      if (inflator.err) {
        throw new Error(inflator.msg || "zlib decompression error");
      }
      return concatByteChunks(chunks);
    } catch (error) {
      try {
        const fallback = zlibSync(payload);
        return fallback instanceof Uint8Array ? fallback : new Uint8Array(fallback);
      } catch (fallbackError) {
        const message = fallbackError instanceof Error ? fallbackError.message : "unexpected EOF";
        throw new Error(`unexpected EOF: ${message}`);
      }
    }
  };
}
function asciiBytes(text) {
  return new TextEncoder().encode(String(text || ""));
}
function bytesToAscii(bytes) {
  return new TextDecoder().decode(toUint8Array2(bytes));
}
function parseVersionString(text) {
  const match = /^RFB (\d{3})\.(\d{3})\n$/.exec(String(text || ""));
  if (!match)
    return null;
  return {
    major: parseInt(match[1], 10),
    minor: parseInt(match[2], 10),
    text: match[0]
  };
}
function chooseClientVersion(serverVersion) {
  if (!serverVersion)
    return `RFB 003.008
`;
  if (serverVersion.major > 3 || serverVersion.minor >= 8)
    return `RFB 003.008
`;
  if (serverVersion.minor >= 7)
    return `RFB 003.007
`;
  return `RFB 003.003
`;
}
function parsePixelFormat(view, offset = 0) {
  return {
    bitsPerPixel: view.getUint8(offset),
    depth: view.getUint8(offset + 1),
    bigEndian: view.getUint8(offset + 2) === 1,
    trueColor: view.getUint8(offset + 3) === 1,
    redMax: view.getUint16(offset + 4, false),
    greenMax: view.getUint16(offset + 6, false),
    blueMax: view.getUint16(offset + 8, false),
    redShift: view.getUint8(offset + 10),
    greenShift: view.getUint8(offset + 11),
    blueShift: view.getUint8(offset + 12)
  };
}
function encodePixelFormat(format) {
  const buffer = new ArrayBuffer(20);
  const view = new DataView(buffer);
  view.setUint8(0, 0);
  view.setUint8(1, 0);
  view.setUint8(2, 0);
  view.setUint8(3, 0);
  view.setUint8(4, format.bitsPerPixel);
  view.setUint8(5, format.depth);
  view.setUint8(6, format.bigEndian ? 1 : 0);
  view.setUint8(7, format.trueColor ? 1 : 0);
  view.setUint16(8, format.redMax, false);
  view.setUint16(10, format.greenMax, false);
  view.setUint16(12, format.blueMax, false);
  view.setUint8(14, format.redShift);
  view.setUint8(15, format.greenShift);
  view.setUint8(16, format.blueShift);
  return new Uint8Array(buffer);
}
function buildSetEncodings(encodings) {
  const list = Array.isArray(encodings) ? encodings : [];
  const buffer = new ArrayBuffer(4 + list.length * 4);
  const view = new DataView(buffer);
  view.setUint8(0, 2);
  view.setUint8(1, 0);
  view.setUint16(2, list.length, false);
  let offset = 4;
  for (const encoding of list) {
    view.setInt32(offset, Number(encoding || 0), false);
    offset += 4;
  }
  return new Uint8Array(buffer);
}
function buildFramebufferUpdateRequest(incremental, width, height, x2 = 0, y = 0) {
  const buffer = new ArrayBuffer(10);
  const view = new DataView(buffer);
  view.setUint8(0, 3);
  view.setUint8(1, incremental ? 1 : 0);
  view.setUint16(2, x2, false);
  view.setUint16(4, y, false);
  view.setUint16(6, Math.max(0, width || 0), false);
  view.setUint16(8, Math.max(0, height || 0), false);
  return new Uint8Array(buffer);
}
function scaleChannel(value, max2) {
  const numericMax = Number(max2 || 0);
  if (numericMax <= 0)
    return 0;
  if (numericMax === 255)
    return value & 255;
  return Math.max(0, Math.min(255, Math.round((value || 0) * 255 / numericMax)));
}
function readPixelValue(bytes, offset, bytesPerPixel, bigEndian) {
  if (bytesPerPixel === 1)
    return bytes[offset];
  if (bytesPerPixel === 2) {
    return bigEndian ? (bytes[offset] << 8 | bytes[offset + 1]) >>> 0 : (bytes[offset] | bytes[offset + 1] << 8) >>> 0;
  }
  if (bytesPerPixel === 3) {
    return bigEndian ? (bytes[offset] << 16 | bytes[offset + 1] << 8 | bytes[offset + 2]) >>> 0 : (bytes[offset] | bytes[offset + 1] << 8 | bytes[offset + 2] << 16) >>> 0;
  }
  if (bytesPerPixel === 4) {
    return bigEndian ? (bytes[offset] << 24 >>> 0 | bytes[offset + 1] << 16 | bytes[offset + 2] << 8 | bytes[offset + 3]) >>> 0 : (bytes[offset] | bytes[offset + 1] << 8 | bytes[offset + 2] << 16 | bytes[offset + 3] << 24 >>> 0) >>> 0;
  }
  return 0;
}
function decodeRawRectToRgba(bytes, width, height, pixelFormat) {
  const format = pixelFormat || DEFAULT_CLIENT_PIXEL_FORMAT;
  const src = toUint8Array2(bytes);
  const bytesPerPixel = Math.max(1, Math.floor(Number(format.bitsPerPixel || 0) / 8));
  const expected = Math.max(0, width || 0) * Math.max(0, height || 0) * bytesPerPixel;
  if (src.byteLength < expected) {
    throw new Error(`Incomplete raw rectangle payload: expected ${expected} byte(s), got ${src.byteLength}`);
  }
  if (!format.trueColor) {
    throw new Error("Indexed-colour VNC framebuffers are not supported yet.");
  }
  const rgba = new Uint8ClampedArray(Math.max(0, width || 0) * Math.max(0, height || 0) * 4);
  let srcOffset = 0;
  let dstOffset = 0;
  for (let i2 = 0;i2 < Math.max(0, width || 0) * Math.max(0, height || 0); i2 += 1) {
    const value = readPixelValue(src, srcOffset, bytesPerPixel, format.bigEndian);
    const red = scaleChannel(value >>> format.redShift & format.redMax, format.redMax);
    const green = scaleChannel(value >>> format.greenShift & format.greenMax, format.greenMax);
    const blue = scaleChannel(value >>> format.blueShift & format.blueMax, format.blueMax);
    rgba[dstOffset] = red;
    rgba[dstOffset + 1] = green;
    rgba[dstOffset + 2] = blue;
    rgba[dstOffset + 3] = 255;
    srcOffset += bytesPerPixel;
    dstOffset += 4;
  }
  return rgba;
}
function decodePixelToRgba(bytes, offset, pixelFormat) {
  const format = pixelFormat || DEFAULT_CLIENT_PIXEL_FORMAT;
  const bytesPerPixel = Math.max(1, Math.floor(Number(format.bitsPerPixel || 0) / 8));
  if (bytes.byteLength < offset + bytesPerPixel)
    return null;
  const value = readPixelValue(bytes, offset, bytesPerPixel, format.bigEndian);
  return {
    rgba: [
      scaleChannel(value >>> format.redShift & format.redMax, format.redMax),
      scaleChannel(value >>> format.greenShift & format.greenMax, format.greenMax),
      scaleChannel(value >>> format.blueShift & format.blueMax, format.blueMax),
      255
    ],
    bytesPerPixel
  };
}
function fillRgbaRect(surface, surfaceWidth, x2, y, width, height, rgba) {
  if (!rgba)
    return;
  for (let row = 0;row < height; row += 1) {
    for (let col = 0;col < width; col += 1) {
      const dst = ((y + row) * surfaceWidth + (x2 + col)) * 4;
      surface[dst] = rgba[0];
      surface[dst + 1] = rgba[1];
      surface[dst + 2] = rgba[2];
      surface[dst + 3] = rgba[3];
    }
  }
}
function blitRgbaTile(surface, surfaceWidth, tileX, tileY, tileWidth, tileHeight, tileRgba) {
  for (let row = 0;row < tileHeight; row += 1) {
    const srcStart = row * tileWidth * 4;
    const dstStart = ((tileY + row) * surfaceWidth + tileX) * 4;
    surface.set(tileRgba.subarray(srcStart, srcStart + tileWidth * 4), dstStart);
  }
}
function parseZrleRunLength(bytes, offset) {
  let cursor = offset;
  let runLength = 1;
  while (true) {
    if (bytes.byteLength < cursor + 1)
      return null;
    const value = bytes[cursor++];
    runLength += value;
    if (value !== 255)
      break;
  }
  return { consumed: cursor - offset, runLength };
}
function parseZrleRect(bytes, offset, width, height, pixelFormat, decodeRawRect, inflateZrle) {
  const format = pixelFormat || DEFAULT_CLIENT_PIXEL_FORMAT;
  const bytesPerPixel = Math.max(1, Math.floor(Number(format.bitsPerPixel || 0) / 8));
  if (bytes.byteLength < offset + 4)
    return null;
  const compressedLength = new DataView(bytes.buffer, bytes.byteOffset + offset, 4).getUint32(0, false);
  if (bytes.byteLength < offset + 4 + compressedLength)
    return null;
  const compressed = bytes.slice(offset + 4, offset + 4 + compressedLength);
  let decoded;
  try {
    decoded = inflateZrle(compressed);
  } catch {
    return {
      consumed: 4 + compressedLength,
      skipped: true
    };
  }
  let cursor = 0;
  const rgba = new Uint8ClampedArray(Math.max(0, width || 0) * Math.max(0, height || 0) * 4);
  for (let tileY = 0;tileY < height; tileY += 64) {
    const tileHeight = Math.min(64, height - tileY);
    for (let tileX = 0;tileX < width; tileX += 64) {
      const tileWidth = Math.min(64, width - tileX);
      if (decoded.byteLength < cursor + 1)
        return null;
      const subencoding = decoded[cursor++];
      const paletteSize = subencoding & 127;
      const runLengthEncoded = (subencoding & 128) !== 0;
      if (!runLengthEncoded && paletteSize === 0) {
        const rawLength = tileWidth * tileHeight * bytesPerPixel;
        if (decoded.byteLength < cursor + rawLength)
          return null;
        const tileRgba = decodeRawRect(decoded.slice(cursor, cursor + rawLength), tileWidth, tileHeight, format);
        cursor += rawLength;
        blitRgbaTile(rgba, width, tileX, tileY, tileWidth, tileHeight, tileRgba);
        continue;
      }
      if (!runLengthEncoded && paletteSize === 1) {
        const background = decodePixelToRgba(decoded, cursor, format);
        if (!background)
          return null;
        cursor += background.bytesPerPixel;
        fillRgbaRect(rgba, width, tileX, tileY, tileWidth, tileHeight, background.rgba);
        continue;
      }
      if (!runLengthEncoded && paletteSize > 1 && paletteSize <= 16) {
        const palette = [];
        for (let i2 = 0;i2 < paletteSize; i2 += 1) {
          const color = decodePixelToRgba(decoded, cursor, format);
          if (!color)
            return null;
          cursor += color.bytesPerPixel;
          palette.push(color.rgba);
        }
        const bitsPerIndex = paletteSize <= 2 ? 1 : paletteSize <= 4 ? 2 : 4;
        const rowBytes = Math.ceil(tileWidth * bitsPerIndex / 8);
        const packedLength = rowBytes * tileHeight;
        if (decoded.byteLength < cursor + packedLength)
          return null;
        for (let row = 0;row < tileHeight; row += 1) {
          const rowStart = cursor + row * rowBytes;
          for (let col = 0;col < tileWidth; col += 1) {
            const bitIndex = col * bitsPerIndex;
            const byteIndex = rowStart + (bitIndex >> 3);
            const shift = 8 - bitsPerIndex - (bitIndex & 7);
            const paletteIndex = decoded[byteIndex] >> shift & (1 << bitsPerIndex) - 1;
            fillRgbaRect(rgba, width, tileX + col, tileY + row, 1, 1, palette[paletteIndex]);
          }
        }
        cursor += packedLength;
        continue;
      }
      if (runLengthEncoded && paletteSize === 0) {
        let px = 0;
        let py = 0;
        while (py < tileHeight) {
          const color = decodePixelToRgba(decoded, cursor, format);
          if (!color)
            return null;
          cursor += color.bytesPerPixel;
          const run = parseZrleRunLength(decoded, cursor);
          if (!run)
            return null;
          cursor += run.consumed;
          for (let i2 = 0;i2 < run.runLength; i2 += 1) {
            fillRgbaRect(rgba, width, tileX + px, tileY + py, 1, 1, color.rgba);
            px += 1;
            if (px >= tileWidth) {
              px = 0;
              py += 1;
              if (py >= tileHeight)
                break;
            }
          }
        }
        continue;
      }
      if (runLengthEncoded && paletteSize > 0) {
        const palette = [];
        for (let i2 = 0;i2 < paletteSize; i2 += 1) {
          const color = decodePixelToRgba(decoded, cursor, format);
          if (!color)
            return null;
          cursor += color.bytesPerPixel;
          palette.push(color.rgba);
        }
        let px = 0;
        let py = 0;
        while (py < tileHeight) {
          if (decoded.byteLength < cursor + 1)
            return null;
          const indexByte = decoded[cursor++];
          let paletteIndex = indexByte;
          let runLength = 1;
          if (indexByte & 128) {
            paletteIndex = indexByte & 127;
            const run = parseZrleRunLength(decoded, cursor);
            if (!run)
              return null;
            cursor += run.consumed;
            runLength = run.runLength;
          }
          const color = palette[paletteIndex];
          if (!color)
            return null;
          for (let i2 = 0;i2 < runLength; i2 += 1) {
            fillRgbaRect(rgba, width, tileX + px, tileY + py, 1, 1, color);
            px += 1;
            if (px >= tileWidth) {
              px = 0;
              py += 1;
              if (py >= tileHeight)
                break;
            }
          }
        }
        continue;
      }
      return {
        consumed: 4 + compressedLength,
        skipped: true
      };
    }
  }
  return {
    consumed: 4 + compressedLength,
    rgba,
    decompressed: decoded
  };
}
function parseRreRect(bytes, offset, width, height, pixelFormat) {
  const format = pixelFormat || DEFAULT_CLIENT_PIXEL_FORMAT;
  const bytesPerPixel = Math.max(1, Math.floor(Number(format.bitsPerPixel || 0) / 8));
  if (bytes.byteLength < offset + 4 + bytesPerPixel)
    return null;
  const view = new DataView(bytes.buffer, bytes.byteOffset + offset, bytes.byteLength - offset);
  const subrectCount = view.getUint32(0, false);
  const totalSize = 4 + bytesPerPixel + subrectCount * (bytesPerPixel + 8);
  if (bytes.byteLength < offset + totalSize)
    return null;
  let cursor = offset + 4;
  const background = decodePixelToRgba(bytes, cursor, format);
  if (!background)
    return null;
  cursor += background.bytesPerPixel;
  const rgba = new Uint8ClampedArray(Math.max(0, width || 0) * Math.max(0, height || 0) * 4);
  fillRgbaRect(rgba, width, 0, 0, width, height, background.rgba);
  for (let i2 = 0;i2 < subrectCount; i2 += 1) {
    const color = decodePixelToRgba(bytes, cursor, format);
    if (!color)
      return null;
    cursor += color.bytesPerPixel;
    if (bytes.byteLength < cursor + 8)
      return null;
    const rectView = new DataView(bytes.buffer, bytes.byteOffset + cursor, 8);
    const x2 = rectView.getUint16(0, false);
    const y = rectView.getUint16(2, false);
    const rectWidth = rectView.getUint16(4, false);
    const rectHeight = rectView.getUint16(6, false);
    cursor += 8;
    fillRgbaRect(rgba, width, x2, y, rectWidth, rectHeight, color.rgba);
  }
  return {
    consumed: cursor - offset,
    rgba
  };
}
function parseHextileRect(bytes, offset, width, height, pixelFormat, decodeRawRect) {
  const format = pixelFormat || DEFAULT_CLIENT_PIXEL_FORMAT;
  const bytesPerPixel = Math.max(1, Math.floor(Number(format.bitsPerPixel || 0) / 8));
  const rgba = new Uint8ClampedArray(Math.max(0, width || 0) * Math.max(0, height || 0) * 4);
  let cursor = offset;
  let background = [0, 0, 0, 255];
  let foreground = [255, 255, 255, 255];
  for (let tileY = 0;tileY < height; tileY += 16) {
    const tileHeight = Math.min(16, height - tileY);
    for (let tileX = 0;tileX < width; tileX += 16) {
      const tileWidth = Math.min(16, width - tileX);
      if (bytes.byteLength < cursor + 1)
        return null;
      const subencoding = bytes[cursor++];
      if (subencoding & 1) {
        const rawLength = tileWidth * tileHeight * bytesPerPixel;
        if (bytes.byteLength < cursor + rawLength)
          return null;
        const tileRgba = decodeRawRect(bytes.slice(cursor, cursor + rawLength), tileWidth, tileHeight, format);
        cursor += rawLength;
        blitRgbaTile(rgba, width, tileX, tileY, tileWidth, tileHeight, tileRgba);
        continue;
      }
      if (subencoding & 2) {
        const decoded = decodePixelToRgba(bytes, cursor, format);
        if (!decoded)
          return null;
        background = decoded.rgba;
        cursor += decoded.bytesPerPixel;
      }
      fillRgbaRect(rgba, width, tileX, tileY, tileWidth, tileHeight, background);
      if (subencoding & 4) {
        const decoded = decodePixelToRgba(bytes, cursor, format);
        if (!decoded)
          return null;
        foreground = decoded.rgba;
        cursor += decoded.bytesPerPixel;
      }
      if (subencoding & 8) {
        if (bytes.byteLength < cursor + 1)
          return null;
        const subrectCount = bytes[cursor++];
        for (let i2 = 0;i2 < subrectCount; i2 += 1) {
          let color = foreground;
          if (subencoding & 16) {
            const decoded = decodePixelToRgba(bytes, cursor, format);
            if (!decoded)
              return null;
            color = decoded.rgba;
            cursor += decoded.bytesPerPixel;
          }
          if (bytes.byteLength < cursor + 2)
            return null;
          const xy = bytes[cursor++];
          const wh = bytes[cursor++];
          const subX = xy >> 4;
          const subY = xy & 15;
          const subWidth = (wh >> 4) + 1;
          const subHeight = (wh & 15) + 1;
          fillRgbaRect(rgba, width, tileX + subX, tileY + subY, subWidth, subHeight, color);
        }
      }
    }
  }
  return {
    consumed: cursor - offset,
    rgba
  };
}
var DEFAULT_CLIENT_PIXEL_FORMAT = {
  bitsPerPixel: 32,
  depth: 24,
  bigEndian: false,
  trueColor: true,
  redMax: 255,
  greenMax: 255,
  blueMax: 255,
  redShift: 16,
  greenShift: 8,
  blueShift: 0
};

class VncRemoteDisplayProtocol {
  protocol = PROTOCOL;
  state;
  framebufferWidth;
  framebufferHeight;
  serverName;
  constructor(options = {}) {
    this.shared = options.shared !== false;
    this.decodeRawRect = typeof options.decodeRawRect === "function" ? options.decodeRawRect : decodeRawRectToRgba;
    this.pipeline = options.pipeline || null;
    this.encodings = normalizeEncodings(options.encodings || null);
    this.state = "version";
    this.buffer = new Uint8Array(0);
    this.serverVersion = null;
    this.clientVersionText = null;
    this.framebufferWidth = 0;
    this.framebufferHeight = 0;
    this.serverName = "";
    this.serverPixelFormat = null;
    this.clientPixelFormat = { ...DEFAULT_CLIENT_PIXEL_FORMAT };
    this.password = typeof options.password === "string" && options.password.length > 0 ? options.password : null;
    this.inflateZrle = typeof options.inflateZrle === "function" ? options.inflateZrle : createZrleInflater();
  }
  receive(chunk) {
    if (chunk) {
      this.buffer = concatBytes(this.buffer, chunk);
    }
    const events = [];
    const outgoing = [];
    let progressed = true;
    while (progressed) {
      progressed = false;
      if (this.state === "version") {
        if (this.buffer.byteLength < 12)
          break;
        const bytes = this.consume(12);
        const text = bytesToAscii(bytes);
        const version = parseVersionString(text);
        if (!version) {
          throw new Error(`Unsupported RFB version banner: ${text || "<empty>"}`);
        }
        this.serverVersion = version;
        this.clientVersionText = chooseClientVersion(version);
        outgoing.push(asciiBytes(this.clientVersionText));
        events.push({ type: "protocol-version", protocol: PROTOCOL, server: version.text.trim(), client: this.clientVersionText.trim() });
        this.state = version.minor >= 7 ? "security-types" : "security-type-33";
        progressed = true;
        continue;
      }
      if (this.state === "security-types") {
        if (this.buffer.byteLength < 1)
          break;
        const count = this.buffer[0];
        if (count === 0) {
          if (this.buffer.byteLength < 5)
            break;
          const view = new DataView(this.buffer.buffer, this.buffer.byteOffset, this.buffer.byteLength);
          const reasonLength = view.getUint32(1, false);
          if (this.buffer.byteLength < 5 + reasonLength)
            break;
          this.consume(1);
          const reason = bytesToAscii(this.consume(4 + reasonLength).slice(4));
          throw new Error(reason || "VNC server rejected the connection.");
        }
        if (this.buffer.byteLength < 1 + count)
          break;
        this.consume(1);
        const types = Array.from(this.consume(count));
        events.push({ type: "security-types", protocol: PROTOCOL, types });
        let selectedType = null;
        if (types.includes(2) && this.password !== null) {
          selectedType = 2;
        } else if (types.includes(1)) {
          selectedType = 1;
        } else if (types.includes(2)) {
          throw new Error("VNC password authentication is required. Enter a password and reconnect.");
        } else {
          throw new Error(`Unsupported VNC security types: ${types.join(", ") || "none"}. This viewer currently supports only "None" and password-based VNC auth.`);
        }
        outgoing.push(Uint8Array.of(selectedType));
        events.push({ type: "security-selected", protocol: PROTOCOL, securityType: selectedType, label: selectedType === 2 ? "VNC Authentication" : "None" });
        this.state = selectedType === 2 ? "security-challenge" : "security-result";
        progressed = true;
        continue;
      }
      if (this.state === "security-type-33") {
        if (this.buffer.byteLength < 4)
          break;
        const view = new DataView(this.buffer.buffer, this.buffer.byteOffset, this.buffer.byteLength);
        const securityType = view.getUint32(0, false);
        this.consume(4);
        if (securityType === 0) {
          if (this.buffer.byteLength < 4)
            break;
          const reasonView = new DataView(this.buffer.buffer, this.buffer.byteOffset, this.buffer.byteLength);
          const reasonLength = reasonView.getUint32(0, false);
          if (this.buffer.byteLength < 4 + reasonLength)
            break;
          const reason = bytesToAscii(this.consume(4 + reasonLength).slice(4));
          throw new Error(reason || "VNC server rejected the connection.");
        }
        if (securityType === 2) {
          if (this.password === null) {
            throw new Error("VNC password authentication is required. Enter a password and reconnect.");
          }
          events.push({ type: "security-selected", protocol: PROTOCOL, securityType: 2, label: "VNC Authentication" });
          this.state = "security-challenge";
          progressed = true;
          continue;
        }
        if (securityType !== 1) {
          throw new Error(`Unsupported VNC security type ${securityType}. This viewer currently supports only "None" and password-based VNC auth.`);
        }
        events.push({ type: "security-selected", protocol: PROTOCOL, securityType: 1, label: "None" });
        outgoing.push(Uint8Array.of(this.shared ? 1 : 0));
        this.state = "server-init";
        progressed = true;
        continue;
      }
      if (this.state === "security-challenge") {
        if (this.buffer.byteLength < 16)
          break;
        if (this.password === null) {
          throw new Error("VNC password authentication is required. Enter a password and reconnect.");
        }
        const challenge = this.consume(16);
        outgoing.push(buildVncPasswordAuthResponse(this.password, challenge));
        this.state = "security-result";
        progressed = true;
        continue;
      }
      if (this.state === "security-result") {
        if (this.buffer.byteLength < 4)
          break;
        const view = new DataView(this.buffer.buffer, this.buffer.byteOffset, this.buffer.byteLength);
        const result = view.getUint32(0, false);
        this.consume(4);
        if (result !== 0) {
          if (this.buffer.byteLength >= 4) {
            const reasonLength = new DataView(this.buffer.buffer, this.buffer.byteOffset, this.buffer.byteLength).getUint32(0, false);
            if (this.buffer.byteLength >= 4 + reasonLength) {
              const reason = bytesToAscii(this.consume(4 + reasonLength).slice(4));
              throw new Error(reason || "VNC authentication failed.");
            }
          }
          throw new Error("VNC authentication failed.");
        }
        events.push({ type: "security-result", protocol: PROTOCOL, ok: true });
        outgoing.push(Uint8Array.of(this.shared ? 1 : 0));
        this.state = "server-init";
        progressed = true;
        continue;
      }
      if (this.state === "server-init") {
        if (this.buffer.byteLength < 24)
          break;
        const view = new DataView(this.buffer.buffer, this.buffer.byteOffset, this.buffer.byteLength);
        const width = view.getUint16(0, false);
        const height = view.getUint16(2, false);
        const pixelFormat = parsePixelFormat(view, 4);
        const nameLength = view.getUint32(20, false);
        if (this.buffer.byteLength < 24 + nameLength)
          break;
        const fixed = this.consume(24);
        const fixedView = new DataView(fixed.buffer, fixed.byteOffset, fixed.byteLength);
        this.framebufferWidth = fixedView.getUint16(0, false);
        this.framebufferHeight = fixedView.getUint16(2, false);
        this.serverPixelFormat = parsePixelFormat(fixedView, 4);
        this.serverName = bytesToAscii(this.consume(nameLength));
        this.state = "connected";
        if (this.pipeline) {
          this.pipeline.initFramebuffer(this.framebufferWidth, this.framebufferHeight);
        }
        outgoing.push(encodePixelFormat(this.clientPixelFormat));
        outgoing.push(buildSetEncodings(this.encodings));
        outgoing.push(buildFramebufferUpdateRequest(false, this.framebufferWidth, this.framebufferHeight));
        events.push({
          type: "display-init",
          protocol: PROTOCOL,
          width,
          height,
          name: this.serverName,
          pixelFormat
        });
        progressed = true;
        continue;
      }
      if (this.state === "connected") {
        if (this.buffer.byteLength < 1)
          break;
        const type = this.buffer[0];
        if (type === 0) {
          if (this.buffer.byteLength < 4)
            break;
          const headerView = new DataView(this.buffer.buffer, this.buffer.byteOffset, this.buffer.byteLength);
          const numberOfRectangles = headerView.getUint16(2, false);
          let offset = 4;
          const rects = [];
          let incomplete = false;
          const usePipeline = !!this.pipeline;
          for (let i2 = 0;i2 < numberOfRectangles; i2 += 1) {
            if (this.buffer.byteLength < offset + 12) {
              incomplete = true;
              break;
            }
            const rectView = new DataView(this.buffer.buffer, this.buffer.byteOffset + offset, 12);
            const x2 = rectView.getUint16(0, false);
            const y = rectView.getUint16(2, false);
            const width = rectView.getUint16(4, false);
            const height = rectView.getUint16(6, false);
            const encoding = rectView.getInt32(8, false);
            offset += 12;
            if (encoding === 0) {
              const bytesPerPixel = Math.max(1, Math.floor(Number(this.clientPixelFormat.bitsPerPixel || 0) / 8));
              const dataLength = width * height * bytesPerPixel;
              if (this.buffer.byteLength < offset + dataLength) {
                incomplete = true;
                break;
              }
              const raw = this.buffer.slice(offset, offset + dataLength);
              offset += dataLength;
              if (usePipeline) {
                this.pipeline.processRawRect(raw, x2, y, width, height, this.clientPixelFormat);
                rects.push({ kind: "pipeline", x: x2, y, width, height });
              } else {
                rects.push({
                  kind: "rgba",
                  x: x2,
                  y,
                  width,
                  height,
                  rgba: this.decodeRawRect(raw, width, height, this.clientPixelFormat)
                });
              }
              continue;
            }
            if (encoding === 2) {
              const rre = parseRreRect(this.buffer, offset, width, height, this.clientPixelFormat);
              if (!rre) {
                incomplete = true;
                break;
              }
              if (usePipeline) {
                const rreData = this.buffer.slice(offset, offset + rre.consumed);
                this.pipeline.processRreRect(rreData, x2, y, width, height, this.clientPixelFormat);
                rects.push({ kind: "pipeline", x: x2, y, width, height });
              } else {
                rects.push({ kind: "rgba", x: x2, y, width, height, rgba: rre.rgba });
              }
              offset += rre.consumed;
              continue;
            }
            if (encoding === 1) {
              if (this.buffer.byteLength < offset + 4) {
                incomplete = true;
                break;
              }
              const copyView = new DataView(this.buffer.buffer, this.buffer.byteOffset + offset, 4);
              const srcX = copyView.getUint16(0, false);
              const srcY = copyView.getUint16(2, false);
              offset += 4;
              if (usePipeline) {
                this.pipeline.processCopyRect(x2, y, width, height, srcX, srcY);
                rects.push({ kind: "pipeline", x: x2, y, width, height });
              } else {
                rects.push({ kind: "copy", x: x2, y, width, height, srcX, srcY });
              }
              continue;
            }
            if (encoding === 16) {
              const zrle = parseZrleRect(this.buffer, offset, width, height, this.clientPixelFormat, this.decodeRawRect, this.inflateZrle);
              if (!zrle) {
                incomplete = true;
                break;
              }
              offset += zrle.consumed;
              if (zrle.skipped)
                continue;
              if (usePipeline && zrle.decompressed) {
                this.pipeline.processZrleTileData(zrle.decompressed, x2, y, width, height, this.clientPixelFormat);
                rects.push({ kind: "pipeline", x: x2, y, width, height });
              } else {
                rects.push({ kind: "rgba", x: x2, y, width, height, rgba: zrle.rgba });
              }
              continue;
            }
            if (encoding === 5) {
              const hextile = parseHextileRect(this.buffer, offset, width, height, this.clientPixelFormat, this.decodeRawRect);
              if (!hextile) {
                incomplete = true;
                break;
              }
              if (usePipeline) {
                const hextileData = this.buffer.slice(offset, offset + hextile.consumed);
                this.pipeline.processHextileRect(hextileData, x2, y, width, height, this.clientPixelFormat);
                rects.push({ kind: "pipeline", x: x2, y, width, height });
              } else {
                rects.push({ kind: "rgba", x: x2, y, width, height, rgba: hextile.rgba });
              }
              offset += hextile.consumed;
              continue;
            }
            if (encoding === -223) {
              this.framebufferWidth = width;
              this.framebufferHeight = height;
              if (usePipeline) {
                this.pipeline.initFramebuffer(width, height);
              }
              rects.push({ kind: "resize", x: x2, y, width, height });
              continue;
            }
            throw new Error(`Unsupported VNC rectangle encoding ${encoding}. This viewer currently supports ZRLE, Hextile, RRE, CopyRect, raw rectangles, and DesktopSize only.`);
          }
          if (incomplete)
            break;
          this.consume(offset);
          const event = {
            type: "framebuffer-update",
            protocol: PROTOCOL,
            width: this.framebufferWidth,
            height: this.framebufferHeight,
            rects
          };
          if (usePipeline) {
            event.framebuffer = this.pipeline.getFramebuffer();
          }
          events.push(event);
          outgoing.push(buildFramebufferUpdateRequest(true, this.framebufferWidth, this.framebufferHeight));
          progressed = true;
          continue;
        }
        if (type === 2) {
          this.consume(1);
          events.push({ type: "bell", protocol: PROTOCOL });
          progressed = true;
          continue;
        }
        if (type === 3) {
          if (this.buffer.byteLength < 8)
            break;
          const view = new DataView(this.buffer.buffer, this.buffer.byteOffset, this.buffer.byteLength);
          const length = view.getUint32(4, false);
          if (this.buffer.byteLength < 8 + length)
            break;
          this.consume(8);
          const text = bytesToAscii(this.consume(length));
          events.push({ type: "clipboard", protocol: PROTOCOL, text });
          progressed = true;
          continue;
        }
        throw new Error(`Unsupported VNC server message type ${type}.`);
      }
    }
    return { events, outgoing };
  }
  consume(length) {
    const chunk = this.buffer.slice(0, length);
    this.buffer = this.buffer.slice(length);
    return chunk;
  }
}

// cmd/womprat/frontend/vnc-piclaw/vnc-input.ts
function clamp(value, min, max2) {
  return Math.max(min, Math.min(max2, value));
}
function encodeVncPointerEvent(buttonMask, x2, y) {
  const buffer = new Uint8Array(6);
  const safeX = clamp(Math.floor(Number(x2 || 0)), 0, 65535);
  const safeY = clamp(Math.floor(Number(y || 0)), 0, 65535);
  buffer[0] = 5;
  buffer[1] = clamp(Math.floor(Number(buttonMask || 0)), 0, 255);
  buffer[2] = safeX >> 8 & 255;
  buffer[3] = safeX & 255;
  buffer[4] = safeY >> 8 & 255;
  buffer[5] = safeY & 255;
  return buffer;
}
function vncButtonMaskForPointerButton(button) {
  switch (Number(button)) {
    case 0:
      return 1;
    case 1:
      return 2;
    case 2:
      return 4;
    default:
      return 0;
  }
}
function resolveVncPointerPressMask(event) {
  const direct = vncButtonMaskForPointerButton(event?.button);
  if (direct)
    return direct;
  const pointerType = String(event?.pointerType || "").toLowerCase();
  if (pointerType === "touch" || pointerType === "pen") {
    return vncButtonMaskForPointerButton(0);
  }
  const buttons = Number(event?.buttons || 0);
  if (buttons & 1)
    return vncButtonMaskForPointerButton(0);
  if (buttons & 4)
    return vncButtonMaskForPointerButton(1);
  if (buttons & 2)
    return vncButtonMaskForPointerButton(2);
  return 0;
}
function mapClientToFramebufferPoint(clientX, clientY, rect, framebufferWidth, framebufferHeight) {
  const width = Math.max(1, Math.floor(Number(framebufferWidth || 0)));
  const height = Math.max(1, Math.floor(Number(framebufferHeight || 0)));
  const rectWidth = Math.max(1, Number(rect?.width || 0));
  const rectHeight = Math.max(1, Number(rect?.height || 0));
  const relX = (Number(clientX || 0) - Number(rect?.left || 0)) / rectWidth;
  const relY = (Number(clientY || 0) - Number(rect?.top || 0)) / rectHeight;
  return {
    x: clamp(Math.floor(relX * width), 0, Math.max(0, width - 1)),
    y: clamp(Math.floor(relY * height), 0, Math.max(0, height - 1))
  };
}
function buildVncWheelPointerEvents(deltaY, x2, y, baseMask = 0) {
  const wheelBit = Number(deltaY) < 0 ? 8 : 16;
  const pressedMask = clamp(Number(baseMask || 0) | wheelBit, 0, 255);
  return [
    encodeVncPointerEvent(pressedMask, x2, y),
    encodeVncPointerEvent(Number(baseMask || 0), x2, y)
  ];
}
function encodeVncKeyEvent(down, keysym) {
  const buffer = new Uint8Array(8);
  const safeKeysym = Math.max(0, Math.min(4294967295, Number(keysym || 0) >>> 0));
  buffer[0] = 4;
  buffer[1] = down ? 1 : 0;
  buffer[4] = safeKeysym >>> 24 & 255;
  buffer[5] = safeKeysym >>> 16 & 255;
  buffer[6] = safeKeysym >>> 8 & 255;
  buffer[7] = safeKeysym & 255;
  return buffer;
}
var KEYSYM_BY_KEY = {
  Backspace: 65288,
  Tab: 65289,
  Enter: 65293,
  Escape: 65307,
  Insert: 65379,
  Delete: 65535,
  Home: 65360,
  End: 65367,
  PageUp: 65365,
  PageDown: 65366,
  ArrowLeft: 65361,
  ArrowUp: 65362,
  ArrowRight: 65363,
  ArrowDown: 65364,
  Shift: 65505,
  ShiftLeft: 65505,
  ShiftRight: 65506,
  Control: 65507,
  ControlLeft: 65507,
  ControlRight: 65508,
  Alt: 65513,
  AltLeft: 65513,
  AltRight: 65514,
  Meta: 65515,
  MetaLeft: 65515,
  MetaRight: 65516,
  Super: 65515,
  Super_L: 65515,
  Super_R: 65516,
  CapsLock: 65509,
  NumLock: 65407,
  ScrollLock: 65300,
  Pause: 65299,
  PrintScreen: 65377,
  ContextMenu: 65383,
  Menu: 65383,
  " ": 32
};
for (let i2 = 1;i2 <= 12; i2 += 1) {
  KEYSYM_BY_KEY[`F${i2}`] = 65470 + (i2 - 1);
}
function resolveVncKeysymFromKeyboardEvent(event) {
  const candidates = [event?.key, event?.code];
  for (const candidate of candidates) {
    if (candidate && Object.prototype.hasOwnProperty.call(KEYSYM_BY_KEY, candidate)) {
      return KEYSYM_BY_KEY[candidate];
    }
  }
  const key = String(event?.key || "");
  const keyCodePoint = key ? key.codePointAt(0) : null;
  const keyUnitLength = keyCodePoint == null ? 0 : keyCodePoint > 65535 ? 2 : 1;
  if (keyCodePoint != null && key.length === keyUnitLength) {
    if (keyCodePoint <= 255)
      return keyCodePoint;
    return (16777216 | keyCodePoint) >>> 0;
  }
  return null;
}

// cmd/womprat/frontend/vnc-src.ts
var textEncoder = new TextEncoder;
function clientCutText(text) {
  const payload = textEncoder.encode(String(text || ""));
  const out = new Uint8Array(8 + payload.length);
  out[0] = 6;
  out[4] = payload.length >>> 24 & 255;
  out[5] = payload.length >>> 16 & 255;
  out[6] = payload.length >>> 8 & 255;
  out[7] = payload.length & 255;
  out.set(payload, 8);
  return out;
}
function wsBase() {
  return window.location.protocol === "https:" ? "wss:" : "ws:";
}
function setStatus(root, text) {
  const el = root.querySelector("[data-vnc-status]");
  if (el)
    el.textContent = text;
}
function setBusy(root, busy) {
  root.toggleAttribute("data-busy", busy);
}
function query(root, selector) {
  const el = root.querySelector(selector);
  if (!el)
    throw new Error(`Missing VNC element: ${selector}`);
  return el;
}

class WompratVncViewer {
  root;
  target;
  password;
  protocol;
  ws = null;
  canvas;
  viewport;
  ctx;
  framebuffer = null;
  buttons = 0;
  activeKeys = new Set;
  constructor(root, target, password = null) {
    this.root = root;
    this.target = target;
    this.password = password;
    this.canvas = query(root, "canvas");
    this.viewport = query(root, "[data-vnc-viewport]");
    const ctx = this.canvas.getContext("2d", { alpha: false });
    if (!ctx)
      throw new Error("Canvas 2D context is unavailable.");
    this.ctx = ctx;
    this.installInput();
    this.installClipboard();
    this.installResize();
  }
  async init() {
    setStatus(this.root, "Loading VNC decoder…");
    const pipeline = await loadRemoteDisplayWasmDecoder();
    this.protocol = new VncRemoteDisplayProtocol({ shared: true, password: this.password, pipeline });
  }
  connect() {
    const url = new URL(`${wsBase()}//${window.location.host}/api/vnc/ws`);
    url.searchParams.set("target", this.target);
    setBusy(this.root, true);
    setStatus(this.root, `Connecting to ${this.target}…`);
    this.ws = new WebSocket(url.toString());
    this.ws.binaryType = "arraybuffer";
    this.ws.onopen = () => setStatus(this.root, `Negotiating VNC for ${this.target}…`);
    this.ws.onerror = () => setStatus(this.root, "VNC connection error.");
    this.ws.onclose = (event) => {
      setBusy(this.root, false);
      setStatus(this.root, event.reason ? `Disconnected: ${event.reason}` : "Disconnected.");
    };
    this.ws.onmessage = (event) => this.receive(new Uint8Array(event.data));
  }
  send(bytes) {
    if (this.ws?.readyState === WebSocket.OPEN)
      this.ws.send(bytes);
  }
  receive(bytes) {
    try {
      const result = this.protocol.receive(bytes);
      for (const out of result.outgoing || [])
        this.send(out);
      for (const event of result.events || [])
        this.handleEvent(event);
    } catch (error) {
      setBusy(this.root, false);
      setStatus(this.root, `VNC error: ${error?.message || error}`);
      try {
        this.ws?.close();
      } catch {}
    }
  }
  handleEvent(event) {
    switch (event.type) {
      case "protocol-version":
        setStatus(this.root, `RFB ${event.server} → ${event.client}`);
        break;
      case "security-selected":
        setStatus(this.root, `Security: ${event.label || event.securityType}`);
        break;
      case "security-result":
        setStatus(this.root, "Authenticated. Initializing display…");
        break;
      case "display-init":
        this.resizeFramebuffer(event.width, event.height);
        setBusy(this.root, false);
        setStatus(this.root, `${event.name || this.target} · ${event.width}×${event.height}`);
        break;
      case "framebuffer-update":
        this.drawFramebufferUpdate(event);
        break;
      case "clipboard": {
        const input = this.root.querySelector("[data-vnc-clipboard]");
        if (input)
          input.value = event.text || "";
        setStatus(this.root, "Remote clipboard updated.");
        break;
      }
      case "bell":
        setStatus(this.root, "Remote bell.");
        break;
    }
  }
  resizeFramebuffer(width, height) {
    const w = Math.max(1, Math.floor(Number(width || 0)));
    const h = Math.max(1, Math.floor(Number(height || 0)));
    this.canvas.width = w;
    this.canvas.height = h;
    this.framebuffer = this.ctx.createImageData(w, h);
    this.fitCanvas();
  }
  fitCanvas() {
    const w = Math.max(1, this.canvas.width || 1);
    const h = Math.max(1, this.canvas.height || 1);
    const bounds = this.viewport.getBoundingClientRect();
    const scale = Math.min(bounds.width / w, bounds.height / h, 1) || 1;
    this.canvas.style.width = `${Math.max(1, Math.floor(w * scale))}px`;
    this.canvas.style.height = `${Math.max(1, Math.floor(h * scale))}px`;
  }
  drawFramebufferUpdate(event) {
    if (event.framebuffer && event.width && event.height) {
      this.resizeFramebuffer(event.width, event.height);
      this.ctx.putImageData(new ImageData(new Uint8ClampedArray(event.framebuffer), event.width, event.height), 0, 0);
      return;
    }
    if (!this.framebuffer)
      this.resizeFramebuffer(event.width || this.canvas.width, event.height || this.canvas.height);
    if (!this.framebuffer)
      return;
    for (const rect of event.rects || []) {
      if (rect.kind === "resize") {
        this.resizeFramebuffer(rect.width, rect.height);
      } else if (rect.kind === "rgba" && rect.rgba) {
        this.blitRgba(rect.x, rect.y, rect.width, rect.height, rect.rgba);
      } else if (rect.kind === "copy") {
        const copy = this.ctx.getImageData(rect.srcX, rect.srcY, rect.width, rect.height);
        this.ctx.putImageData(copy, rect.x, rect.y);
        this.framebuffer = this.ctx.getImageData(0, 0, this.canvas.width, this.canvas.height);
      }
    }
    this.ctx.putImageData(this.framebuffer, 0, 0);
  }
  blitRgba(x2, y, width, height, rgba) {
    if (!this.framebuffer)
      return;
    const fb = this.framebuffer;
    for (let row = 0;row < height; row += 1) {
      const src = row * width * 4;
      const dst = ((y + row) * fb.width + x2) * 4;
      fb.data.set(rgba.subarray(src, src + width * 4), dst);
    }
  }
  point(event) {
    return mapClientToFramebufferPoint(event.clientX, event.clientY, this.canvas.getBoundingClientRect(), this.canvas.width, this.canvas.height);
  }
  installResize() {
    const ro = new ResizeObserver(() => this.fitCanvas());
    ro.observe(this.viewport);
  }
  installClipboard() {
    query(this.root, "[data-vnc-send-clipboard]").addEventListener("click", () => {
      const input = this.root.querySelector("[data-vnc-clipboard]");
      this.send(clientCutText(input?.value || ""));
      setStatus(this.root, "Clipboard sent to remote.");
    });
  }
  installInput() {
    this.canvas.tabIndex = 0;
    this.canvas.addEventListener("contextmenu", (event) => event.preventDefault());
    this.canvas.addEventListener("pointerdown", (event) => {
      this.canvas.focus();
      try {
        this.canvas.setPointerCapture(event.pointerId);
      } catch {}
      this.buttons |= resolveVncPointerPressMask(event);
      const p = this.point(event);
      this.send(encodeVncPointerEvent(this.buttons, p.x, p.y));
      event.preventDefault();
    });
    const release = (event) => {
      this.buttons &= ~resolveVncPointerPressMask(event);
      const p = this.point(event);
      this.send(encodeVncPointerEvent(this.buttons, p.x, p.y));
      event.preventDefault();
    };
    this.canvas.addEventListener("pointerup", release);
    this.canvas.addEventListener("pointercancel", release);
    this.canvas.addEventListener("pointermove", (event) => {
      const p = this.point(event);
      this.send(encodeVncPointerEvent(this.buttons, p.x, p.y));
    });
    this.canvas.addEventListener("wheel", (event) => {
      const p = this.point(event);
      for (const msg of buildVncWheelPointerEvents(event.deltaY, p.x, p.y, this.buttons))
        this.send(msg);
      event.preventDefault();
    }, { passive: false });
    this.canvas.addEventListener("keydown", (event) => {
      const keysym = resolveVncKeysymFromKeyboardEvent(event);
      if (!keysym)
        return;
      if (!event.repeat || !this.activeKeys.has(keysym)) {
        this.activeKeys.add(keysym);
        this.send(encodeVncKeyEvent(true, keysym));
      }
      event.preventDefault();
    });
    this.canvas.addEventListener("keyup", (event) => {
      const keysym = resolveVncKeysymFromKeyboardEvent(event);
      if (!keysym)
        return;
      this.activeKeys.delete(keysym);
      this.send(encodeVncKeyEvent(false, keysym));
      event.preventDefault();
    });
  }
}
async function startVNC(root, target) {
  const password = root.getAttribute("data-vnc-password") || null;
  const viewer = new WompratVncViewer(root, target, password);
  await viewer.init();
  viewer.connect();
}
export {
  startVNC
};
