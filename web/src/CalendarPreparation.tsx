import {useEffect,useRef,useState} from 'react';
import {request} from './api';
import {clockTime,repeatLabel} from './uiText';
import type {Action,Plan,Rule,Preview} from './Schedule';

type Source={snapshot_at:string;baseline_sha256?:string};
type Blocker={code:string;message:string;reference_id?:string};
type Readiness={execution_enabled:false;timezone:string;checked_at:string;until:string;schedule_revision:number;source:Source;blockers:Blocker[];bindings:{id:string;name:string;required:boolean;action:Action;status:string;message:string;slot?:number}[];current:Preview['segments'][number];next:Preview['segments'][number]|null;issues:Preview['issues']};
type NativeReport={execution_enabled:false;timezone:string;schedule_revision:number;source:Source;entries:{native:{slot:number;enabled:boolean;pattern_name:string;repetition_interpretation:string;start_time:string;off_time:string};reason:string;rule?:Rule}[]};

export function CalendarPreparation({plan,revision,latestRevision,csrf,busy,setBusy,onError,onAppend,describeAction}:{plan:Plan;revision:number;latestRevision:number;csrf:string;busy:boolean;setBusy:(value:boolean)=>void;onError:(e:unknown)=>void;onAppend:(rules:Rule[],reviewed:Plan,reviewedRevision:number)=>void;describeAction:(a:Action)=>string}) {
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
 const staleReadiness=readinessBase!==base||latestRevision!==revision;
 const staleNative=nativeBase!==copyBase||latestRevision!==revision;
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
 const sourceLabel=(source:Source,zone:string)=>source.baseline_sha256&&source.snapshot_at&&!source.snapshot_at.startsWith('0001-')?`Last checked ${format(source.snapshot_at,zone)}.`:'Refresh your lights on Home first.';
 const checkLabel=(code:string)=>({custom_effect:'Choose a pattern already saved on your lights.',changed_slot:'This pattern has changed on your lights. Add it to your library again.',unverified:'Refresh your lights on Home, then check again.',matched:'Saved on your lights.',off:'Lights off.',unsupported_snapshot:'Refresh your lights on Home first.',stale_snapshot:'Refresh your lights on Home for an up-to-date check.',observation_error:'Refresh your lights on Home, then check again.',calendar_conflict:'Some routines overlap. Review your week ahead and choose which should play.',calendar_adjustments:'Some times need attention. Review your week ahead.'}[code]??'Review this routine before using it.');
 const timerHelp=(reason:string)=>reason.startsWith('No current')?'Add this pattern from your lights in Library first.':reason.startsWith('Multiple')?'Choose a pattern for this timer in a new routine.':reason.includes('already in')||reason.includes('already used')?'Already included in your plan.':reason.includes('maximum')?'Your calendar has reached 100 routines.':'Add this timer by hand after checking its times in the Trimlight phone app.';
 const selectedRules=(native?.entries.flatMap(e=>e.rule&&chosen.includes(e.rule.id)?[e.rule]:[])??[]);
 return <details className="panel calendar-settings calendar-preparation">
  <summary>Bring over your light timers</summary>
  <p>Add the patterns from your lights in Library first, then choose the timers you’d like in your plan.</p>
  <label className="field">Date for one-day timers<input type="date" min="1970-01-01" max="2199-12-31" value={date} disabled={busy} onChange={e=>{setDate(e.target.value);setChosen([])}}/><small>Choose the date you want in {plan.timezone}.</small></label>
  <button className="button secondary" disabled={busy||!date} onClick={()=>void run('native-draft')}>{working?'Checking…':'Find my timers'}</button>
  {native&&<>
   <p className="footnote">{sourceLabel(native.source,native.timezone)}</p>
   {staleNative&&<p role="status" className="notice">Your plan or date changed. Find your timers again to continue.</p>}
   <ul className="native-timer-entries">{native.entries.map(e=><li key={e.native.slot}>
    <strong>Timer {e.native.slot} · {e.native.enabled?'On':'Off'}</strong>
    <p>{e.native.pattern_name||'Saved pattern'} · {clockTime(e.native.start_time)}–{clockTime(e.native.off_time)} · {repeatLabel(e.native.repetition_interpretation)}</p>
    {e.rule?<label className="check-label"><input type="checkbox" disabled={busy||staleNative} checked={chosen.includes(e.rule.id)} onChange={event=>setChosen(old=>event.target.checked?[...old,e.rule!.id]:old.filter(id=>id!==e.rule!.id))}/>Add timer {e.native.slot}{e.rule.kind==='exception'?` on ${e.rule.from}`:' every day'}{e.rule.enabled?'':' (paused)'}</label>:<p>{timerHelp(e.reason)}</p>}
   </li>)}</ul>
   <button className="button primary" disabled={busy||staleNative||!selectedRules.length||plan.rules.length+selectedRules.length>100} onClick={()=>{if(staleNative)return;onAppend(selectedRules,plan,revision);setChosen([])}}>Add to my plan</button>
   {plan.rules.length+selectedRules.length>100&&<p role="alert">A calendar can have up to 100 routines.</p>}
   <p className="footnote">Review your week ahead, then save your calendar.</p>
  </>}
  <details className="native-copy"><summary>Check planned patterns</summary>
   <p>See which patterns in your plan are already saved on your lights.</p>
   <button className="button secondary" disabled={busy} onClick={()=>void run('readiness')}>{working?'Checking…':'Check patterns'}</button>
   {readiness&&<div aria-live="polite">
    {staleReadiness&&<p className="notice">Your plan changed. Check again to include your latest edits.</p>}
    <p className="footnote">{sourceLabel(readiness.source,readiness.timezone)}</p>
    <ul className="calendar-issues">{readiness.blockers.filter(b=>!['sunday_clock_unverified','timer_handoff_unverified','runtime_unavailable'].includes(b.code)).map((b,i)=><li key={`${b.code}:${i}`}>{checkLabel(b.code)}</li>)}</ul>
    <p><strong>Planned now:</strong> {describeAction(readiness.current.action)}</p>
    <p><strong>Up next:</strong> {readiness.next?`${format(readiness.next.from,readiness.timezone)} · ${describeAction(readiness.next.action)}`:'No change this week.'}</p>
    <ul className="pattern-checks">{readiness.bindings.map((b,i)=><li key={`${b.id}:${i}`}><strong>{b.name}</strong>{b.required?'':' · paused'}<br/>{checkLabel(b.status)}</li>)}</ul>
    {!readiness.bindings.length&&<p>Add a routine to plan your first evening.</p>}
   </div>}
  </details>
 </details>
}
