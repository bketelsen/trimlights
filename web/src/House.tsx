import {useCallback,useEffect,useRef,useState} from 'react';
import {APIError,photoUploadMessage,request,SignedOut,userMessage} from './api';
import {HouseCanvas} from './HouseCanvas';
import {rotateView} from './house';
import type {Layout,Mapping,Photo,Run,SavedHouse,View} from './house';
import type {SavedPattern} from './pattern';

function download(layout:Layout){const url=URL.createObjectURL(new Blob([JSON.stringify(layout,null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download='house-layout-draft.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}
const clone=(l:Layout):Layout=>structuredClone(l);
export function House({csrf,onError,onDirty}:{csrf:string;onError:(e:unknown)=>void;onDirty:(dirty:boolean)=>void}){
 const [saved,setSaved]=useState<SavedHouse>();const [draft,setDraft]=useState<Layout>();const [photos,setPhotos]=useState<Photo[]>([]);const [patterns,setPatterns]=useState<SavedPattern[]>([]);
 const [photoID,setPhotoID]=useState('');const [runID,setRunID]=useState('');const [patternID,setPatternID]=useState('');const [mode,setMode]=useState<'draw'|'points'|'bulbs'|'pan'>('pan');
 const [message,setMessage]=useState('');const [busy,setBusy]=useState(false);const [conflict,setConflict]=useState(false);const [history,setHistory]=useState<SavedHouse[]>([]);const [pointIndex,setPointIndex]=useState(0);
 const past=useRef<Layout[]>([]),future=useRef<Layout[]>([]);const [undoCount,setUndoCount]=useState(0),[redoCount,setRedoCount]=useState(0);
 const loadSequence=useRef(0);const mounted=useRef(true);
 const draftRef=useRef(draft);draftRef.current=draft;const dirty=!!draft&&!!saved&&JSON.stringify(draft)!==JSON.stringify(saved.layout);const dirtyRef=useRef(dirty);dirtyRef.current=dirty;
 const load=useCallback(async(force=false)=>{const sequence=++loadSequence.current;try{const [h,p,patterns]=await Promise.all([request<SavedHouse>('/api/v1/house'),request<{photos:Photo[]}>('/api/v1/photos'),request<{patterns:SavedPattern[]}>('/api/v1/patterns')]);if(!mounted.current||sequence!==loadSequence.current)return;setPhotos(p.photos);setPatterns(patterns.patterns);if(force||!dirtyRef.current){setSaved(old=>old?.revision===h.revision?old:h);setDraft(old=>JSON.stringify(old)===JSON.stringify(h.layout)?old:h.layout);past.current=[];future.current=[];setUndoCount(0);setRedoCount(0);setConflict(false);}setPhotoID(id=>id||p.photos[0]?.id||'');}catch(e){onError(e)}},[onError]);
 useEffect(()=>{mounted.current=true;void load();const timer=setInterval(()=>{if(!document.hidden&&!dirtyRef.current)void load()},5000);return()=>{mounted.current=false;loadSequence.current++;clearInterval(timer)}},[load]);
 useEffect(()=>{onDirty(dirty)},[dirty,onDirty]);useEffect(()=>()=>onDirty(false),[onDirty]);
 useEffect(()=>{if(!dirty)return;const before=(e:BeforeUnloadEvent)=>e.preventDefault();window.addEventListener('beforeunload',before);return()=>window.removeEventListener('beforeunload',before)},[dirty]);
 function checkpoint(){if(!draftRef.current)return;past.current=[...past.current.slice(-49),clone(draftRef.current)];future.current=[];setUndoCount(past.current.length);setRedoCount(0);}
 function change(fn:(next:Layout)=>void,record=true){if(!draftRef.current)return;if(record)checkpoint();const next=clone(draftRef.current);fn(next);draftRef.current=next;dirtyRef.current=true;setDraft(next);setMessage('');}
 function undo(redo=false){if(!draftRef.current)return;const from=redo?future:past,to=redo?past:future;const next=from.current.pop();if(!next)return;to.current.push(clone(draftRef.current));draftRef.current=next;setDraft(next);setUndoCount(past.current.length);setRedoCount(future.current.length);}
 const photo=photos.find(p=>p.id===photoID),view=draft?.views.find(v=>v.photo_id===photoID)??{photo_id:photoID,turns:0,mappings:[]};const run=draft?.runs.find(r=>r.id===runID);const mapping=view.mappings.find(m=>m.run_id===runID);const pattern=patterns.find(p=>p.id===patternID);
 function viewChange(fn:(view:View)=>void,record=true){change(next=>{let v=next.views.find(v=>v.photo_id===photoID);if(!v){v={photo_id:photoID,turns:0,mappings:[]};next.views.push(v)}fn(v)},record);}
 function mappingChange(fn:(mapping:Mapping)=>void,record=true){viewChange(v=>{let m=v.mappings.find(m=>m.run_id===runID);if(!m){m={run_id:runID,points:[],reverse:false,adjustments:[]};v.mappings.push(m)}fn(m)},record);}
 function addRun(){if(!draft)return;const id=crypto.randomUUID();const nextStart=draft.runs.filter(r=>r.output==='output-1').reduce((v,r)=>Math.max(v,r.start+r.count),1);change(l=>l.runs.push({id,name:`Roofline ${l.runs.length+1}`,output:'output-1',start:nextStart,count:20}));setRunID(id);setMode('draw');setPointIndex(0);}
 function updateRun(fields:Partial<Run>){change(l=>{const r=l.runs.find(r=>r.id===runID);if(r)Object.assign(r,fields)});}
 async function upload(file:File){setBusy(true);setMessage('');try{if(file.size>12*1024*1024)throw new Error('Photo exceeds 12 MiB. Export a smaller JPEG.');const p=await request<Photo>('/api/v1/photos',csrf,undefined,{raw:file});setPhotos(old=>[...old,p]);setPhotoID(p.id);setMode('pan');setMessage('Photo uploaded. Add a roofline, then draw its corners.');}catch(e){if(e instanceof SignedOut)onError(e);else setMessage(photoUploadMessage(e))}finally{setBusy(false)}}
 async function importLayout(file:File){
  setBusy(true);setMessage('');loadSequence.current++;
  try{
   if(file.size>512*1024)throw new Error('Layout files must be no larger than 512 KiB.');
   const input:unknown=JSON.parse(await file.text());
   const validated=await request<{layout:Layout}>('/api/v1/house/validate',csrf,input);
   if(!mounted.current)return;
   if(dirtyRef.current&&!window.confirm('Replace the current unsaved layout with this imported draft? You can undo this change.'))return;
   change(l=>{Object.assign(l,validated.layout)});
   setPhotoID(validated.layout.views[0]?.photo_id??photos[0]?.id??'');
   setRunID(validated.layout.runs[0]?.id??'');setPointIndex(0);setMode('pan');
   setMessage('Layout imported as a draft. Review the photo alignment, then save to share it. Photos are not included in layout files.');
  }catch(e){if(e instanceof SignedOut)onError(e);else setMessage(userMessage(e,'Could not open this layout. Choose a layout downloaded from this app.'));}finally{if(mounted.current)setBusy(false)}
 }
 async function save(){if(!draft||!saved)return;const submitted=JSON.stringify(draft);loadSequence.current++;setBusy(true);try{const result=await request<SavedHouse>('/api/v1/house',csrf,draft,{method:'PUT',revision:saved.revision});if(!mounted.current)return;setSaved(result);if(JSON.stringify(draftRef.current)===submitted){setDraft(result.layout);dirtyRef.current=false;past.current=[];future.current=[];setUndoCount(0);setRedoCount(0);}setConflict(false);setMessage('House saved.');}catch(e){if(e instanceof SignedOut)onError(e);else{setConflict(e instanceof APIError&&e.status===409);setMessage(userMessage(e,'Could not save your house. Please try again.'))}}finally{setBusy(false)}}
 if(!draft)return <p>Opening house photos…</p>;
 return <>
  <p className="muted">Add a photo, trace your roofline, and try your favorite colors.</p>
  <div className="house-toolbar"><label className="button secondary house-upload">{busy?'Working…':'Upload house photo'}<input type="file" accept="image/jpeg,image/png,image/webp" disabled={busy||photos.length>=20} onChange={e=>{const file=e.target.files?.[0];e.target.value='';if(file)void upload(file)}}/></label>{photo&&<button className="button primary" disabled={draft.runs.length>=40} onClick={addRun}>Add roofline</button>}<span className="muted">{photos.length}/20 photos · JPEG, PNG, WebP · up to 12 MB</span></div>
  {message&&<p className={conflict?'notice':'house-message'} role="status">{message}</p>}
  {conflict&&<div className="house-toolbar"><button className="button secondary" onClick={()=>download(draft)}>Download my draft</button><button className="button secondary" onClick={()=>{if(window.confirm('Discard this draft and load the saved house?'))void load(true)}}>Reload saved house</button></div>}
  {!!photos.length&&<div className="house-photos" aria-label="Photo views">{photos.map((p,i)=><button key={p.id} className={p.id===photoID?'selected':''} aria-pressed={p.id===photoID} onClick={()=>{setPhotoID(p.id);setMode('pan');setPointIndex(0)}}><img src={`/api/v1/photos/${p.id}/thumbnail`} alt={`House view ${i+1}`}/><span>View {i+1}</span></button>)}</div>}
  {photo?<div className="house-workspace">
   <section className="panel house-controls"><h2>Rooflines</h2><label>Roofline<select value={runID} onChange={e=>{setRunID(e.target.value);setPointIndex(0);setMode('points')}}><option value="">Choose a roofline</option>{draft.runs.map(r=><option key={r.id} value={r.id}>{r.name}</option>)}</select></label>
    {run&&<>
     <label>Name<input maxLength={80} value={run.name} onChange={e=>updateRun({name:e.target.value})}/></label>
     <label>Bulb count<input type="number" min="1" max="2048" value={run.count} onChange={e=>{const count=Number(e.target.value);change(l=>{l.runs.find(r=>r.id===runID)!.count=count;for(const v of l.views)for(const m of v.mappings)if(m.run_id===runID)m.adjustments=m.adjustments.filter(a=>a.index<count)})}}/></label>
     <details className="roofline-setup"><summary>Light setup</summary>     <div className="house-pair"><label>Output<input value={run.output} maxLength={64} onChange={e=>updateRun({output:e.target.value})}/></label><label>First bulb<input type="number" min="1" max="2048" value={run.start} onChange={e=>updateRun({start:Number(e.target.value)})}/></label></div>
     <label>Match another roofline<select value={run.mirror_of??''} onChange={e=>updateRun({mirror_of:e.target.value||undefined})}><option value="">Use its own colors</option>{draft.runs.filter(r=>r.id!==runID&&!r.mirror_of).map(r=><option key={r.id} value={r.id}>{r.name} ({r.count} bulbs)</option>)}</select></label>
     <p className="muted">Use separate rooflines for gaps. Matching rooflines share colors and need the same number of bulbs.</p>
</details>
     <label><input type="checkbox" disabled={!mapping} checked={mapping?.reverse??false} onChange={e=>mappingChange(m=>{m.reverse=e.target.checked})}/> Reverse placement in this photo</label>
     <button className="button secondary" disabled={!mapping?.adjustments.length} onClick={()=>mappingChange(m=>{m.adjustments=[]})}>Reset individual bulbs</button>
     <button className="button secondary" disabled={!mapping} onClick={()=>viewChange(v=>{v.mappings=v.mappings.filter(m=>m.run_id!==runID)})}>Remove from this view</button>
     <button className="button secondary" onClick={()=>{if(draft.runs.some(r=>r.mirror_of===runID)){setMessage('Update the rooflines that match this one before removing it.');return;}if(window.confirm('Remove this roofline from every photo?')){change(l=>{l.runs=l.runs.filter(r=>r.id!==runID);for(const v of l.views)v.mappings=v.mappings.filter(m=>m.run_id!==runID)});setRunID('')}}}>Remove roofline from all photos</button>
    </>}
   </section>
   <section className="panel house-stage">
    <div className="house-toolbar" aria-label="Roofline tools">{(['draw','points','bulbs','pan'] as const).map(tool=><button className={`button ${mode===tool?'primary':'secondary'}`} key={tool} disabled={tool!=='pan'&&!run} aria-pressed={mode===tool} onClick={()=>setMode(tool)}>{tool==='draw'?'Draw':tool==='points'?'Move corners':tool==='bulbs'?'Move bulbs':'Move photo'}</button>)}</div>
    <p className="muted">{mode==='draw'?'Tap each roofline corner in order. Add at least two, then choose Move corners.':mode==='points'?'Drag a white corner to adjust the roofline.':mode==='bulbs'?'Drag an individual bulb to correct perspective or spacing.':'Drag to move the photo. Use + to zoom in for finer placement.'}</p>
    <HouseCanvas photo={photo} view={view} layout={draft} design={pattern?.design} selectedRun={runID} mode={mode} onPoint={p=>{if(run)mappingChange(m=>{if(m.points.length<100)m.points.push(p)})}} onStart={checkpoint} onSelect={setPointIndex} onMove={(index,p,bulb)=>mappingChange(m=>{if(bulb){m.adjustments=m.adjustments.filter(a=>a.index!==index);m.adjustments.push({index,point:p})}else m.points[index]=p},false)}/>
    <div className="house-toolbar"><button className="button secondary" disabled={!run||(mapping?.points.length??0)>=100} onClick={()=>mappingChange(m=>{m.points.push({x:.5,y:.5});setPointIndex(m.points.length-1)})}>Add corner at center</button><button className="button secondary" disabled={!undoCount} onClick={()=>undo()}>Undo</button><button className="button secondary" disabled={!redoCount} onClick={()=>undo(true)}>Redo</button><button className="button secondary" onClick={()=>viewChange(v=>Object.assign(v,rotateView(v)))}>Rotate photo ↻</button></div>
    {mapping&&mapping.points.length>0&&<details className="corner-details"><summary>Fine-tune corners</summary><div className="house-coordinates"><label>Corner<select value={Math.min(pointIndex,mapping.points.length-1)} onChange={e=>setPointIndex(Number(e.target.value))}>{mapping.points.map((_,i)=><option key={i} value={i}>{i+1}</option>)}</select></label>{(['x','y'] as const).map(axis=><label key={axis}>{axis.toUpperCase()} %<input type="number" min="0" max="100" step="0.1" value={Math.round(mapping.points[Math.min(pointIndex,mapping.points.length-1)][axis]*1000)/10} onChange={e=>mappingChange(m=>{m.points[Math.min(pointIndex,m.points.length-1)][axis]=Math.max(0,Math.min(1,Number(e.target.value)/100))})}/></label>)}<button className="button secondary" onClick={()=>mappingChange(m=>{m.points.splice(Math.min(pointIndex,m.points.length-1),1)})}>Remove corner</button></div></details>}
    <label>Preview pattern<select value={patternID} onChange={e=>setPatternID(e.target.value)}><option value="">Roofline only</option>{patterns.map(p=><option key={p.id} value={p.id}>{p.design.name}{p.design.effect==='native'?' (preview unavailable)':''}</option>)}</select></label>
   </section>
  </div>:<section className="panel empty"><h2>Your house, in lights</h2><p>Start with a straight-on photo of the roofline. Add another view for the side of the house; both can use the same physical bulbs.</p></section>}
  <div className="house-toolbar house-save"><button className="button primary" disabled={!dirty||busy} onClick={()=>void save()}>Save house</button><span className="muted">{dirty?'Unsaved changes':'Saved'}</span><button className="button secondary" onClick={()=>download(draft)}>Download layout</button><label className="button secondary house-upload">Upload layout<input type="file" accept="application/json,.json" disabled={busy} onChange={e=>{const file=e.target.files?.[0];e.target.value='';if(file)void importLayout(file)}}/></label><button className="button secondary" onClick={()=>request<{revisions:SavedHouse[]}>('/api/v1/house/revisions').then(r=>setHistory(r.revisions)).catch(onError)}>History</button></div>
  {!!history.length&&<section className="panel house-history"><h2>Saved versions</h2>{history.map(h=><div className="row" key={h.revision}><span>{new Date(h.updated).toLocaleString()}</span><button className="button secondary" onClick={()=>{if(!dirty||window.confirm('Replace your changes with this saved version?'))change(l=>Object.assign(l,clone(h.layout)))}}>Use as draft</button></div>)}</section>}
 </>;
}
