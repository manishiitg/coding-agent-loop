export class PromiseLane {
  private readonly tails = new Map<string, Promise<void>>()

  enqueue<T>(key: string, task: () => Promise<T>): Promise<T> {
    const previous = this.tails.get(key) || Promise.resolve()
    const run = previous.catch(() => undefined).then(task)
    const tail = run.then(() => undefined, () => undefined)
    this.tails.set(key, tail)
    void tail.finally(() => {
      if (this.tails.get(key) === tail) {
        this.tails.delete(key)
      }
    })
    return run
  }

  // Make two identifiers share the same outstanding tail. Chat sessions begin
  // under a local tab ID and later receive a backend session ID; linking them
  // closes the small transition window where each ID could otherwise start an
  // independent queue.
  link(firstKey: string, secondKey: string): void {
    if (!firstKey || !secondKey || firstKey === secondKey) return
    const pending = [this.tails.get(firstKey), this.tails.get(secondKey)]
      .filter((tail): tail is Promise<void> => Boolean(tail))
    if (pending.length === 0) return

    const linked = Promise.all(pending).then(() => undefined, () => undefined)
    this.tails.set(firstKey, linked)
    this.tails.set(secondKey, linked)
    void linked.finally(() => {
      if (this.tails.get(firstKey) === linked) this.tails.delete(firstKey)
      if (this.tails.get(secondKey) === linked) this.tails.delete(secondKey)
    })
  }
}

// ChatArea can remount while a workflow switch or terminal restore is in
// progress. Keep submission ordering outside the component lifecycle so an
// older in-flight send and a new view cannot overtake each other.
export const chatSubmissionLane = new PromiseLane()
