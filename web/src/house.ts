export type Point={x:number;y:number};
export type Run={id:string;name:string;output:string;start:number;count:number;mirror_of?:string};
export type Mapping={run_id:string;points:Point[];reverse:boolean;adjustments:{index:number;point:Point}[]};
export type View={photo_id:string;turns:number;mappings:Mapping[]};
export type Layout={schema:1;runs:Run[];views:View[]};
export type Photo={id:string;name:string;width:number;height:number;created:string};
export type SavedHouse={revision:number;layout:Layout;actor:string;updated:string};
export function positions(m:Mapping,count:number,width:number,height:number):Point[]{
 if(count<1||count>2048||m.points.length<2||width<=0||height<=0)return [];
 const distances=[0];for(let i=1;i<m.points.length;i++){const a=m.points[i-1],b=m.points[i];distances.push(distances[i-1]+Math.hypot((b.x-a.x)*width,(b.y-a.y)*height));}
 const total=distances.at(-1)!;
 const result=Array.from({length:count},(_,i)=>{let f=count===1?.5:i/(count-1);if(m.reverse)f=1-f;const target=total*f;let j=1;while(j<distances.length-1&&distances[j]<target)j++;const a=m.points[j-1],b=m.points[j],span=distances[j]-distances[j-1],t=span>0?(target-distances[j-1])/span:0;return {x:a.x+(b.x-a.x)*t,y:a.y+(b.y-a.y)*t};});
 for(const a of m.adjustments)if(a.index>=0&&a.index<count)result[a.index]=a.point;
 return result;
}
export function rotateView(view:View):View {const rotate=(p:Point)=>({x:1-p.y,y:p.x});return {...view,turns:(view.turns+1)%4,mappings:view.mappings.map(m=>({...m,points:m.points.map(rotate),adjustments:m.adjustments.map(a=>({...a,point:rotate(a.point)}))}))};}
export function sourceRun(run:Run,runs:Run[]):Run{return runs.find(r=>r.id===run.mirror_of)??run;}
