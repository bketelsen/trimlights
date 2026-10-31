export type ColorRun = {color:string;count:number};
export type NativePattern = {id:number;name:string;parameters_hex:string;snapshot_at:string};
export type Design = {schema:1;name:string;favorite:boolean;effect:'static'|'chase'|'native';brightness:number;speed:number;reverse:boolean;palette:ColorRun[];native?:NativePattern};
export type SavedPattern = {id:string;revision:number;design:Design;actor:string;updated:string;archived:boolean};

// Shared household name rule (internal/pattern ValidName): no outer white space,
// control (Cc) or format (Cf) characters, except zero-width joiner and non-joiner,
// and no lone surrogate (Go needs valid UTF-8 and would store U+FFFD instead).
// Both implementations must pass name-vectors.json.
export function validName(name:unknown):boolean{return typeof name==='string'&&name!==''&&name===name.trim()&&[...name].length<=80&&!/(?![\u200C\u200D])[\p{Cc}\p{Cf}\p{Cs}]/u.test(name);}
export function validateDesign(d:Design):string|null {
 if(!d||d.schema!==1)return 'Choose a pattern file downloaded from this app.';
 if(!validName(d.name))return 'Give your pattern a name of up to 80 characters, with no spaces at the beginning or end and no hidden characters.';
 if(typeof d.favorite!=='boolean'||typeof d.reverse!=='boolean')return 'This pattern file has invalid settings.';
 if(!Array.isArray(d.palette))return 'This pattern file is missing its colors.';
 if(d.effect==='native'){
  if(!d.native||d.palette.length||d.brightness!==100||d.speed!==0||d.reverse)return 'Only the name and favorite can be changed for this saved pattern.';
  if(!Number.isInteger(d.native.id)||d.native.id<0||d.native.id>255||typeof d.native.name!=='string'||[...d.native.name].length>80||!/^[0-9a-fA-F]{312}$/.test(d.native.parameters_hex)||!Number.isFinite(Date.parse(d.native.snapshot_at)))return 'This saved pattern is incomplete. Add it from your lights again.';
  return null;
 }
 if(d.native||!['static','chase'].includes(d.effect))return 'Choose a supported effect.';
 if(!Number.isInteger(d.brightness)||d.brightness<0||d.brightness>100||!Number.isInteger(d.speed)||d.speed<0||d.speed>100)return 'Brightness and speed must be whole numbers from 0 to 100.';
 if(d.palette.length<1||d.palette.length>30)return 'Choose between 1 and 30 colors.';
 for(const run of d.palette){if(!run||typeof run.color!=='string'||!/^#[0-9a-fA-F]{6}$/.test(run.color)||!Number.isInteger(run.count)||run.count<1||run.count>60)return 'Choose a color and between 1 and 60 bulbs for each group.';}
 return null;
}
export const notPatternFile='This file is not a valid pattern file. Choose a pattern file downloaded from this app.';
// parsePatternFile reads an imported pattern file. Text that is not JSON, or JSON that
// is not a schema 1 design, gets one clear message; a design with bad fields gets
// the validation message for that field.
export function parsePatternFile(text:string):Design {
 let value:unknown;
 try{value=JSON.parse(text);}catch{throw new Error(notPatternFile);}
 if(!value||typeof value!=='object'||Array.isArray(value)||(value as {schema?:unknown}).schema!==1)throw new Error(notPatternFile);
 const invalid=validateDesign(value as Design);if(invalid)throw new Error(invalid);
 return value as Design;
}
export function renderPattern(design:Design,count:number,tickMS:number):string[]{
 const error=validateDesign(design);if(error)throw new Error(error);
 if(design.effect==='native')throw new Error('A preview is not available for this saved pattern yet.');
 if(!Number.isInteger(count)||count<1||count>2048||!Number.isInteger(tickMS)||tickMS<0||tickMS>86400000)throw new Error('Preview count or time outside supported range.');
 const base:string[]=[];
 for(const run of design.palette){
  const color='#'+[1,3,5].map(offset=>Math.floor((parseInt(run.color.slice(offset,offset+2),16)*design.brightness+50)/100).toString(16).padStart(2,'0')).join('');
  for(let i=0;i<run.count;i++)base.push(color);
 }
 const shift=design.effect==='chase'&&design.speed>0?Math.floor(tickMS*design.speed/10000):0;
 return Array.from({length:count},(_,i)=>{const index=(design.reverse?base.length-1-i:i)-shift;return base[((index%base.length)+base.length)%base.length]});
}
export function newDesign():Design{return {schema:1,name:'New pattern',favorite:false,effect:'static',brightness:70,speed:25,reverse:false,palette:[{color:'#ffb05a',count:3},{color:'#000000',count:1}]};}
export function exportDesign(design:Design):string{return JSON.stringify(design,null,2)+'\n';}
