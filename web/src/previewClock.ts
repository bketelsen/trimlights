// Browser frame timestamps can precede the effect that requested the frame.
// Start from the first delivered frame, and retain progress across pause/resume.
export class PreviewClock {
 private elapsed=0;
 private last:number|undefined;
 restart(){this.last=undefined;}
 tick(now:number,running:boolean):number{
  if(running&&this.last!==undefined)this.elapsed=(this.elapsed+Math.max(0,Math.min(100,now-this.last)))%86400000;
  this.last=now;
  return Math.floor(this.elapsed);
 }
}
