import { StrictMode, useCallback, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';
import {request,SignedOut} from './api';
import {PatternLibrary} from './PatternLibrary';
import {Schedule} from './Schedule';
import {House} from './House';

type Identity = {subject:string;name:string;csrf_token:string;expires:string};
type Pattern = {id:number;name:string};
type NativeSchedule = {slot:number;enabled:boolean;pattern_name:string;repetition_interpretation:string;start_time:string;off_time:string};
type Status = {observation:{snapshot?:{queried_at:string;patterns:Pattern[];daily_schedules?:NativeSchedule[]};last_attempt?:string;last_error?:string;source:string};worker:{enabled:boolean;queued:number;active_operation?:string;persistence_error?:string};snapshot_age_seconds:number|null;capabilities:{snapshot:boolean;reason:string}};
type Operation = {id:string;actor:string;started:string;finished:string;error?:string};
type Page = 'home'|'patterns'|'schedule'|'house'|'settings';
function Logo(){return <svg viewBox="0 0 44 34" width="40" height="32" aria-hidden="true"><path d="M3 24 17 8l10 11 6-7 8 12" stroke="currentColor" strokeWidth="2" fill="none"/><circle cx="3" cy="24" r="2.5"/><circle cx="17" cy="8" r="2.5"/><circle cx="27" cy="19" r="2.5"/><circle cx="33" cy="12" r="2.5"/><circle cx="41" cy="24" r="2.5"/></svg>}
function timeLabel(value:string){return new Date(value).toLocaleString(undefined,{dateStyle:'medium',timeStyle:'short'});}
function age(seconds:number|null){if(seconds===null)return 'No snapshot yet';if(seconds<60)return 'Less than a minute ago';if(seconds<3600)return `${Math.floor(seconds/60)} minutes ago`;if(seconds<86400)return `${Math.floor(seconds/3600)} hours ago`;return `${Math.floor(seconds/86400)} days ago`;}
function App(){
 const [identity,setIdentity]=useState<Identity|null|undefined>(undefined);
 const [status,setStatus]=useState<Status|null>(null);
 const [operations,setOperations]=useState<Operation[]>([]);
 const [page,setPage]=useState<Page>('home');
 const [scheduleDirty,setScheduleDirty]=useState(false);
 const [houseDirty,setHouseDirty]=useState(false);
 const [error,setError]=useState('');
 const [refreshing,setRefreshing]=useState(false);
 const [refreshDialog,setRefreshDialog]=useState(false);
 const dialogRef=useRef<HTMLDialogElement>(null);
 useEffect(()=>{const dialog=dialogRef.current;if(refreshDialog&&dialog){dialog.showModal();return()=>dialog.close();}},[refreshDialog]);
 const report=useCallback((error:unknown)=>{if(error instanceof SignedOut){setIdentity(null);setStatus(null);setOperations([]);}else setError(error instanceof Error?error.message:'Could not reach the app.');},[]);
 const load=useCallback(async()=>{try{const result=await request<Status>('/api/v1/status');setStatus(result);setError('');}catch(error){report(error);}},[report]);
 useEffect(()=>{let live=true;request<Identity>('/api/v1/me').then(value=>{if(live)setIdentity(value);}).catch(error=>{if(live)report(error);});return()=>{live=false};},[report]);
 useEffect(()=>{if(!identity)return;void load();const timer=setInterval(()=>{if(!document.hidden)void load();},5000);return()=>clearInterval(timer);},[identity,load]);
 useEffect(()=>{if(page!=='settings'||!identity)return;request<{operations:Operation[]}>('/api/v1/operations').then(result=>setOperations(result.operations)).catch(report);},[page,identity,report]);
 async function refresh(){if(!identity)return;setRefreshing(true);setRefreshDialog(false);try{await request('/api/v1/controller/refresh',identity.csrf_token,{acknowledge_clock_sync:true});await load();}catch(error){report(error);}finally{setRefreshing(false);}}
 async function logout(){if(!identity)return;try{await request('/api/v1/logout',identity.csrf_token);setIdentity(null);setStatus(null);setOperations([]);}catch(error){report(error);}}
 const patterns=status?.observation.snapshot?.patterns??[];
 const schedules=status?.observation.snapshot?.daily_schedules??[];
 const connected=!!status?.observation.snapshot&&!status.observation.last_error;
 return <div className="app">
  <header><a className="brand" href="/" aria-label="Trimlights home"><Logo/><span>trimlights<span className="brand-dot">.</span></span></a><span className="household-label">OUR HOME · OUR LIGHTS</span>{identity&&<span className="avatar" title={identity.name}>{(identity.name||'H').slice(0,1)}</span>}</header>
  {error&&<div className="error-banner" role="alert">{error} <button onClick={()=>location.reload()}>Reconnect</button></div>}
  {identity===undefined?<main className="welcome"><span className="eyebrow">A LITTLE LIGHT, TOGETHER</span><h1>Welcome home.</h1><p>{error?'The app is unavailable. Try reconnecting.':'Opening your household…'}</p></main>:identity===null?<main className="welcome"><div className="welcome-mark"><Logo/></div><span className="eyebrow">A LITTLE LIGHT, TOGETHER</span><h1>Welcome home.</h1><p>One place for your lights, your favorite evenings,<br className="desktop"/> and the people you share them with.</p><a className="button primary" href="/auth/login">Sign in with Pocket ID <span aria-hidden="true">↗</span></a><small>Private access for your household</small></main>:<>
  <nav aria-label="Main navigation">{(['home','patterns','schedule','house','settings'] as Page[]).map(item=><button key={item} aria-current={page===item?'page':undefined} onClick={()=>{if(item!==page&&(scheduleDirty||houseDirty)&&!window.confirm('Leave this page and discard unsaved changes?'))return;setPage(item);setError('');}}><span aria-hidden="true">{item==='home'?'⌂':item==='patterns'?'◉':item==='schedule'?'▦':item==='house'?'⌂':'⚙'}</span>{item==='home'?'Home':item==='patterns'?'Library':item==='schedule'?'Calendar':item==='house'?'House':'Settings'}</button>)}</nav>
  <main>
   <div className="page-heading"><div><span className="eyebrow">{page==='home'?'HOUSEHOLD OVERVIEW':page==='patterns'?'YOUR SHARED LIBRARY':page==='schedule'?'YOUR EVENINGS, PLANNED':page==='house'?'YOUR HOUSE, IN LIGHTS':'YOUR HOUSEHOLD'}</span><h1>{page==='home'?'Home lights':page==='patterns'?'Pattern library':page==='schedule'?'Household calendar':page==='house'?'House preview':'Settings'}</h1></div><span className="version">EARLY PREVIEW</span></div>
   {page==='home'&&<>
    <section className="hero panel"><div><span className={`status-pill ${connected?'good':''}`}><i/>{status?(status.worker.enabled?(connected?'Snapshot available':'Awaiting controller'):'Controller access disabled'):'Loading status'}</span><h2>{connected?'Your library is here.':'Let’s get connected.'}</h2><p>{connected?`${patterns.length} saved patterns and ${schedules.length} decoded daily schedules.`:'Connect the controller to bring its library and existing schedules into view.'}</p><div className="hero-actions"><button className="button primary" disabled={!status?.capabilities.snapshot||refreshing} onClick={()=>setRefreshDialog(true)}>{refreshing?'Refreshing…':'Refresh controller'}<span aria-hidden="true">↻</span></button><span className="muted">{age(status?.snapshot_age_seconds??null)}</span></div></div><div className="roof-art" aria-hidden="true"><svg viewBox="0 0 360 220"><path d="M20 135 113 42l68 68 53-53 106 106M55 102v105h248v-80M152 207v-65h39v65"/><path className="lit" d="M20 135 113 42l68 68 53-53 106 106"/></svg><span>MAP YOUR ROOFLINE IN HOUSE</span></div></section>
    {(status?.observation.last_error||status?.worker.persistence_error)&&<div className="notice" role="status">Last refresh: {status.observation.last_error||status.worker.persistence_error}. The saved snapshot may be out of date.</div>}
    <div className="section-heading"><h2>Controller schedules</h2><span>Native timers</span></div>
    {schedules.length?<div className="schedule-grid">{schedules.map(schedule=><section className="panel schedule-card" key={schedule.slot}><div className="row"><span className="eyebrow">DAILY {schedule.slot}</span><span className={`status-pill ${schedule.enabled?'good':''}`}>{schedule.enabled?'Enabled':'Disabled'}</span></div><h3>{schedule.pattern_name||'Unknown pattern'}</h3><p className="schedule-time">{schedule.start_time} <span>→</span> {schedule.off_time}</p><p className="muted">{schedule.repetition_interpretation}</p></section>)}</div>:<section className="panel empty"><h3>No decoded schedules yet</h3><p>A refresh with the validated schedule layout will show the controller’s two daily timers.</p></section>}
    <p className="footnote">This preview displays saved controller state. Plan upcoming evenings in Calendar. Power, scene changes and calendar execution unlock after protocol verification.</p>
   </>}
   {page==='schedule'&&<Schedule csrf={identity.csrf_token} onError={report} onDirty={setScheduleDirty}/>}
   {page==='house'&&<House csrf={identity.csrf_token} onError={report} onDirty={setHouseDirty}/>}
   {page==='patterns'&&<PatternLibrary csrf={identity.csrf_token} onError={report}/>}
   {page==='settings'&&<div className="settings-grid"><section className="panel settings"><h2>Your session</h2><dl><dt>Signed in as</dt><dd>{identity.name||identity.subject}</dd><dt>Session expires</dt><dd>{timeLabel(identity.expires)}</dd><dt>Authentication</dt><dd>Pocket ID</dd><dt>Controller</dt><dd>{status?.worker.enabled?'Configured':'Disabled'}</dd></dl><button className="button secondary" onClick={()=>void logout()}>Sign out</button></section><section className="panel settings"><h2>Recent refreshes</h2>{operations.length?<ul className="history">{operations.map(op=><li key={op.id}><span className={op.error?'warn':'success'} aria-label={op.error?'Failed':'Successful'}>{op.error?'!':'✓'}</span><div><strong>{op.error?'Refresh failed':'Snapshot saved'}</strong><small>{timeLabel(op.finished)}</small>{op.error&&<p>{op.error}</p>}</div></li>)}</ul>:<p className="muted">No refreshes recorded yet.</p>}</section></div>}
  </main>
  {refreshDialog&&<dialog ref={dialogRef} aria-labelledby="refresh-title" className="panel modal" onCancel={()=>setRefreshDialog(false)}><span className="eyebrow">CONTROLLER REFRESH</span><h2 id="refresh-title">Update the saved snapshot?</h2><p>This request may synchronize the controller’s clock. Close the Trimlight phone app first so the two apps do not compete for a connection.</p><div className="modal-actions"><button autoFocus className="button secondary" onClick={()=>setRefreshDialog(false)}>Cancel</button><button className="button primary" onClick={()=>void refresh()}>Refresh now</button></div></dialog>}
  </>}
  <footer><span>A brighter kind of evening.</span><span>Trimlights · Homelab edition</span></footer>
 </div>;
}

createRoot(document.getElementById('root')!).render(<StrictMode><App/></StrictMode>);
