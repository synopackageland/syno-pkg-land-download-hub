// Store text comes only from a schema 4 catalog (SYNOPKGLAND_CATALOG), which
// lives outside this repository. Tests that load the manifest get a
// placeholder catalog for the current SPK version, so they check the package
// itself rather than the real store copy.
// Import this module before anything that reads the manifest.
import {mkdtempSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import YAML from 'yaml';

const LOCALES=['en','zh-TW','zh-CN','ja','ko','de','fr','es','it'];
const m=YAML.parse(readFileSync(new URL('../dsm-package.yaml',import.meta.url),'utf8'));
const spkVersion=`${m.version}-${String(m.build).padStart(4,'0')}`;
const text=(value:unknown)=>Object.fromEntries(LOCALES.map(locale=>[locale,value]));
const catalog={schema:4,packages:[{
 pins:[{package:m.package}],
 metadata:{description:text('Test description'),changelog:{releases:{[spkVersion]:text(['Test note'])}}},
}]};
const dir=mkdtempSync(join(tmpdir(),'download-hub-catalog-'));
const path=join(dir,'catalog.yaml');
writeFileSync(path,YAML.stringify(catalog));
process.env.SYNOPKGLAND_CATALOG=path;
process.on('exit',()=>rmSync(dir,{recursive:true,force:true}));
