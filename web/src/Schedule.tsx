import {useCallback,useEffect,useRef,useState} from 'react';
import {APIError,request,SignedOut} from './api';
import type {SavedPattern} from './pattern';

type Action={kind:'off'|'pattern';pattern_id?:string;revision?:number};
type Boundary={kind:'clock'|'sunrise'|'sunset';time?:string;offset_minutes?:number};
type Rule={id:string;name:string;enabled:boolean;kind:'weekly'|'seasonal'|'exception';priority:number;from?:string;through?:string;weekdays:number[];start:Boundary;end:Boundary;end_next_day:boolean;action:Action};
type Plan={schema:1;timezone:string;location?:{latitude:number;longitude:number};rules:Rule[];override?:{id:string;action:Action;starts_at:string;expires_at:string}};
type Saved={revision:number;plan:Plan;actor:string;updated:string};
type Candidate={id:string;name:string;kind:string;priority:number;action:Action};
type Preview={execution_enabled:boolean;timezone:string;segments:{from:string;until:string;action:Action;conflict:boolean;reason:string;winners:Candidate[];shadowed:Candidate[]}[];issues:{rule_id:string;date:string;message:string}[]};
const weekdays=['Sun','Mon','Tue','Wed','Thu','Fri','Sat'];
function today(zone:string){try{return new Intl.DateTimeFormat('en-CA',{timeZone:zone,year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date());}catch{return new Date().toISOString().slice(0,10)}}
function actionKey(action:Action){return action.kind==='off'?'off':`${action.pattern_id}:${action.revision}`}
function actionFromKey(value:string):Action{if(value==='off')return {kind:'off'};const [pattern_id,revision]=value.split(':');return {kind:'pattern',pattern_id,revision:Number(revision)}}
function BoundaryEditor({title,value,onChange}:{title:string;value:Boundary;onChange:(value:Boundary)=>void}){
 return <fieldset className="boundary-editor"><legend>{title}</legend><label className="field">{title} basis<select value={value.kind} onChange={e=>onChange(e.target.value==='clock'?{kind:'clock',time:title==='Start'?'19:30':'22:59'}:{kind:e.target.value as 'sunrise'|'sunset',offset_minutes:0})}><option value="clock">Clock time</option><option value="sunset">Sunset</option><option value="sunrise">Sunrise</option></select></label>{value.kind==='clock'?<label className="field">{title} time<input type="time" required value={value.time??''} onChange={e=>onChange({...value,time:e.target.value})}/></label>:<label className="field">{title} offset (minutes)<input type="number" min="-180" max="180" step="1" value={value.offset_minutes??0} onChange={e=>onChange({...value,offset_minutes:Number(e.target.value)})}/><small>Negative is before; positive is after.</small></label>}</fieldset>
}
function ActionPicker({value,patterns,onChange,label='Pattern or off'}:{value:Action;patterns:SavedPattern[];onChange:(action:Action)=>void;label?:string}){
 const key=actionKey(value);const options=patterns.filter(p=>!p.archived);
 const missing=key!=='off'&&!options.some(p=>`${p.id}:${p.revision}`===key);
 return <label className="field">{label}<select value={key} onChange={e=>onChange(actionFromKey(e.target.value))}><option value="off">Lights off</option>{missing&&<option value={key}>{patterns.find(p=>p.id===value.pattern_id)?.design.name??'Retained pattern'} · pinned revision {value.revision}</option>}{options.map(p=><option key={p.id} value={`${p.id}:${p.revision}`}>{p.design.name} · revision {p.revision}</option>)}</select><small>Uses this exact version. Later library edits do not change it.</small></label>
}
export function Schedule({csrf,onError,onDirty}:{csrf:string;onError:(error:unknown)=>void;onDirty:(dirty:boolean)=>void}){
 const [saved,setSaved]=useState<Saved|null>(null);const [draft,setDraft]=useState<Plan|null>(null);
 const [patterns,setPatterns]=useState<SavedPattern[]>([]);const [selected,setSelected]=useState<string|null>(null);
 const [error,setError]=useState('');const [notice,setNotice]=useState('');const [busy,setBusy]=useState(false);
 const [conflict,setConflict]=useState(false);const [remoteRevision,setRemoteRevision]=useState(0);
 const [preview,setPreview]=useState<Preview|null>(null);const [previewPlan,setPreviewPlan]=useState('');
 const [fromDate,setFromDate]=useState('');const [days,setDays]=useState(7);const [previewRange,setPreviewRange]=useState('');
 const [history,setHistory]=useState<Saved[]|null>(null);const [overrideAction,setOverrideAction]=useState<Action>({kind:'off'});const [overrideHours,setOverrideHours]=useState(1);
 const mounted=useRef(true);const draftRef=useRef<Plan|null>(null);const savedRef=useRef<Saved|null>(null);const loadSequence=useRef(0);const previewSequence=useRef(0);
 const dirty=!!draft&&JSON.stringify(draft)!==JSON.stringify(saved?.plan);
 draftRef.current=draft;savedRef.current=saved;
 const report=useCallback((e:unknown)=>{if(e instanceof SignedOut)onError(e);else setError(e instanceof Error?e.message:'Could not reach the calendar.');},[onError]);
 useEffect(()=>{onDirty(dirty);return()=>onDirty(false)},[dirty,onDirty]);
 useEffect(()=>{if(!dirty)return;const warn=(e:BeforeUnloadEvent)=>e.preventDefault();window.addEventListener('beforeunload',warn);return()=>window.removeEventListener('beforeunload',warn)},[dirty]);
 const load=useCallback(async(force=false)=>{
  const sequence=++loadSequence.current;
  try{const [s,p]=await Promise.all([request<Saved>('/api/v1/schedule'),request<{patterns:SavedPattern[]}>('/api/v1/patterns?include_archived=1')]);
   if(!mounted.current||sequence!==loadSequence.current)return;
   if(savedRef.current&&s.revision<savedRef.current.revision)return;
   setPatterns(p.patterns);setRemoteRevision(s.revision);
   if(force||!savedRef.current||JSON.stringify(draftRef.current)===JSON.stringify(savedRef.current.plan)){
    if(force){setError('');setNotice('Loaded the latest saved calendar.')}
    setSaved(s);setDraft(structuredClone(s.plan));setFromDate(d=>d||today(s.plan.timezone));setConflict(false);
   }
  }catch(e){if(mounted.current)report(e)}
 },[report]);
 useEffect(()=>{mounted.current=true;void load();const timer=setInterval(()=>{if(!document.hidden)void load()},5000);return()=>{mounted.current=false;loadSequence.current++;previewSequence.current++;clearInterval(timer)}},[load]);
 const update=(change:Partial<Plan>)=>{if(draft)setDraft({...draft,...change});setNotice('')};
 const updateRule=(change:Partial<Rule>)=>{if(draft)update({rules:draft.rules.map(r=>r.id===selected?{...r,...change}:r)})};
 const addRule=()=>{if(!draft)return;const id=crypto.randomUUID();update({rules:[...draft.rules,{id,name:'Evening lights',enabled:true,kind:'weekly',priority:0,weekdays:[0,1,2,3,4,5,6],start:{kind:'clock',time:'19:30'},end:{kind:'clock',time:'22:59'},end_next_day:false,action:patterns.find(p=>!p.archived)?{kind:'pattern',pattern_id:patterns.find(p=>!p.archived)!.id,revision:patterns.find(p=>!p.archived)!.revision}:{kind:'off'}}]});setSelected(id);setError('')};
 const save=async()=>{if(!draft||!saved)return;setBusy(true);setError('');setNotice('');loadSequence.current++;
  try{const result=await request<Saved>('/api/v1/schedule',csrf,draft,{method:'PUT',revision:saved.revision});if(!mounted.current)return;setSaved(result);setDraft(structuredClone(result.plan));setRemoteRevision(result.revision);setConflict(false);setHistory(null);setNotice('Calendar saved for your household. Hardware execution is still disabled.');}
  catch(e){if(e instanceof APIError&&e.status===409)setConflict(true);report(e)}finally{setBusy(false)}
 };
 const runPreview=async()=>{if(!draft)return;const sequence=++previewSequence.current;setBusy(true);setError('');const plan=structuredClone(draft);const range=`${fromDate}:${days}`;
  try{const result=await request<Preview>('/api/v1/schedule/preview',csrf,{plan,from_date:fromDate,days});if(mounted.current&&sequence===previewSequence.current){setPreview(result);setPreviewPlan(JSON.stringify(plan));setPreviewRange(range)}}catch(e){report(e)}finally{if(mounted.current)setBusy(false)}
 };
 const showHistory=async()=>{try{const result=await request<{revisions:Saved[]}>('/api/v1/schedule/revisions');setHistory(result.revisions)}catch(e){report(e)}};
 const exportDraft=()=>{if(!draft)return;const blob=new Blob([JSON.stringify(draft,null,2)],{type:'application/json'});const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download='trimlights-calendar-draft.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)};
 const describeAction=(a:Action)=>a.kind==='off'?'Lights off':`${patterns.find(p=>p.id===a.pattern_id)?.design.name??'Saved pattern'} · revision ${a.revision}`;
 const dateTime=(s:string)=>new Date(s).toLocaleString(undefined,{timeZone:preview?.timezone??draft?.timezone,dateStyle:'medium',timeStyle:'short'});
 if(!draft||!saved)return <section className="panel empty"><h2>Loading your calendar…</h2>{error&&<p role="alert">{error}</p>}</section>;
 const rule=draft.rules.find(r=>r.id===selected);
 const stalePreview=previewPlan!==JSON.stringify(draft)||previewRange!==`${fromDate}:${days}`;
 return <section className="calendar">
  <div className="notice"><strong>Plan your evenings.</strong> Calendar saves and previews do not control the lights yet. The calendar has not taken over controller scheduling.</div>
  {error&&<div role="alert" className="notice field-error">{error}</div>}{notice&&<div role="status" className="notice">{notice}</div>}
  {(conflict||remoteRevision>saved.revision)&&<div className="conflict" role="status"><strong>A newer calendar is saved on another device.</strong><p>Your draft has been kept. Download it before loading the latest version if you want to keep your edits.</p><div className="action-row"><button className="button secondary" onClick={exportDraft}>Download draft</button><button className="button secondary" onClick={()=>{if(!dirty||window.confirm('Discard your calendar draft and load the latest saved version?')){setSelected(null);setPreview(null);void load(true)}}}>Discard draft and load latest</button></div></div>}
  <div className="editor-toolbar"><span className="muted">Revision {saved.revision} · {dirty?'Unsaved changes':'Saved calendar'}</span><button className="button secondary" disabled={busy} onClick={()=>void showHistory()}>History</button><button className="button primary" disabled={busy||!dirty} onClick={()=>void save()}>{busy?'Working…':'Save calendar'}</button></div>
  <fieldset disabled={busy} className="calendar-fields">
  <details className="panel calendar-settings"><summary>Timezone &amp; sunset location</summary><div className="calendar-grid"><label className="field">Household timezone<input value={draft.timezone} placeholder="America/New_York" onChange={e=>update({timezone:e.target.value})}/></label><label className="check-label"><input type="checkbox" checked={!!draft.location} onChange={e=>update({location:e.target.checked?{latitude:0,longitude:0}:undefined})}/>Use sunrise and sunset</label>{draft.location&&<><label className="field">Latitude<input type="number" min="-90" max="90" step="any" value={draft.location.latitude} onChange={e=>update({location:{...draft.location!,latitude:Number(e.target.value)}})}/></label><label className="field">Longitude<input type="number" min="-180" max="180" step="any" value={draft.location.longitude} onChange={e=>update({location:{...draft.location!,longitude:Number(e.target.value)}})}/></label><p className="footnote">Enter your home’s coordinates. Zero is a real location, not an automatic lookup. Solar calculations stay on the server.</p></>}</div></details>
  <div className="section-heading"><h2>Your rules</h2><button className="button primary" disabled={draft.rules.length>=100} onClick={addRule}>+ Add rule</button></div>
  <p className="muted">Temporary override → one-day exception → seasonal → weekly. Higher priority wins within each group.</p>
  <div className="calendar-rule-list">{draft.rules.map(r=><button className={`panel rule-summary ${r.id===selected?'selected':''}`} key={r.id} aria-pressed={r.id===selected} onClick={()=>setSelected(r.id)}><span><strong>{r.name||'Untitled rule'}</strong><small>{r.kind} · priority {r.priority} · {describeAction(r.action)}</small></span><span className="status-pill">{r.enabled?'Enabled':'Disabled'}</span></button>)}</div>
  {!draft.rules.length&&<section className="panel empty"><h3>Your evenings start here.</h3><p>Add a weekly routine, a seasonal range or a one-day exception, then preview the upcoming timeline.</p></section>}
  {rule&&<section className="panel calendar-editor"><div className="section-heading"><h2>Edit rule</h2><button className="button secondary" onClick={()=>setSelected(null)}>Close editor</button></div>
   <div className="calendar-grid"><label className="field">Rule name<input maxLength={80} value={rule.name} onChange={e=>updateRule({name:e.target.value})}/></label><label className="check-label"><input type="checkbox" checked={rule.enabled} onChange={e=>updateRule({enabled:e.target.checked})}/>Rule enabled</label>
   <label className="field">Recurrence<select value={rule.kind} onChange={e=>{const kind=e.target.value as Rule['kind'];const start=rule.from||today(draft.timezone);updateRule({kind,from:kind==='weekly'?undefined:start,through:kind==='weekly'?undefined:kind==='exception'?start:rule.through||start,weekdays:kind==='exception'?[]:[0,1,2,3,4,5,6]})}}><option value="weekly">Weekly routine</option><option value="seasonal">Seasonal date range</option><option value="exception">One-day exception</option></select></label><label className="field">Priority<input type="number" min="0" max="100" step="1" value={rule.priority} onChange={e=>updateRule({priority:Number(e.target.value)})}/></label>
   {rule.kind==='weekly'&&<label className="check-label"><input type="checkbox" checked={!!rule.from} onChange={e=>updateRule(e.target.checked?{from:today(draft.timezone),through:today(draft.timezone)}:{from:undefined,through:undefined})}/>Limit to a date range</label>}
   {(rule.kind!=='weekly'||rule.from)&&<><label className="field">{rule.kind==='exception'?'Date':'First start date'}<input type="date" value={rule.from??''} onChange={e=>updateRule({from:e.target.value,...(rule.kind==='exception'?{through:e.target.value}:{})})}/></label>{rule.kind!=='exception'&&<label className="field">Last start date (inclusive)<input type="date" value={rule.through??''} onChange={e=>updateRule({through:e.target.value})}/></label>}</>}
   </div>
   {rule.kind!=='exception'&&<fieldset className="weekday-picker"><legend>Start days</legend>{weekdays.map((day,i)=><label className="check-label" key={day}><input type="checkbox" checked={rule.weekdays.includes(i)} onChange={e=>updateRule({weekdays:e.target.checked?[...rule.weekdays,i].sort():rule.weekdays.filter(d=>d!==i)})}/>{day}</label>)}</fieldset>}
   <div className="calendar-grid"><BoundaryEditor title="Start" value={rule.start} onChange={start=>updateRule({start})}/><BoundaryEditor title="End" value={rule.end} onChange={end=>updateRule({end})}/></div>
   <label className="check-label"><input type="checkbox" checked={rule.end_next_day} onChange={e=>updateRule({end_next_day:e.target.checked})}/>End on the following day</label>
   <ActionPicker value={rule.action} patterns={patterns} onChange={action=>updateRule({action})}/>
   <div className="action-row"><button className="button secondary" onClick={()=>{const id=crypto.randomUUID();update({rules:[...draft.rules,{...structuredClone(rule),id,name:rule.name.slice(0,73)+' (copy)'}]});setSelected(id)}} disabled={draft.rules.length>=100}>Duplicate rule</button><button className="button secondary" onClick={()=>{if(window.confirm(`Remove ${rule.name} from this draft? Saved history is retained.`)){update({rules:draft.rules.filter(r=>r.id!==rule.id)});setSelected(null)}}}>Remove from draft</button></div>
  </section>}
  <details className="panel calendar-settings"><summary>Temporary override · preview only</summary><p>An override takes precedence until its expiry. Saving one still sends no hardware command.</p>{draft.override?<><p><strong>{describeAction(draft.override.action)}</strong><br/>Starts {new Date(draft.override.starts_at).toLocaleString()}<br/>Expires {new Date(draft.override.expires_at).toLocaleString()}{new Date(draft.override.expires_at)<new Date()?' · Expired':''}</p><button className="button secondary" onClick={()=>update({override:undefined})}>Resume calendar in draft</button></>:<div className="calendar-grid"><ActionPicker label="Override action" value={overrideAction} patterns={patterns} onChange={setOverrideAction}/><label className="field">Duration in hours<input type="number" min="0.25" max="168" step="0.25" value={overrideHours} onChange={e=>setOverrideHours(Number(e.target.value))}/></label><button className="button secondary" disabled={!Number.isFinite(overrideHours)||overrideHours<=0||overrideHours>168} onClick={()=>{const now=new Date();update({override:{id:crypto.randomUUID(),action:overrideAction,starts_at:now.toISOString(),expires_at:new Date(now.getTime()+overrideHours*3600000).toISOString()}})}}>Add override to draft</button></div>}</details>
  </fieldset>
  {history&&<section className="panel calendar-settings"><h2>Saved calendar history</h2><ul className="revision-history">{history.map(h=><li key={h.revision}><span>Revision {h.revision} · {new Date(h.updated).toLocaleString()}<small>{h.plan.rules.length} rules</small></span><button className="button secondary" disabled={busy} onClick={()=>{if(!dirty||window.confirm('Replace your unsaved draft with this earlier calendar?')){setDraft(structuredClone(h.plan));setSelected(null);setNotice('Earlier calendar loaded as a draft. Save to create a new revision.')}}}>Use as draft</button></li>)}</ul></section>}
  <section className="panel calendar-settings"><div className="section-heading"><h2>Upcoming timeline</h2><span>Draft preview</span></div><div className="calendar-grid"><label className="field">First day<input type="date" value={fromDate} onChange={e=>setFromDate(e.target.value)}/></label><label className="field">Preview length<select value={days} onChange={e=>setDays(Number(e.target.value))}><option value="1">One day</option><option value="7">One week</option><option value="14">Two weeks</option><option value="30">30 days</option></select></label><button className="button primary" disabled={busy||!fromDate} onClick={()=>void runPreview()}>Preview timeline</button></div>
   <p className="footnote">All timeline times use {preview?.timezone??draft.timezone}. Equal-priority conflicts propose off. Daylight-saving adjustments appear below.</p>
   {preview&&<>{stalePreview&&<div className="notice" role="status">This preview is out of date. Preview again to include your latest changes.</div>}{preview.issues.length>0&&<ul className="calendar-issues">{preview.issues.map((issue,i)=><li key={i}>{issue.date} · {draft.rules.find(r=>r.id===issue.rule_id)?.name??issue.rule_id}: {issue.message}</li>)}</ul>}<ol className={`timeline ${stalePreview?'stale':''}`}>{preview.segments.map((segment,i)=><li key={i} className={segment.conflict?'timeline-conflict':segment.action.kind==='off'?'timeline-off':'timeline-on'}><div><time dateTime={segment.from}>{dateTime(segment.from)}</time><span> → </span><time dateTime={segment.until}>{dateTime(segment.until)}</time></div><strong>{segment.conflict?'Conflict · proposed lights off':describeAction(segment.action)}</strong><p>{segment.winners.length?segment.winners.map(w=>w.name).join(' + '):'Default off'}</p><details><summary>Why this result?</summary><p>{segment.reason}</p>{segment.winners.map(w=><p key={w.id}>{w.name} · {w.kind} · priority {w.priority}</p>)}{segment.shadowed.length>0&&<p>Lower precedence: {segment.shadowed.map(w=>w.name).join(', ')}</p>}</details></li>)}</ol></>}
  </section>
 </section>
}
