import {HousePreview} from './HousePreview';
import {useCallback,useEffect,useRef,useState} from 'react';
import {APIError,request,SignedOut} from './api';
import {exportDesign,newDesign,renderPattern,validateDesign} from './pattern';
import type {Design,SavedPattern} from './pattern';

export function StripPreview({design}:{design:Design}){
 const canvas=useRef<HTMLCanvasElement>(null);
 const [playing,setPlaying]=useState(false);
 const invalid=validateDesign(design);
 useEffect(()=>{
  if(invalid||design.effect==='native')return;
  const ctx=canvas.current?.getContext('2d');if(!ctx)return;
  let handle=0;const start=performance.now();
  const frame=(now:number)=>{
   const tick=playing&&design.effect==='chase'?Math.floor(now-start)%86400000:0;
   const colors=renderPattern(design,48,tick);
   ctx.clearRect(0,0,960,120);ctx.strokeStyle='#34404d';ctx.lineWidth=2;ctx.beginPath();ctx.moveTo(10,60);ctx.lineTo(950,60);ctx.stroke();
   colors.forEach((color,i)=>{const x=10+i*20;ctx.shadowBlur=12;ctx.shadowColor=color;ctx.fillStyle=color;ctx.beginPath();ctx.arc(x,60,6,0,Math.PI*2);ctx.fill();ctx.shadowBlur=0;ctx.strokeStyle='#637080';ctx.lineWidth=.5;ctx.stroke();});
   if(playing&&!document.hidden)handle=requestAnimationFrame(frame);
  };
  const resume=()=>{cancelAnimationFrame(handle);handle=requestAnimationFrame(frame)};
  handle=requestAnimationFrame(frame);document.addEventListener('visibilitychange',resume);
  return()=>{cancelAnimationFrame(handle);document.removeEventListener('visibilitychange',resume)};
 },[design,playing,invalid]);
 if(design.effect==='native')return <div className="native-preview"><strong>Native effect preserved</strong><p>Its captured parameters are saved. A color preview will be available once this effect has been decoded.</p></div>;
 return <div className="strip-preview"><canvas ref={canvas} width="960" height="120" role="img" aria-label={`Preview of ${design.name}, 48 logical bulbs`}/><div className="preview-caption"><span>App preview · hardware playback unverified</span>{design.effect==='chase'&&<button type="button" className="button secondary" disabled={!!invalid} onClick={()=>setPlaying(!playing)}>{playing?'Pause animation':'Play animation'}</button>}</div>{invalid&&<p className="field-error">{invalid}</p>}</div>;
}

function download(design:Design){
 const url=URL.createObjectURL(new Blob([exportDesign(design)],{type:'application/json'}));
 const a=document.createElement('a');a.href=url;a.download='trimlights-pattern.json';document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),1000);
}
type Reference={rule_id:string;name:string;enabled:boolean;action:{revision:number}};
function ReferenceNotice({references}:{references:Reference[]|null}){return <div className="notice">{references===null?'Checking calendar references…':references.length?<><strong>Used by your calendar</strong><ul>{references.map(r=><li key={r.rule_id}>{r.name} · revision {r.action.revision}{r.enabled?'':' · disabled'}</li>)}</ul><p>These rules keep their pinned versions after edits or archival. Update the calendar explicitly to use a new version. Existing scheduled use continues when archived.</p></>:'No current calendar references.'}</div>}
function copyName(name:string){return [...name].slice(0,73).join('')+' (copy)'}

