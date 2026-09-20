# Bot Connectors QA Checklist

Full manual test plan for the per-workflow Slack connections feature and the
connector lifecycle fixes (route-switch isolation, mention guard, removal of
session end commands). Check each item; every check states its expected
result. File issues against `bot_connector.go` (lifecycle),
`slack_connections.go` / `slack_service.go` (connections), or
`whatsapp_service.go` (WhatsApp routing).

Related docs: `slack_connections.md` (model, ownership), and the review
findings in `bot_connectors_combined.md` (all closed).

## A. Environment setup

- [ ] A1. Server runs with Slack enabled (global Enable switch on) and at
      least one platform default connection configured by an admin.
- [ ] A2. Two test workflows exist (Workflow A, Workflow B), each owned by a
      non-admin owner account. QA has the owner login plus a second login
      with no access to Workflow B.
- [ ] A3. Two Slack apps exist (App 1, App 2) with bot + app tokens, each
      installed in the test workspace. Note which app posts each message.
- [ ] A4. WhatsApp bot account is linked, with slugs configured for Workflow
      A and Workflow B (`@list` shows both).

## B. Per-workflow Slack connections

- [ ] B1. As the Workflow A owner, open the workflow's Bots panel and add a
      new Slack connection (App 1 tokens). Expected: the connection saves,
      is scoped to Workflow A, and tokens are masked on every read.
- [ ] B2. Select App 1 on Workflow A (`slack_connection_id`), trigger a
      Slack run. Expected: all bot messages arrive from App 1, not the
      platform default.
- [ ] B3. Select nothing (inherit) on Workflow B. Expected: its bot traffic
      uses the platform default app.
- [ ] B4. As a non-owner, attempt to create/edit Workflow A's connection.
      Expected: denied. Bot-route principals can never manage connections.
- [ ] B5. Attempt to delete the default connection, then a connection still
      selected by a workflow. Expected: both blocked with a clear error.
- [ ] B6. Disable Workflow A's connection (or select an unknown ID) and
      trigger a run. Expected: loud failure, no message sent. It must never
      silently fall back to another app.
- [ ] B7. With two enabled connections, run both workflows at once.
      Expected: both Socket Mode listeners stay up; replies from each run
      go out through the correct app.

## C. WhatsApp workflow switching (P1: fresh conversation per route)

- [ ] C1. In a WhatsApp DM, `@switch` to Workflow A and complete a run that
      references something distinctive (e.g. "my favorite color is teal").
- [ ] C2. In the same DM, `@switch` to Workflow B and ask "what is my
      favorite color?". Expected: the bot does not know — the switch
      started a fresh conversation, no history carried over.
- [ ] C3. Repeat C1–C2 while Workflow A's run is still running.
      Expected: the old run is canceled and the B run starts clean.
- [ ] C4. `@off`, then send a plain message. Expected: a fresh generic-chat
      conversation, not a continuation of the workflow run.
- [ ] C5. Stay on one workflow and send several follow-ups within the hour.
      Expected: same conversation continues (no spurious fresh starts).
- [ ] C6. Restart the server mid-conversation, then reply in the same chat.
      Expected: the conversation resumes (durable binding), still on the
      same workflow; a reply routed to a different workflow starts fresh.

## D. Slack mention guard (P2: completed threads stay out)

- [ ] D1. Alice @mentions the bot in a channel thread; the run completes.
      Bob posts a plain reply (no @mention) continuing with Alice.
      Expected: the bot stays silent apart from a reaction; no new run.
- [ ] D2. Same thread; Bob @mentions the bot with a follow-up question.
      Expected: the conversation restarts (same durable identity) and
      answers.
- [ ] D3. Single-user thread: Alice alone, run completes, Alice posts a
      plain follow-up without @mention. Expected: the conversation restarts
      and answers (single-user threads don't require mentions).
- [ ] D4. Mid-run: while a run is active in a multi-user thread, a
      non-mentioned reply arrives. Expected: no follow-up injected
      (existing running-state guard, unchanged).
- [ ] D5. Blocking feedback: with an approval/human-feedback prompt
      outstanding, reply without @mention. Expected: the answer is accepted
      and the run proceeds — blocking answers bypass the guard.

## E. Removed session end commands

- [ ] E1. Send `@reset`, `@done`, `@end` (and `done`/`end` plain) in Slack
      and WhatsApp sessions. Expected: no session control happens; the text
      is treated as an ordinary message (or ignored per the mention guard).
- [ ] E2. Send `@status` in an active session. Expected: status reply with
      run state and resume list; conversation continues.
- [ ] E3. Send `@full` then `@concise` (WhatsApp). Expected: detail mode
      toggles with confirmation; conversation continues.
- [ ] E4. Send `@resume` with no session. Expected: resume picker or a
      "no matching chat" message, never a crash or a cleared session.

## F. Regression: core bot flows

- [ ] F1. Fresh @mention in a new Slack thread starts a run; replies stream
      back into the thread.
- [ ] F2. Plan-approval flow: approve and reject paths both resolve the
      prompt and behave (execute / cancel with a message).
- [ ] F3. Revoke a workflow route (or the sender's access) mid-thread, then
      @mention. Expected: clear access-denied reply, no run.
- [ ] F4. Server restart with an active Slack thread, then reply.
      Expected: conversation continues via the durable binding.
- [ ] F5. WhatsApp 1-hour idle boundary: reply after >1h idle. Expected: a
      new conversation that briefly recalls the prior chat, not a silent
      continuation of the old run.

## G. WhatsApp account management

- [ ] G1. Pair a number: start pairing for the owner account, scan the QR
      with the phone. Expected: device shows connected; bot DMs work.
- [ ] G2. Pair a second device (phone-2 slot) and set a label. Expected:
      both devices send/receive; labels shown in the device list.
- [ ] G3. Slugs: `@list` shows the workflows; `@switch <number>` and a
      direct `@<slug>` select one; `@off` returns to generic chat.
- [ ] G4. Per-slug access: an account without Workflow B access tries its
      slug. Expected: denied with a clear message, no run starts.
- [ ] G5. Link code: confirm codes expire (~24h) and rotate; an expired
      code is rejected.
- [ ] G6. Unpair one device. Expected: it stops receiving; other devices
      unaffected; the slot can be re-paired.
- [ ] G7. Voice note: send a voice message. Expected: transcribed and
      answered (if a transcriber is configured; else skipped).
- [ ] G8. Restart the server with linked devices. Expected: devices
      reconnect without re-pairing (durable session store).

## Sign-off

- [ ] All boxes checked, or failures filed as issues with repro steps.
- [ ] Tester name + date: _______________
