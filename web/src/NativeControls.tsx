import {useEffect,useRef,useState} from 'react';
import {request,SignedOut} from './api';

export type NativeSnapshot={queried_at:string;stable_sha256:string;patterns:{id:number;name:string}[];playback?:{mode:string;matching_pattern_ids:number[]}};
type Action='off'|'play'|'timer';

export function NativeControls({snapshot,available,busy,csrf,refreshVersion,onBusy,onComplete,onError}:{snapshot?:NativeSnapshot;available:boolean;busy:boolean;csrf:string;refreshVersion:number;onBusy:(busy:boolean)=>void;onComplete:()=>Promise<void>;onError:(error:unknown)=>void}){
 const [selected,setSelected]=useState('');
 const [acknowledged,setAcknowledged]=useState(false);
 const [error,setError]=useState('');
 const [notice,setNotice]=useState('');
 const sending=useRef(false);
 useEffect(()=>{
 // eslint-disable-next-line react-hooks/set-state-in-effect -- clears the old error after a refresh; moving it changes behaviour.
 setError('');},[refreshVersion]);
 const patterns=snapshot?.patterns??[];
 const selectedPattern=patterns.find(p=>p.id===Number(selected));
 const mode=snapshot?.playback?.mode??'unknown';
 const matches=patterns.filter(p=>snapshot?.playback?.matching_pattern_ids.includes(p.id)).map(p=>p.name||`Pattern ${p.id}`);
 const disabled=!available||busy||!acknowledged||!!error;
 async function send(action:Action){
  if(sending.current||disabled||!snapshot||(action==='play'&&!selectedPattern))return;
  sending.current=true;onBusy(true);setError('');setNotice('');
  const name=selectedPattern?.name||`Pattern ${selectedPattern?.id}`;
  try{
   await request('/api/v1/controller/control',csrf,{action,...(action==='play'?{pattern_id:selectedPattern!.id}:{}),baseline_sha256:snapshot.stable_sha256,acknowledge_clock_sync:true});
   await onComplete();
   setNotice(action==='play'?`Playing ${name}.`:action==='off'?'Lights turned off.':'Following your light timers.');
  }catch(e){
   if(e instanceof SignedOut)onError(e);
   else setError(e instanceof Error&&/sunday/i.test(e.message)?'Light controls are unavailable on Sundays for now.':'We couldn’t confirm the change. Refresh the lights before trying again.');
   await onComplete();
  }finally{sending.current=false;onBusy(false);}
 }
 return <section className="panel native-controls" aria-labelledby="native-controls-title">
  <div className="row"><div><span className="eyebrow">MAKE TONIGHT YOURS</span><h2 id="native-controls-title">Your lights</h2></div><span className="status-pill">{mode==='timer'?'Following timers':mode==='manual'?'Playing a pattern':mode==='off'?'Turned off':'Refresh to check'}</span></div>
  {mode==='manual'&&matches.length>0&&<p className="now-playing">{matches.join(' / ')}</p>}
  {!available&&<p className="notice">Refresh your lights to get started. If they’re still unavailable, check that they have power and Wi-Fi.</p>}
  <div className="native-play"><label className="field">Choose a pattern<select value={selected} disabled={busy} onChange={e=>setSelected(e.target.value)}><option value="">Choose a pattern</option>{patterns.map(p=><option key={p.id} value={p.id}>{p.name||`Pattern ${p.id}`}</option>)}</select></label><button className="button primary" disabled={disabled||!selectedPattern} onClick={()=>void send('play')}>Play pattern</button></div>
  <label className="native-ack"><input type="checkbox" checked={acknowledged} disabled={busy} onChange={e=>setAcknowledged(e.target.checked)}/><span>The Trimlight phone app is closed. Keep the lights’ clock up to date.</span></label>
  <div className="card-actions"><button className="button secondary" disabled={disabled} onClick={()=>void send('off')}>Turn off</button><button className="button secondary" disabled={disabled} onClick={()=>void send('timer')}>Follow timers</button>{busy&&<span role="status">Updating lights…</span>}</div>
  {error&&<div className="error-banner" role="alert">{error}</div>}
  {notice&&<p className="notice" role="status">{notice}</p>}
  <p className="footnote">When you’re ready to return to your usual times, choose “Follow timers.”</p>
 </section>;
}
