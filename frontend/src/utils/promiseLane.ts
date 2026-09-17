export class PromiseLane {
  private readonly groups = new Map<string, { tail: Promise<void>; keys: Set<string> }>()

  enqueue<T>(key: string, task: () => Promise<T>): Promise<T> {
    const group = this.groups.get(key) || { tail: Promise.resolve(), keys: new Set([key]) }
    this.groups.set(key, group)
    const previous = group.tail
    const run = previous.catch(() => undefined).then(task)
    const tail = run.then(() => undefined, () => undefined)
    group.tail = tail
    void tail.finally(() => {
      if (group.tail !== tail) return
      for (const alias of group.keys) if (this.groups.get(alias) === group) this.groups.delete(alias)
    })
    return run
  }

  // Make two identifiers share the same outstanding tail. Chat sessions begin
  // under a local tab ID and later receive a backend session ID; linking them
  // closes the small transition window where each ID could otherwise start an
  // independent queue.
  link(firstKey: string, secondKey: string): void {
    if (!firstKey || !secondKey || firstKey === secondKey) return
    const first = this.groups.get(firstKey)
    const second = this.groups.get(secondKey)
    if (first && first === second) return
    if (!first && !second) return
    const group = { tail: Promise.all([first?.tail, second?.tail]).then(() => undefined),
      keys: new Set([firstKey, secondKey, ...(first?.keys ?? []), ...(second?.keys ?? [])]) }
    for (const key of group.keys) this.groups.set(key, group)
    const linked = group.tail
    void linked.finally(() => {
      if (group.tail !== linked) return
      for (const key of group.keys) if (this.groups.get(key) === group) this.groups.delete(key)
    })
  }
}

// ChatArea can remount while a workflow switch or terminal restore is in
// progress. Keep submission ordering outside the component lifecycle so an
// older in-flight send and a new view cannot overtake each other.
export const chatSubmissionLane = new PromiseLane()
