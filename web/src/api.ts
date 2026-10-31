export class SignedOut extends Error {}
// fromServer is true only when the service sent a JSON {"error": "..."} body; gateway pages never set it.
export class APIError extends Error {status:number;fromServer:boolean;constructor(message:string,status:number,fromServer=false){super(message);this.status=status;this.fromServer=fromServer;}}
function serverText(message:string):string {
 const text=message.trim();
 const sentence=text.charAt(0).toUpperCase()+text.slice(1);
 return /[.!?]$/.test(sentence)?sentence:`${sentence}.`;
}
export function userMessage(error:unknown,fallback:string):string {
 if(error instanceof APIError){
  if(/sunday/i.test(error.message))return 'Light controls are unavailable on Sundays for now.';
  if(error.status===409)return 'Someone else saved a change. Your edits are still here; load the latest version or save a copy.';
  if(error.status===400&&/latest controller snapshot/.test(error.message))return 'This saved-on-lights pattern no longer matches your lights, so it cannot be added as a copy.';
  if(error.status===403)return 'Please sign in again to continue.';
  // The service writes 4xx JSON errors as user-safe validation and limit text.
  if(error.fromServer&&error.status>=400&&error.status<500&&error.message.trim())return serverText(error.message);
  if(error.status===413)return 'This file is too large. Choose a smaller file.';
  if(error.status===429)return 'The app is busy. Wait a moment and try again.';
  if(error.status===400||error.status===422)return 'Check your entries and try again.';
  return fallback;
 }
 if(error instanceof TypeError||error instanceof SyntaxError)return fallback;
 return error instanceof Error?error.message:fallback;
}
export function photoUploadMessage(error:unknown):string {
 // A photo upload conflicts only when the household already keeps the most photos.
 if(error instanceof APIError&&error.status===409)return 'This house already has 20 photos, the most it can keep.';
 return userMessage(error,'Could not upload this photo. Check your connection and try again.');
}
// MissingCSRF is a programming error: a mutation was called without the session's CSRF token.
export class MissingCSRF extends Error {constructor(){super('Could not send this change. Reload the app and try again.');}}
// The method is options.method when given; otherwise GET when there is no CSRF token,
// body or raw Blob, and POST when there is. A mutation (any method but GET or HEAD)
// needs a token: without one request() throws and sends nothing, so an empty token
// can never turn a mutation into a GET.
// raw sends a Blob (for example a photo File) as the body with its own content type instead of JSON.
export async function request<T>(path:string,csrf?:string,body?:unknown,options?:{method?:string;revision?:number;raw?:Blob}):Promise<T>{
 const raw=options?.raw;
 const method=(options?.method??(csrf===undefined&&body===undefined&&!raw?'GET':'POST')).toUpperCase();
 const mutation=method!=='GET'&&method!=='HEAD';
 if(mutation&&!csrf)throw new MissingCSRF();
 const headers:Record<string,string>=mutation?{'Content-Type':raw?(raw.type||'application/octet-stream'):'application/json','X-CSRF-Token':csrf!}:{};
 if(options?.revision!==undefined)headers['If-Match']=`"${options.revision}"`;
 const response=await fetch(path,{credentials:'same-origin',cache:'no-store',method,headers,body:mutation?(raw??JSON.stringify(body??{})):undefined});
 if(response.status===401)throw new SignedOut();
 if(!response.ok){let message=`Request failed (${response.status})`,fromServer=false;try{const value=await response.json();if(typeof value?.error==='string'){message=value.error;fromServer=true;}}catch{/* Do not display an HTML gateway error as application content. */}throw new APIError(message,response.status,fromServer);}
 return response.status===204?undefined as T:response.json();
}
