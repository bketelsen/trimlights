import {useEffect,useRef,useState} from 'react';
import {request} from './api';
import type {Action,Plan,Rule,Preview} from './Schedule';

type Source={snapshot_at:string;baseline_sha256?:string};
type Blocker={code:string;message:string;reference_id?:string};
type Readiness={execution_enabled:false;timezone:string;checked_at:string;until:string;schedule_revision:number;source:Source;blockers:Blocker[];bindings:{id:string;name:string;required:boolean;action:Action;status:string;message:string;slot?:number}[];current:Preview['segments'][number];next:Preview['segments'][number]|null;issues:Preview['issues']};
type NativeReport={execution_enabled:false;timezone:string;schedule_revision:number;source:Source;entries:{native:{slot:number;enabled:boolean;pattern_name:string;repetition_interpretation:string;start_time:string;off_time:string};reason:string;rule?:Rule}[]};

export function CalendarPreparation({plan,revision,csrf,busy,setBusy,onError,onAppend,describeAction}:{plan:Plan;revision:number;csrf:string;busy:boolean;setBusy:(value:boolean)=>void;onError:(e:unknown)=>void;onAppend:(rules:Rule[],reviewed:Plan,reviewedRevision:number)=>void;describeAction:(a:Action)=>string}) {
 const [readiness,setReadiness]=useState<Readiness|null>(null);
 const [readinessBase,setReadinessBase]=useState('');
 const [native,setNative]=useState<NativeReport|null>(null);
 const [nativeBase,setNativeBase]=useState('');
 const [date,setDate]=useState('');
 const [chosen,setChosen]=useState<string[]>([]);
 const [working,setWorking]=useState(false);
 const sending=useRef(false);const mounted=useRef(true);
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[]);
 const base=JSON.stringify({plan,revision});
 const copyBase=JSON.stringify({plan,revision,date});
 const staleReadiness=readinessBase!==base;
 const staleNative=nativeBase!==copyBase;
 const run=async(kind:'readiness'|'native-draft')=>{
  if(sending.current||busy)return;
  sending.current=true;setWorking(true);setBusy(true);
  const submitted=structuredClone(plan);const submittedRevision=revision;const submittedDate=date;
  try {
   if(kind==='readiness'){
    const result=await request<Readiness>('/api/v1/schedule/readiness',csrf,{plan:submitted},{revision:submittedRevision});
    if(mounted.current){setReadiness(result);setReadinessBase(JSON.stringify({plan:submitted,revision:submittedRevision}))}
   }else{
    const result=await request<NativeReport>('/api/v1/schedule/native-draft',csrf,{plan:submitted,today_only_date:submittedDate},{revision:submittedRevision});
    if(mounted.current){setNative(result);setNativeBase(JSON.stringify({plan:submitted,revision:submittedRevision,date:submittedDate}));setChosen([])}
   }
  }catch(e){if(mounted.current)onError(e)}finally{sending.current=false;if(mounted.current){setWorking(false);setBusy(false)}}
 };
 const format=(instant:string,zone=plan.timezone)=>{try{return new Date(instant).toLocaleString(undefined,{timeZone:zone,dateStyle:'medium',timeStyle:'short'})}catch{return instant}};
 const sourceLabel=(source:Source,zone:string)=>source.baseline_sha256&&source.snapshot_at&&!source.snapshot_at.startsWith('0001-')?`Cached controller state from ${format(source.snapshot_at,zone)}. Checks do not refresh the controller.`:'No dated controller snapshot is available. Refresh on Home first.';
 const selectedRules=(native?.entries.flatMap(e=>e.rule&&chosen.includes(e.rule.id)?[e.rule]:[])??[]);
 return <section className="panel calendar-settings calendar-preparation">
  <div className="section-heading"><h2>Prepare Calendar control</h2><span className="status-pill">Not running</span></div>
  <p>Check whether this draft can use the patterns already saved on your controller. These checks and copies leave the lights and controller timers unchanged.</p>
  <button className="button secondary" disabled={busy} onClick={()=>void run('readiness')}>{working?'Checking…':'Check this draft'}</button>
  {readiness&&<div aria-live="polite">
   {staleReadiness&&<p className="notice">This report is out of date. Check again after your changes.</p>}
   <p className="footnote">{sourceLabel(readiness.source,readiness.timezone)}</p>
   <h3>Before automatic scheduling</h3>
   <ul className="calendar-issues">{readiness.blockers.map((b,i)=><li key={`${b.code}:${i}`}>{b.message}</li>)}</ul>
   <p><strong>Current proposed intent:</strong> {describeAction(readiness.current.action)}. {readiness.current.reason}</p>
   <p><strong>Next timeline interval:</strong> {readiness.next?`${format(readiness.next.from,readiness.timezone)} · ${describeAction(readiness.next.action)}`:'No change in the checked interval.'}</p>
   <p className="footnote">Timeline checked from {format(readiness.checked_at,readiness.timezone)} through {format(readiness.until,readiness.timezone)} ({readiness.timezone}). Later conflicts may still exist. A server outage can leave the controller in its last state; no automatic fallback is verified.</p>
   <details><summary>Pattern checks ({readiness.bindings.length})</summary><ul className="calendar-issues">{readiness.bindings.map((b,i)=><li key={`${b.id}:${i}`}><strong>{b.name}</strong>{b.required?'':' · disabled or expired'}{b.slot?` · controller slot ${b.slot}`:''}<br/>{b.message}</li>)}</ul>{!readiness.bindings.length&&<p>No rules or overrides. The proposed intent is OFF.</p>}</details>
   {readiness.issues.length>0&&<details><summary>Skipped or adjusted times</summary><ul className="calendar-issues">{readiness.issues.map((issue,i)=><li key={i}>{issue.date} · {plan.rules.find(r=>r.id===issue.rule_id)?.name??issue.rule_id}: {issue.message}</li>)}</ul></details>}
  </div>}
  <details className="native-copy"><summary>Copy controller timers into this draft</summary>
   <p>First import the saved controller patterns in Library. Then review the two cached timers here and choose which rules to append. Your existing draft stays intact. Clock times are copied as shown and will use this calendar’s timezone.</p>
   <label className="field">Date to assign to “today only” entries<input type="date" min="1970-01-01" max="2199-12-31" value={date} disabled={busy} onChange={e=>{setDate(e.target.value);setChosen([])}}/><small>Use a date in {plan.timezone}. The controller snapshot does not tell us the original date.</small></label>
   <button className="button secondary" disabled={busy||!date} onClick={()=>void run('native-draft')}>Review controller timers</button>
   {native&&<>
    <p className="footnote">{sourceLabel(native.source,native.timezone)} Copying does not disable native timers.</p>
    {staleNative&&<p role="status" className="notice">The draft, saved revision or date changed. Review controller timers again before adding rules.</p>}
    <ul className="native-timer-entries">{native.entries.map(e=><li key={e.native.slot}>
     <strong>Timer {e.native.slot} · {e.native.enabled?'Enabled':'Disabled'}</strong>
     <p>{e.native.pattern_name||'Unknown slot'} · {e.native.start_time}–{e.native.off_time} · {e.native.repetition_interpretation}</p>
     <p>{e.reason}</p>
     {e.rule&&<label className="check-label"><input type="checkbox" disabled={busy||staleNative} checked={chosen.includes(e.rule.id)} onChange={event=>setChosen(old=>event.target.checked?[...old,e.rule!.id]:old.filter(id=>id!==e.rule!.id))}/>Add {e.rule.name}{e.rule.kind==='exception'?` on ${e.rule.from}`:' every day'}{e.rule.enabled?'':' (disabled)'}</label>}
    </li>)}</ul>
    <button className="button primary" disabled={busy||staleNative||!selectedRules.length||plan.rules.length+selectedRules.length>100} onClick={()=>{if(staleNative)return;onAppend(selectedRules,plan,revision);setChosen([])}}>Add selected rules to draft</button>
    {plan.rules.length+selectedRules.length>100&&<p role="alert">A calendar supports at most 100 rules.</p>}
    <p className="footnote">Review the combined timeline and priorities, then Save calendar. Saving still does not enable execution.</p>
   </>}
  </details>
 </section>
}
