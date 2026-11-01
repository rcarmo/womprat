import {readFileSync,writeFileSync} from 'node:fs';
const path=process.argv[2];
if(!path)throw Error('WebKit library path required');
const data=readFileSync(path);
const before='/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1';
const after='/opt/womprat/usr/lib/webkit2gtk-4.1';
for(const suffix of ['', '/injected-bundle/']){
 const original=before+suffix,replacement=after+suffix;
 const needle=Buffer.from(original+'\0');let offset=0,matches=0;
 for(;;){const index=data.indexOf(needle,offset);if(index<0)break;data.fill(0,index,index+original.length);data.write(replacement,index);offset=index+needle.length;matches++}
 if(matches!==1)throw Error(`Expected exactly one WebKit ${suffix||'process'} directory string, found ${matches}`);
}
writeFileSync(path,data);
