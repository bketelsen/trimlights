export class SignedOut extends Error {}
export class APIError extends Error {status:number;constructor(message:string,status:number){super(message);this.status=status;}}
export async function request<T>(path:string,csrf?:string,body?:unknown,options?:{method?:string;revision?:number}):Promise<T>{
 const headers:Record<string,string>=csrf?{'Content-Type':'application/json','X-CSRF-Token':csrf}:{};
 if(options?.revision!==undefined)headers['If-Match']=`"${options.revision}"`;
 const response=await fetch(path,{credentials:'same-origin',cache:'no-store',method:options?.method??(csrf?'POST':'GET'),headers,body:csrf?JSON.stringify(body??{}):undefined});
 if(response.status===401)throw new SignedOut();
 if(!response.ok){let message=`Request failed (${response.status})`;try{const value=await response.json();if(typeof value.error==='string')message=value.error;}catch{/* Do not display an HTML gateway error as application content. */}throw new APIError(message,response.status);}
 return response.status===204?undefined as T:response.json();
}
