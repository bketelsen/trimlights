// Top-level pages live in the URL hash so that reload keeps the page and the
// browser Back button (Android included) returns to the previous page. The
// service serves only "/", so the hash never reaches it.
export type Page='home'|'patterns'|'schedule'|'house'|'settings';
export const pages:readonly Page[]=['home','patterns','schedule','house','settings'];
const hashes:Record<Page,string>={home:'#home',patterns:'#library',schedule:'#calendar',house:'#house',settings:'#settings'};
const labels:Record<Page,string>={home:'Home',patterns:'Library',schedule:'Calendar',house:'House',settings:'Settings'};
export function pageHash(page:Page):string{return hashes[page];}
export function pageLabel(page:Page):string{return labels[page];}
export function pageFromHash(hash:string):Page{return pages.find(page=>hashes[page]===hash)??'home';}
export function pageTitle(page:Page|null):string{return page?`Trimlights · ${labels[page]}`:'Trimlights';}

// Each app history entry stores its position, so a refused page change can
// return to the exact entry it left without deleting any history.
const routeKey='trimlightsRoute';
export function routeState(index:number):Record<string,number>{return {[routeKey]:index};}
export function routeIndex(state:unknown):number|undefined{
 if(typeof state!=='object'||state===null)return undefined;
 const index=(state as Record<string,unknown>)[routeKey];
 return typeof index==='number'&&Number.isInteger(index)?index:undefined;
}
// restoring: after a refused change, the entry the app is returning to (to) and
// the refused entry (from; null for an unstamped, typed hash).
export type RouteModel={page:Page;index:number;restoring?:{to:number;from:number|null}};
// show: render this page. stamp: record this index on the current entry.
// go: history.go(delta) back to the entry that was left. replace: write the
// model's page and index over the current entry (same entry, nothing lost).
export type RouteStep={model:RouteModel;show?:Page;stamp?:number;go?:number;replace?:boolean};
// routeStep handles one popstate or hashchange. Browsers can fire both for one
// history move; the second event is a no-op. canLeave asks about unsaved edits.
export function routeStep(current:RouteModel,page:Page,index:number|undefined,canLeave:()=>boolean):RouteStep{
 let model=current;
 if(model.restoring){
  if(index===model.restoring.to)return {model:{page:model.page,index:model.restoring.to}};
  if((index??null)===model.restoring.from)return {model};
  model={page:model.page,index:model.index};
 }
 if(index===model.index){
  if(page===model.page)return {model};
  return canLeave()?{model:{page,index},show:page}:{model,replace:true};
 }
 const target=index??model.index+1,stamp=index===undefined?target:undefined;
 if(page===model.page)return {model:{page,index:target},stamp};
 if(canLeave())return {model:{page,index:target},show:page,stamp};
 return {model:{...model,restoring:{to:model.index,from:index??null}},go:model.index-target};
}
// routePush records a page change made inside the app (a navigation button).
// history.go is asynchronous: while a refusal is still returning, the browser is
// on the refused entry, so a push would delete entries. It returns null then.
export function routePush(current:RouteModel,page:Page):RouteModel|null{return current.restoring?null:{page,index:current.index+1};}
// routeSettle ends a restoration that has not landed after a short wait (for
// example, an out-of-range go). If the same restoration is still pending, the
// model takes the browser's actual index and the step replaces that entry with
// the shown page, so the URL matches the page again.
export const restoreTimeoutMs=1000;
export function routeSettle(current:RouteModel,pending:RouteModel['restoring'],actualIndex:number|undefined):RouteStep{
 if(!pending||current.restoring!==pending)return {model:current};
 return {model:{page:current.page,index:actualIndex??current.index},replace:true};
}
