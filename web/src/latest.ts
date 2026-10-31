// latestRequests orders the answers of repeated requests for one resource (for
// example the status poll). Each request gets a ticket, in the order it starts.
//
// - settle() records an answer (or a failure). It returns true when no newer request
//   has answered yet, so a slow older answer cannot overwrite fresher state. A newer
//   request that has only started does not reject an older answer: when every answer
//   takes longer than the poll interval, each answer still applies in turn.
// - busy() is true while a request runs. The periodic poll waits for it, so polls do
//   not pile up. A request that runs longer than staleMs no longer counts, so a hung
//   request cannot stop the poll for good.
export function latestRequests(now: () => number = Date.now) {
  let issued = 0;
  let settled = 0;
  const running = new Map<number, number>();
  return {
    begin() {
      const ticket = ++issued;
      running.set(ticket, now());
      return function settle() {
        running.delete(ticket);
        if (ticket <= settled) return false;
        settled = ticket;
        return true;
      };
    },
    busy(staleMs: number) {
      for (const started of running.values()) if (now() - started < staleMs) return true;
      return false;
    },
  };
}
