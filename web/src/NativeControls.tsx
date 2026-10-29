import {useEffect,useRef,useState} from 'react';
import {request,SignedOut} from './api';

export type NativeSnapshot={queried_at:string;stable_sha256:string;patterns:{id:number;name:string}[];playback?:{mode:string;matching_pattern_ids:number[]}};
type Action='off'|'play'|'timer';

export function NativeControls({snapshot,available,reason,busy,csrf,refreshVersion,onBusy,onComplete,onError}:{snapshot?:NativeSnapshot;available:boolean;reason:string;busy:boolean;csrf:string;refreshVersion:number;onBusy:(busy:boolean)=>void;onComplete:()=>Promise<void>;onError:(error:unknown)=>void}){
 const [selected,setSelected]=useState('');
 const [acknowledged,setAcknowledged]=useState(false);
 const [error,setError]=useState('');
 const [notice,setNotice]=useState('');
 const sending=useRef(false);
 useEffect(()=>{setError('');},[refreshVersion]);
 const patterns=snapshot?.patterns??[];
 const selectedPattern=patterns.find(p=>p.id===Number(selected));
 const mode=snapshot?.playback?.mode??'unknown';
 const matches=patterns.filter(p=>snapshot?.playback?.matching_pattern_ids.includes(p.id)).map(p=>p.name||`Slot ${p.id}`);
 const disabled=!available||busy||!acknowledged||!!error;
 async function send(action:Action){
  if(sending.current||disabled||!snapshot||(action==='play'&&!selectedPattern))return;
  sending.current=true;onBusy(true);setError('');setNotice('');
  const name=selectedPattern?.name||`Slot ${selectedPattern?.id}`;
  try{
   await request('/api/v1/controller/control',csrf,{action,...(action==='play'?{pattern_id:selectedPattern!.id}:{}),baseline_sha256:snapshot.stable_sha256,acknowledge_clock_sync:true});
   await onComplete();
   setNotice(action==='play'?`Controller confirmed manual playback of ${name}.`:action==='off'?'Controller confirmed OFF.':'Controller confirmed TIMER. Its native schedules now determine the lights.');
  }catch(e){
   if(e instanceof SignedOut)onError(e);
   else setError(`${e instanceof Error?e.message:'Could not confirm the command.'} Refresh before another command; it was not automatically retried.`);
   await onComplete();
  }finally{sending.current=false;onBusy(false);}
 }
 return <section className="panel native-controls" aria-labelledby="native-controls-title">
  <div className="row"><h2 id="native-controls-title">Control the lights</h2><span className="status-pill">Reported mode: {mode==='timer'?'TIMER':mode==='manual'?'MANUAL':mode==='off'?'OFF':'Unknown'}</span></div>
  {matches.length>0&&<p className="muted">{mode==='off'?'Retained effect':'Effect parameters match'}: {matches.join(' / ')}{matches.length>1?' (multiple saved slots)':''}</p>}
  {!available&&<p className="notice">{reason}</p>}
  <p>Play a saved controller pattern for manual lighting, or return to the controller’s own timers. The app will not automatically return to TIMER after manual playback or OFF.</p>
  <div className="native-play"><label>Saved controller pattern<select value={selected} disabled={busy} onChange={e=>setSelected(e.target.value)}><option value="">Choose a pattern</option>{patterns.map(p=><option key={p.id} value={p.id}>{p.name||`Slot ${p.id}`}</option>)}</select></label><button className="button primary" disabled={disabled||!selectedPattern} onClick={()=>void send('play')}>Play selected pattern</button></div>
  <label className="native-ack"><input type="checkbox" checked={acknowledged} disabled={busy} onChange={e=>setAcknowledged(e.target.checked)}/><span>The official Trimlight app is closed. Allow these commands to synchronize the controller’s clock.</span></label>
  <div className="card-actions"><button className="button secondary" disabled={disabled} onClick={()=>void send('off')}>Turn off</button><button className="button secondary" disabled={disabled} onClick={()=>void send('timer')}>Use controller timer</button>{busy&&<span role="status">Checking controller…</span>}</div>
  {error&&<div className="error-banner" role="alert">{error}</div>}
  {notice&&<p className="notice" role="status">{notice}</p>}
  <p className="footnote">These are the controller’s saved patterns. Library designs and the app Calendar do not control the lights yet. TIMER reports a mode; it does not guarantee that lights are currently on. Sunday commands are unavailable until clock encoding is verified.</p>
 </section>;
}
