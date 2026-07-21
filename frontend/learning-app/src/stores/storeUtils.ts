// SetStateAction mirrors React's useState setter signature exactly (accepts
// either a plain value or an updater function) so migrating a useState call
// site to a Zustand store field requires no change to the call site itself.
export type SetStateAction<T> = T | ((prev: T) => T)

export function resolveSetState<T>(action: SetStateAction<T>, prev: T): T {
  return typeof action === 'function' ? (action as (p: T) => T)(prev) : action
}
