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
