import type { SyncSnapshot } from "./types";

export function syncActions(state: SyncSnapshot["state"] | undefined) {
  return {
    canPull: state === "remote-ahead" || state === "diverged",
    canPush: state === "local-only" || state === "local-ahead" || state === "diverged",
  };
}
