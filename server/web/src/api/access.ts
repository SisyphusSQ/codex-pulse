import { api, ApiError } from './client';
export interface Client { id:string; purpose:'admin'|'collector'; name:string; created_at_ms:number; expires_at_ms:number|null; revoked_at_ms:number|null; last_received_at_ms:number|null }
export interface Pairing { code:string; purpose:'admin'|'collector'; expires_at_ms:number }
export async function getClients(signal?:AbortSignal):Promise<Client[]>{
 const value=await api.get<{clients:Client[]}>('/api/v1/clients',{},signal);
 if(!value||!Array.isArray(value.clients))throw new ApiError(502);
 return value.clients;
}
export async function issuePairing(purpose:Client['purpose'],name:string):Promise<Pairing>{
 const value=await api.request<Pairing>('/api/v1/pairings',{body:{purpose,name}});
 if(!value||typeof value.code!=='string'||value.purpose!==purpose||!Number.isSafeInteger(value.expires_at_ms))throw new ApiError(502);
 return value;
}
export const revokePairing=(code:string)=>api.request('/api/v1/pairings/revoke',{body:{code}});
export const revokeClient=(id:string)=>api.request(`/api/v1/clients/${encodeURIComponent(id)}/revoke`,{body:{}});
export const renameClient=(id:string,name:string)=>api.request(`/api/v1/clients/${encodeURIComponent(id)}/rename`,{body:{name}});
