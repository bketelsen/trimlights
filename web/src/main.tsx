import { StrictMode, useCallback, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';
import {request,SignedOut,userMessage} from './api';
import {PatternLibrary} from './PatternLibrary';
import {Schedule} from './Schedule';
import {House} from './House';
import {clockTime,repeatLabel} from './uiText';
import {NativeControls,type NativeSnapshot} from './NativeControls';

type Identity = {subject:string;name:string;csrf_token:string;expires:string};
type NativeSchedule = {slot:number;enabled:boolean;pattern_name:string;repetition_interpretation:string;start_time:string;off_time:string};
type Status = {calendar_runtime?:{status:string;reason?:string};observation:{snapshot?:NativeSnapshot&{daily_schedules?:NativeSchedule[]};last_attempt?:string;last_error?:string;source:string};worker:{enabled:boolean;queued:number;active_operation?:string;persistence_error?:string};snapshot_age_seconds:number|null;capabilities:{snapshot:boolean;native_controls:boolean;reason:string}};
type Operation = {id:string;actor:string;started:string;finished:string;error?:string;control?:{request:{action:string;pattern_id?:number};outcome:string;transport_error?:string;calendar?:{calendar_revision:number;action:{kind:string;pattern_id?:string;revision?:number}}}};
type Page = 'home'|'patterns'|'schedule'|'house'|'settings';
function Logo(){return <svg viewBox="0 0 44 34" width="40" height="32" aria-hidden="true"><path d="M3 24 17 8l10 11 6-7 8 12" stroke="currentColor" strokeWidth="2" fill="none"/><circle cx="3" cy="24" r="2.5"/><circle cx="17" cy="8" r="2.5"/><circle cx="27" cy="19" r="2.5"/><circle cx="33" cy="12" r="2.5"/><circle cx="41" cy="24" r="2.5"/></svg>}
function timeLabel(value:string){return new Date(value).toLocaleString(undefined,{dateStyle:'medium',timeStyle:'short'});}
function age(seconds:number|null){if(seconds===null)return 'Not checked yet';if(seconds<60)return 'just now';if(seconds<3600)return `${Math.floor(seconds/60)} minutes ago`;if(seconds<86400)return `${Math.floor(seconds/3600)} hours ago`;return `${Math.floor(seconds/86400)} days ago`;}
function App(){
 const [identity,setIdentity]=useState<Identity|null|undefined>(undefined);
 const [status,setStatus]=useState<Status|null>(null);
 const [operations,setOperations]=useState<Operation[]>([]);
 const [page,setPage]=useState<Page>('home');
 const [scheduleDirty,setScheduleDirty]=useState(false);
 const [houseDirty,setHouseDirty]=useState(false);
 const [patternDirty,setPatternDirty]=useState(false);
 const [error,setError]=useState('');
 const [refreshing,setRefreshing]=useState(false);
 const [controlling,setControlling]=useState(false);
 const [refreshVersion,setRefreshVersion]=useState(0);
 const [refreshDialog,setRefreshDialog]=useState(false);
 const dialogRef=useRef<HTMLDialogElement>(null);
 useEffect(()=>{const dialog=dialogRef.current;if(refreshDialog&&dialog){dialog.showModal();return()=>dialog.close();}},[refreshDialog]);
 const report=useCallback((error:unknown)=>{if(error instanceof SignedOut){setIdentity(null);setStatus(null);setOperations([]);}else setError(userMessage(error,'Could not reach the app. Try again.'));},[]);
 const load=useCallback(async()=>{try{const result=await request<Status>('/api/v1/status');setStatus(result);setError('');}catch(error){report(error);}},[report]);
 useEffect(()=>{let live=true;request<Identity>('/api/v1/me').then(value=>{if(live)setIdentity(value);}).catch(error=>{if(live)report(error);});return()=>{live=false};},[report]);
 useEffect(()=>{if(!identity)return;
 // eslint-disable-next-line react-hooks/set-state-in-effect -- the first load starts with the effect; changing the load flow changes behaviour.
 void load();const timer=setInterval(()=>{if(!document.hidden)void load();},5000);return()=>clearInterval(timer);},[identity,load]);
 useEffect(()=>{if(page!=='settings'||!identity)return;request<{operations:Operation[]}>('/api/v1/operations').then(result=>setOperations(result.operations)).catch(report);},[page,identity,report]);
 async function refresh(){if(!identity)return;setRefreshing(true);setRefreshDialog(false);try{await request('/api/v1/controller/refresh',identity.csrf_token,{acknowledge_clock_sync:true});await load();setRefreshVersion(value=>value+1);}catch(error){report(error);}finally{setRefreshing(false);}}
 async function logout(){if(!identity)return;try{await request('/api/v1/logout',identity.csrf_token);setIdentity(null);setStatus(null);setOperations([]);}catch(error){report(error);}}
 const patterns=status?.observation.snapshot?.patterns??[];
 const schedules=status?.observation.snapshot?.daily_schedules??[];

 return <div className="app">
  <header><a className="brand" href="/" aria-label="Trimlights home"><Logo/><span>trimlights<span className="brand-dot">.</span></span></a><span className="household-label">OUR HOME · OUR LIGHTS</span>{identity&&<span className="avatar" title={identity.name}>{(identity.name||'H').slice(0,1)}</span>}</header>
  {error&&<div className="error-banner" role="alert">{error} <button onClick={()=>location.reload()}>Reconnect</button></div>}
  {identity===undefined?<main className="welcome"><span className="eyebrow">A LITTLE LIGHT, TOGETHER</span><h1>Welcome home.</h1><p>{error?'The app is unavailable. Try reconnecting.':'Opening your household…'}</p></main>:identity===null?<main className="welcome"><div className="welcome-mark"><Logo/></div><span className="eyebrow">A LITTLE LIGHT, TOGETHER</span><h1>Welcome home.</h1><p>One place for your lights, your favorite evenings,<br className="desktop"/> and the people you share them with.</p><a className="button primary" href="/auth/login">Sign in <span aria-hidden="true">↗</span></a><small>Private access for your household</small></main>:<>
  <nav aria-label="Main navigation">{(['home','patterns','schedule','house','settings'] as Page[]).map(item=><button key={item} aria-current={page===item?'page':undefined} onClick={()=>{if(item!==page&&(scheduleDirty||houseDirty||patternDirty)&&!window.confirm('Leave this page and discard unsaved changes?'))return;setPage(item);setError('');}}><span aria-hidden="true">{item==='home'?'⌂':item==='patterns'?'◉':item==='schedule'?'▦':item==='house'?'⌂':'⚙'}</span>{item==='home'?'Home':item==='patterns'?'Library':item==='schedule'?'Calendar':item==='house'?'House':'Settings'}</button>)}</nav>
  <main>
   <div className="page-heading"><div><span className="eyebrow">{page==='home'?'HOUSEHOLD OVERVIEW':page==='patterns'?'YOUR SHARED LIBRARY':page==='schedule'?'YOUR EVENINGS, PLANNED':page==='house'?'YOUR HOUSE, IN LIGHTS':'YOUR HOUSEHOLD'}</span><h1>{page==='home'?'Home lights':page==='patterns'?'Pattern library':page==='schedule'?'Your calendar':page==='house'?'House preview':'Settings'}</h1></div></div>
   {page==='home'&&<>
    <div className="home-update"><span className="muted">Last checked: {age(status?.snapshot_age_seconds??null)}</span><button className="button secondary" disabled={!status?.capabilities.snapshot||refreshing||controlling||!!status?.worker.active_operation} onClick={()=>setRefreshDialog(true)}>{refreshing?'Refreshing…':'Refresh'} <span aria-hidden="true">↻</span></button></div>
    {(status?.observation.last_error||status?.worker.persistence_error)&&<div className="notice" role="status">{status.worker.persistence_error?'The app can’t save changes right now. Please try again later.':'We couldn’t check the lights. Refresh to try again.'}</div>}
    <NativeControls refreshVersion={refreshVersion} snapshot={status?.observation.snapshot} available={status?.capabilities.native_controls??false} busy={refreshing||controlling||!!status?.worker.active_operation} csrf={identity.csrf_token} onBusy={setControlling} onComplete={load} onError={report}/>
    <div className="section-heading"><h2>Your light timers</h2><span>Saved on your lights</span></div>
    {schedules.length?<div className="schedule-grid">{schedules.map(schedule=><section className="panel schedule-card" key={schedule.slot}><div className="row"><span className="eyebrow">TIMER {schedule.slot}</span><span className={`status-pill ${schedule.enabled?'good':''}`}>{schedule.enabled?'On':'Off'}</span></div><h3>{schedule.pattern_name||'Unknown pattern'}</h3><p className="schedule-time">{clockTime(schedule.start_time)} <span>–</span> {clockTime(schedule.off_time)}</p><p className="muted">{repeatLabel(schedule.repetition_interpretation)}</p></section>)}</div>:<section className="panel empty"><h3>Your timers will appear here</h3><p>Refresh to see the times already set for your lights.</p></section>}
    <p className="footnote">Choose “Follow timers” above to use these times. Manage them in the Trimlight phone app.</p>
   </>}
   {page==='schedule'&&<Schedule csrf={identity.csrf_token} onError={report} onDirty={setScheduleDirty}/>}
   {page==='house'&&<House csrf={identity.csrf_token} onError={report} onDirty={setHouseDirty}/>}
   {page==='patterns'&&<PatternLibrary csrf={identity.csrf_token} onError={report} onDirty={setPatternDirty}/>}
   {page==='settings'&&<div className="settings-grid"><section className="panel settings"><h2>Your household</h2><dl><dt>Signed in as</dt><dd>{identity.name||'Household member'}</dd><dt>Lights</dt><dd>{status?.worker.enabled?'Set up':'Not connected'}</dd><dt>Calendar</dt><dd>Planning only</dd></dl><p className="muted">Your saved light timers are ready to use from Home. Automatic Calendar playback is coming soon.</p><button className="button secondary" onClick={()=>void logout()}>Sign out</button></section><section className="panel settings"><h2>Recent activity</h2>{operations.length?<ul className="history">{operations.map(op=><li key={op.id}><span className={op.error?'warn':'success'} aria-label={op.error?'Needs attention':op.control?.outcome==='pending'?'In progress':'Complete'}>{op.error?'!':op.control?.outcome==='pending'?'…':'✓'}</span><div><strong>{op.error?(op.control?'Couldn’t confirm the change':'Couldn’t refresh the lights'):op.control?.outcome==='pending'?'Updating the lights…':op.control?op.control.request.action==='play'?`Selected ${patterns.find(p=>p.id===op.control!.request.pattern_id)?.name||'a pattern'}`:op.control.request.action==='off'?'Lights turned off':'Following light timers':'Light details refreshed'}</strong><small>{timeLabel(op.finished)}</small>{op.error&&<p>Refresh from Home before trying again.</p>}</div></li>)}</ul>:<p className="muted">Your recent changes will appear here.</p>}</section></div>}

  </main>
  {refreshDialog&&<dialog ref={dialogRef} aria-labelledby="refresh-title" className="panel modal" onCancel={()=>setRefreshDialog(false)}><span className="eyebrow">CHECK YOUR LIGHTS</span><h2 id="refresh-title">Refresh light details?</h2><p>Close the Trimlight phone app first. Refreshing also keeps the lights’ clock up to date.</p><div className="modal-actions"><button
// eslint-disable-next-line jsx-a11y/no-autofocus -- the confirmation dialog focuses the safe Cancel action.
autoFocus className="button secondary" onClick={()=>setRefreshDialog(false)}>Cancel</button><button className="button primary" onClick={()=>void refresh()}>Refresh now</button></div></dialog>}
  </>}
  <footer><span>A brighter kind of evening.</span><span>Made for home</span></footer>
 </div>;
}

createRoot(document.getElementById('root')!).render(<StrictMode><App/></StrictMode>);
