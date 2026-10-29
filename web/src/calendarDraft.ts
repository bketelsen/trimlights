import type {Plan,Rule} from './Schedule';

// Called against the latest draft, not the request's closed-over value. Review is
// invalidated by any intervening edit; appending never replaces unrelated state.
export function appendNativeRules(current:Plan,reviewed:Plan,rules:Rule[]):Plan {
 if(JSON.stringify(current)!==JSON.stringify(reviewed))throw new Error('The draft changed. Review controller timers again before adding rules.');
 if(!rules.length||current.rules.length+rules.length>100)throw new Error('Select rules within the 100-rule calendar limit.');
 const ids=new Set(current.rules.map(r=>r.id));
 if(current.override)ids.add(current.override.id);
 for(const rule of rules){if(ids.has(rule.id))throw new Error('This timer rule is already in the draft. Existing rules were kept.');ids.add(rule.id)}
 return {...current,rules:[...current.rules,...structuredClone(rules)]};
}