export function PatternLibrary({csrf,onError}:{csrf:string;onError:(error:unknown)=>void}){
 const [patterns,setPatterns]=useState<SavedPattern[]>([]);
 const [loading,setLoading]=useState(true);
 const [search,setSearch]=useState('');
 const [archived,setArchived]=useState(false);
 const [draft,setDraft]=useState<Design|null>(null);
 const [editing,setEditing]=useState<SavedPattern|null>(null);
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState('');
 const [notice,setNotice]=useState('');
 const [conflict,setConflict]=useState(false);
 const [history,setHistory]=useState<SavedPattern[]|null>(null);
 const [archiveTarget,setArchiveTarget]=useState<SavedPattern|null>(null);
 const [references,setReferences]=useState<{id:string;values:Reference[]}|null>(null);
 const referenceID=archiveTarget?.id??editing?.id;
 const dialog=useRef<HTMLDialogElement>(null);
 const file=useRef<HTMLInputElement>(null);
 const loadSequence=useRef(0);
 const report=useCallback((e:unknown)=>{if(e instanceof SignedOut)onError(e);else setError(e instanceof Error?e.message:'Could not reach the pattern library.');},[onError]);
 const load=useCallback(async()=>{const sequence=++loadSequence.current;try{const result=await request<{patterns:SavedPattern[]}>('/api/v1/patterns?include_archived=1');if(sequence===loadSequence.current)setPatterns(result.patterns);}catch(e){report(e);}finally{setLoading(false);}},[report]);
 useEffect(()=>{void load();const timer=setInterval(()=>{if(!document.hidden)void load();},5000);return()=>{clearInterval(timer);loadSequence.current++};},[load]);
 useEffect(()=>{if(archiveTarget&&dialog.current){const el=dialog.current;el.showModal();return()=>el.close();}},[archiveTarget]);
 useEffect(()=>{if(!referenceID){setReferences(null);return;}let live=true;setReferences(null);const loadRefs=async()=>{try{const result=await request<{references:Reference[]}>(`/api/v1/patterns/${referenceID}/schedule-references`);if(live)setReferences({id:referenceID,values:result.references})}catch(e){if(live)report(e)}};void loadRefs();const timer=setInterval(()=>{if(!document.hidden)void loadRefs()},5000);return()=>{live=false;clearInterval(timer)}},[referenceID,report]);
 const currentReferences=references&&references.id===referenceID?references.values:null;
 const open=(p:SavedPattern|null,copy=false)=>{setEditing(copy?null:p);setDraft(p?structuredClone({...p.design,name:copy?copyName(p.design.name):p.design.name}):newDesign());setError('');setNotice('');setConflict(false);setHistory(null)};
 const update=(change:Partial<Design>)=>{setDraft(draft?{...draft,...change}:null);setNotice('');};
 const save=async(asNew=false)=>{
  if(!draft)return;const validation=validateDesign(draft);if(validation){setError(validation);return;}
  setBusy(true);setError('');setNotice('');
  try{const existing=editing&&!asNew;const saved=await request<SavedPattern>(existing?`/api/v1/patterns/${editing.id}`:'/api/v1/patterns',csrf,draft,existing?{method:'PUT',revision:editing.revision}:undefined);setEditing(saved);setDraft(structuredClone(saved.design));setConflict(false);setHistory(null);setNotice('Saved to your shared library.');await load();}
  catch(e){if(e instanceof APIError&&e.status===409)setConflict(true);report(e);}finally{setBusy(false);}
 };
 const restore=async(p:SavedPattern)=>{setBusy(true);setError('');setNotice('');try{await request(`/api/v1/patterns/${p.id}/restore`,csrf,{}, {revision:p.revision});setNotice('Pattern restored.');await load();}catch(e){report(e);}finally{setBusy(false)}};
 const confirmArchive=async()=>{if(!archiveTarget)return;setBusy(true);try{await request(`/api/v1/patterns/${archiveTarget.id}/archive`,csrf,{}, {revision:archiveTarget.revision});setArchiveTarget(null);setNotice('Pattern archived. You can restore it from Archived.');await load();}catch(e){setArchiveTarget(null);report(e);}finally{setBusy(false)}};
 const toggleFavorite=async(p:SavedPattern)=>{setBusy(true);setError('');setNotice('');try{await request(`/api/v1/patterns/${p.id}`,csrf,{...p.design,favorite:!p.design.favorite},{method:'PUT',revision:p.revision});await load();}catch(e){report(e);}finally{setBusy(false)}};
 const importController=async()=>{setBusy(true);setError('');setNotice('');try{const result=await request<{imported:SavedPattern[];skipped:number;snapshot_at:string}>('/api/v1/patterns/import-controller',csrf,{});setNotice(`Imported ${result.imported.length} native patterns; kept ${result.skipped} previously imported slots. Snapshot: ${new Date(result.snapshot_at).toLocaleString()}.`);await load();}catch(e){report(e);}finally{setBusy(false)}};
 const importFile=async(input:File)=>{if(input.size>32768){setError('Pattern files must be no larger than 32 KiB.');return;}try{const design=JSON.parse(await input.text()) as Design;const invalid=validateDesign(design);if(invalid)throw new Error(invalid);setEditing(null);setDraft(design);setHistory(null);setConflict(false);setError('');setNotice('Imported into the editor. Review it, then save to your library.');}catch(e){report(e)}};
 const showHistory=async()=>{if(!editing)return;try{const result=await request<{revisions:SavedPattern[]}>(`/api/v1/patterns/${editing.id}/revisions`);setHistory(result.revisions);}catch(e){report(e)}};
 const reloadDraft=async()=>{if(!editing)return;try{const latest=await request<SavedPattern>(`/api/v1/patterns/${editing.id}`);if(latest.archived){setError('This pattern was archived. Save your draft as a new pattern.');return;}open(latest);}catch(e){report(e)}};
 const filtered=patterns.filter(p=>p.archived===archived&&p.design.name.toLowerCase().includes(search.toLowerCase()));
 return <section className="library">
  <input hidden ref={file} type="file" accept="application/json,.json" aria-label="Import pattern file" onChange={e=>{const selected=e.target.files?.[0];e.target.value='';if(selected)void importFile(selected);}}/>
  {error&&<div role="alert" className="notice field-error">{error}</div>}
  {notice&&<div role="status" className="notice library-notice">{notice}</div>}
  {draft?<>
   <div className="editor-toolbar"><button type="button" className="button secondary" onClick={()=>{if(editing&&JSON.stringify(draft)===JSON.stringify(editing.design)){setDraft(null);return;}if(window.confirm('Leave the editor and discard unsaved changes?'))setDraft(null);}}>← Library</button><span className="muted">{editing?`Revision ${editing.revision}`:'Unsaved pattern'}</span><button className="button primary" disabled={busy||!!validateDesign(draft)||!!editing&&currentReferences===null} onClick={()=>void save()}>{busy?'Saving…':'Save pattern'}</button></div>
   {conflict&&<div className="conflict" role="status"><strong>A newer revision is saved.</strong><p>Your draft is still here. Save it as a separate pattern or discard it and load the latest version.</p><div className="action-row"><button className="button primary" disabled={busy} onClick={()=>void save(true)}>Save as new pattern</button><button className="button secondary" onClick={()=>void reloadDraft()}>Discard draft and load latest</button></div></div>}
   <div className="panel editor-panel">{editing&&<ReferenceNotice references={currentReferences}/>}<label className="field">Pattern name<input maxLength={80} value={draft.name} onChange={e=>update({name:e.target.value})}/></label><label className="check-label"><input type="checkbox" checked={draft.favorite} onChange={e=>update({favorite:e.target.checked})}/>Favorite</label><StripPreview design={draft}/><HousePreview design={draft}/>
   {draft.effect!=='native'?<><div className="effect-fields"><label className="field">Effect<select value={draft.effect} onChange={e=>update({effect:e.target.value as 'static'|'chase'})}><option value="static">Static palette</option><option value="chase">Chase preview</option></select></label><label className="field">Brightness · {draft.brightness}%<input type="range" min="0" max="100" value={draft.brightness} onChange={e=>update({brightness:Number(e.target.value)})}/></label><label className="field">Speed · {draft.speed}<input type="range" min="0" max="100" disabled={draft.effect==='static'} value={draft.speed} onChange={e=>update({speed:Number(e.target.value)})}/></label><label className="check-label"><input type="checkbox" checked={draft.reverse} onChange={e=>update({reverse:e.target.checked})}/>Reverse direction</label></div>
   <div className="section-heading"><h2>Color runs</h2><span>{draft.palette.reduce((n,r)=>n+r.count,0)} bulbs per repeat</span></div>
   <ol className="color-runs">{draft.palette.map((run,i)=><li key={i}><span className="run-number">{i+1}</span><label className="swatch-input"><span className="sr-only">Color {i+1}</span><input type="color" value={run.color} onChange={e=>update({palette:draft.palette.map((r,j)=>j===i?{...r,color:e.target.value}:r)})}/></label><label className="field run-count">Bulbs<input type="number" min="1" max="60" step="1" value={run.count} onChange={e=>update({palette:draft.palette.map((r,j)=>j===i?{...r,count:Number(e.target.value)}:r)})}/></label><button className="icon-button" aria-label={`Move color ${i+1} earlier`} disabled={i===0} onClick={()=>{const palette=[...draft.palette];[palette[i-1],palette[i]]=[palette[i],palette[i-1]];update({palette});}}>↑</button><button className="icon-button" aria-label={`Move color ${i+1} later`} disabled={i===draft.palette.length-1} onClick={()=>{const palette=[...draft.palette];[palette[i+1],palette[i]]=[palette[i],palette[i+1]];update({palette});}}>↓</button><button className="icon-button" aria-label={`Remove color ${i+1}`} disabled={draft.palette.length===1} onClick={()=>update({palette:draft.palette.filter((_,j)=>j!==i)})}>×</button></li>)}</ol><button className="button secondary" disabled={draft.palette.length>=30} onClick={()=>update({palette:[...draft.palette,{color:'#ffffff',count:1}]})}>+ Add color run</button>
   </>:<p className="footnote">This native reference retains controller slot {draft.native?.id} and its original parameters. Create a new pattern to design your own palette.</p>}
   <div className="editor-footer"><button className="button secondary" disabled={!!validateDesign(draft)} onClick={()=>download(draft)}>Export JSON</button>{editing&&<button className="button secondary" onClick={()=>void showHistory()}>Revision history</button>}<span className="muted">Saving updates the library. Playback is not enabled yet.</span></div>
   {history&&<ul className="revision-history">{history.map(p=><li key={p.revision}><span><strong>Revision {p.revision}</strong> · {new Date(p.updated).toLocaleString()} {p.archived?'· Archived':''}<small>{p.design.name}</small></span><button className="button secondary" onClick={()=>{setDraft(structuredClone(p.design));setNotice('Older revision loaded as a draft. Save to create a new revision.');}}>Use as draft</button></li>)}</ul>}
   </div>
  </>:<>
   <div className="library-toolbar"><button className="button primary" onClick={()=>open(null)}>+ New pattern</button><button className="button secondary" disabled={busy} onClick={()=>void importController()}>Import controller library</button><button className="button secondary" onClick={()=>file.current?.click()}>Import JSON</button></div>
   <div className="section-heading"><label className="search"><span className="sr-only">Find a pattern</span><input type="search" value={search} onChange={e=>setSearch(e.target.value)} placeholder="Find a pattern…"/></label><label className="check-label"><input type="checkbox" checked={archived} onChange={e=>setArchived(e.target.checked)}/>Archived</label></div>
   {filtered.length?<div className="pattern-grid">{filtered.map(p=><article key={p.id} className="panel pattern saved-pattern"><div className="row"><span className="eyebrow">{p.design.effect==='native'?'NATIVE REFERENCE':p.design.effect==='chase'?'CHASE DESIGN':'STATIC DESIGN'}</span><button className="icon-button favorite" aria-label={`${p.design.favorite?'Unfavorite':'Favorite'} ${p.design.name}`} aria-pressed={p.design.favorite} disabled={busy||p.archived} onClick={()=>void toggleFavorite(p)}>{p.design.favorite?'★':'☆'}</button></div><h2>{p.design.name}</h2>{p.design.effect!=='native'&&<div className="mini-palette" aria-hidden="true">{p.design.palette.map((r,i)=><span key={i} style={{backgroundColor:r.color,flexGrow:r.count}}/>)}</div>}<span className="muted">Revision {p.revision}{p.archived?' · Archived':''}</span><div className="card-actions">{p.archived?<button className="button secondary" disabled={busy} onClick={()=>void restore(p)}>Restore</button>:<button className="button secondary" onClick={()=>open(p)}>Edit</button>}<button className="button secondary" onClick={()=>open(p,true)}>Duplicate</button><button className="icon-button" aria-label={`Export ${p.design.name}`} onClick={()=>download(p.design)}>↓</button>{!p.archived&&<button className="icon-button" aria-label={`Archive ${p.design.name}`} disabled={busy} onClick={()=>setArchiveTarget(p)}>×</button>}</div></article>)}</div>:<section className="panel empty"><h2>{loading?'Loading your library…':archived?'No archived patterns':search?'No matching patterns':'Make it your kind of evening.'}</h2><p>{archived?'Archived patterns keep their revision history.':'Create a palette, import a design file, or preserve the patterns from your cached controller snapshot.'}</p></section>}
   <p className="footnote">Your shared library is stored on the homelab. Controller imports use the saved snapshot. Creating and previewing patterns sends no commands to the lights.</p>
  </>}
  {archiveTarget&&<dialog ref={dialog} className="panel modal" aria-labelledby="archive-title" onCancel={()=>setArchiveTarget(null)}><h2 id="archive-title">Archive {archiveTarget.design.name}?</h2><p>It will leave the active library. Its revisions remain available, and you can restore it from Archived.</p><ReferenceNotice references={currentReferences}/><div className="modal-actions"><button autoFocus className="button secondary" onClick={()=>setArchiveTarget(null)}>Cancel</button><button className="button primary" disabled={busy||currentReferences===null} onClick={()=>void confirmArchive()}>Archive pattern</button></div></dialog>}
 </section>;
}
