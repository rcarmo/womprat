import {readFileSync,writeFileSync} from 'node:fs';
const path=process.argv[2];
if(!path)throw Error('WebKit library path required');
const data=readFileSync(path);
const before='/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1';
const after='/opt/womprat/usr/lib/webkit2gtk-4.1';
const needle=Buffer.from(before+'\0');
let offset=0,count=0;
for(;;){const index=data.indexOf(needle,offset);if(index<0)break;data.fill(0,index,index+before.length);data.write(after,index);offset=index+needle.length;count++}
if(count!==1)throw Error(`Expected exactly one WebKit process-directory string, found ${count}`);
writeFileSync(path,data);
