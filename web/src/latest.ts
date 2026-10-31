// latestRequests gives each request a ticket. Only the newest ticket can apply its
// answer: an older request that answers late (a slow poll) cannot overwrite fresher
// state. begin() returns a check that stays true while no newer request has begun.
export function latestRequests(){
 let newest=0;
 return {begin(){const ticket=++newest;return ()=>ticket===newest;}};
}
