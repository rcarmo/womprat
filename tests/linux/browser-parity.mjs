// AppImage preflight plus native acceptance orchestrator. Fedora runner checks
// the exact release image; functional fixtures use a separately test-tagged GUI
// and cannot enable direct dialing in the shipping binary.
import { spawnSync } from 'node:child_process';
import { statSync, openSync, readSync, closeSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
const args=process.argv.slice(2);const option=name=>args[args.indexOf(name)+1];
const image=resolve(option('--appimage')||'');
const profiles=resolve(option('--profiles')||'');
if(!args.includes('--require-release')||!args.includes('--appimage')||!args.includes('--profiles'))throw Error('Usage: browser-parity.mjs --appimage <path> --profiles <dir> --require-release');
const root=process.env.WOMPRAT_RUN_DIR;
if(!root||!image.startsWith(root+'/')||!profiles.startsWith(root+'/'))throw Error('AppImage and profiles must be run-owned');
if(!statSync(image).isFile()||statSync(image).size<1024*1024)throw Error('AppImage missing/truncated');
const header=Buffer.alloc(16);const fd=openSync(image,'r');try{readSync(fd,header,0,16,0)}finally{closeSync(fd)}
if(header.subarray(0,4).toString('hex')!=='7f454c46'||header[8]!==0x41||header[9]!==0x49||header[10]!==2)throw Error('Not a type-2 AppImage');
mkdirSync(profiles,{recursive:true});
const out=spawnSync('bash',['tests/linux/fedora-appimage.sh','--appimage',image,'--profiles',profiles,'--fedora-version','44','--preflight-only'],{stdio:'inherit',env:process.env});
if(out.status!==0)throw Error('AppImage Fedora dependency/release preflight failed');
console.log('PASS type-2 x86-64 AppImage preflight; native Fedora acceptance follows');
