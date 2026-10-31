// Compiles the components that src/components.test.mjs renders.
// Node strips types from .ts files but cannot load .tsx, so the project's own
// TypeScript compiler emits CommonJS (tsconfig.test.json) into the ignored
// .test-build directory. CommonJS lets Node resolve the extensionless imports.
import {execFileSync} from 'node:child_process';
import {rmSync,writeFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {dirname,join} from 'node:path';
import {fileURLToPath} from 'node:url';

const web=fileURLToPath(new URL('../',import.meta.url));
const out=join(web,'.test-build');
const tsc=join(dirname(createRequire(import.meta.url).resolve('typescript/package.json')),'bin','tsc');
rmSync(out,{recursive:true,force:true});
execFileSync(process.execPath,[tsc,'-p','tsconfig.test.json'],{cwd:web,stdio:'inherit'});
// package.json declares "type":"module"; mark the output as CommonJS.
writeFileSync(join(out,'package.json'),'{"type":"commonjs"}\n');
