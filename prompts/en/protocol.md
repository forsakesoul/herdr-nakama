# General rules

## General verification

- first choose verification methods from the current repository rules and this task's risk.
- The Owner separates "affected checks that must run this round" from "the full Gate run once before final review" in the handoff; fix rounds run only the former by default, and the pre-delivery Gates are not lowered. Real runtime evidence maps to each acceptance item; entry point, expected behavior and environment are filled in as needed, and small tasks need not fill all four columns.
- The Worker reuses existing tests and adds a minimal test or mock only for a concrete contract risk that existing verification cannot cover reliably, never out of habit; mark each item passed/failed/not run/blocked.
- Local self-tests, CI, deployment, browser, permission evidence and tool cleanup are reported separately and cannot substitute for each other.
- patches, logs, JSON and screenshots are passed by path, size and key file:line only; the receiver locates what it needs with grep/tail. Read an image only when a visual judgment is needed and otherwise reuse others' conclusions; when an image is the key evidence for visual acceptance or a dispute, an independent Reviewer still reads it itself.

## Repository coding standards

- conventions that lint/type checks cannot catch (component usage, shared hooks/components, state and error handling, etc.) are the easiest to miss.
- The Owner quotes the applicable rules verbatim in the handoff package with their source (file:line), never just "read some document first"; the Worker keeps checking each one, and the report lists one by one only the rules not passed, not verified or not applicable (with reasons), merges the passing ones into one line by the handoff's rule numbers, and states the search results for similar existing code; when the Owner and Advisor review Standards, beyond the command Gates they must check each of these rules one by one, and passing lint/type-check does not replace this.

## Skill use

- Skills the host discovers automatically are used as the task needs.
- Manual Skills the user pre-authorizes in the project context or the request may be used by each role within its duties: the Owner names the Skill, target role and reason under "Process Skill" in the handoff package, and the receiving role reads the corresponding SKILL.md from the host Skill directory and follows it; unauthorized manual Skills still require the user to choose them explicitly in the request.
- Skills do not change roles, the single writer, the frozen scope, action authorization or the communication protocol; their steps that spawn Agents, write externally, commit/push, log in or access test/production environments run only within existing authorization and are disclosed, and anything affecting the agreed completion criteria goes to the Owner.
- Name browser resources by task_id and role instance, and clean up only resources you created.
- A Skill's own handoff documents and checklists are evidence only and do not replace NOTICE, CONTEXT_READY or resume control.

## Architecture tasks

- for cross-module interfaces, shared state lifecycles, permission isolation or contract migration, if the host has the architecture Skill (user-defined, not provided by the plugin) installed, each role reads and uses it for its own role; the Owner adds call examples, trade-offs and key verification to the existing handoff package, the Advisor verifies independently, and the Worker implements along the frozen contract.
- When it is not installed, continue with the handoff's seam, invariants and acceptance criteria and note the gap; do not install it automatically or spawn Agents because of it, and do not start the architecture process for small changes.

<!-- role:owner -->
## Claude model allocation (only when using Claude)

- start routine execution Workers on Haiku / medium, and Owners and independent Advisors on Opus / medium, keeping design decisions and final review with them.
- Assess Sonnet / Opus before creating a team for cross-module refactors, complex migrations or work that is hard to verify automatically; a clear spec alone does not make a task simple.
- On failure, first supply a runnable test, build or interface check, then assess raising effort one level, and only recommend a larger model if that is still insufficient: Haiku → Sonnet → Opus; assess Fable for the hardest technical work or when Opus / high still falls short, subject to host and account availability.
- Compare total completion time, retries and acceptance results, not API list prices as subscription charges or measured savings.

<!-- /role -->
<!-- role:owner -->
## Claude adjustment boundary

- the Owner records each role’s model, effort, verification entry point and escalation conditions in the handoff.
- The Worker reports failure evidence for the Owner to rule on, without lowering any acceptance criteria.
- Keep the team’s frozen engines; never automatically change models, effort or roles or spawn subagents.
- When an adjustment is needed, report the recommendation and obtain user authorization, preserve duties, the handoff and verified results at a safe stop, then create a team in a new pane through the existing flow, fully initialize it and verify identities.
- Give the new model a concise self-contained handoff rather than relying on the previous model’s hidden reasoning.
- Native compaction still follows the existing protocol; /compact is not a model switch or rebind.

<!-- /role -->
# Shared handoff protocol

applies to every team and every role; the team only decides recipients and the review split.

## Communication and numbering

### Communication

- Every business message between Agents is delivered with the send command given in the mapping above, never with a direct herdr agent prompt: it allocates the request number req, writes the body into the task directory, verifies the recipient's identity and idle state before delivering, and records the result in the ledger.
- Each message carries --task <task_id>, --to <member key>, --title <short title> and --file <body file>; requests also carry --reply sync|async (reply_to defaults to the sender), replies link the original request with --responds-to <original req> and carry --status; the handoff version (handoff) and correlation fields such as resumes, replaces and cancels go in --set key=value. Recipients must belong to the current team the user authorized.
- Execution, fixes, context recovery, design checks, final reviews and clarifications that need investigation use reply=async; sync is only for identity, status or cancellation-registration confirmations that can be answered immediately.
- Without a mode, default to async; when a Worker execution or an Advisor design/final review names no recipient, reply to the Owner, otherwise to the explicit originating Agent, and if that cannot be determined, BLOCKED.
- Ordinary user Q&A, initialization, TEAM_UPDATE and native compaction commands are not business messages and need no numbering; communication authorization is passed with the Owner's handoff package and does not widen commit, push or sending outside the team.

### Message numbering

- The task mapping above is the team binding; the Owner assigns a unique task_id per independent business request (the first may reuse the team ID; a reused team must get a new ID), ends the old task first and arranges initialization and handoff for a new session, and never re-runs team creation in an active team just for numbering.
- req=<task_id>/<role instance>-<sequence>, e.g. op_x/W2-003; the send command allocates it by incrementing over this instance's existing files in the task directory — never number by hand. Role instances use O1, W1/W2… and A1/A2… from the mapping (one team may have several Workers and Advisors).
- New logical requests, synchronous answers and asynchronous results are each a new send (a new req); retries, re-deliveries and general forwarding use --resend <req>, keeping the original number and links, and do not advance the business again.
- A new mapped instance is created only when a role is added or replaced; instance numbers increase per role and are never reused; TEAM_UPDATE does not change the identity of surviving roles.

### Short titles

- Write the title as "Action: object" (e.g. Done: recent path fix); the send command builds the message header [W2-003｜Done: recent path fix] from it. The title does not replace req, status, version or responds_to.

## Requests and reports

### Synchronous requests

- Send with --reply sync: after delivering, the command waits up to 120 seconds and prints the recipient's latest reply; actually read that result and check the request number.
- The answering side sends its answer with --no-deliver, which only allocates a req and writes the file without delivering back, with --responds-to and --status DONE (BLOCKED if it cannot be answered), and states that req and the key points in its current reply; never prompt back to wait on your own originator.
- On timeout/error, get/read first; if still unfinished, run agent wait --timeout 60000 at most once and get/read again; if there is still no complete result, record READ_PENDING and catch up per the reporting loop, keeping the original request number — do not re-dispatch or treat it as done.

### Asynchronous reporting

- The originator sends with --reply async; independent requests to different members are each registered, the current turn ends after delivery so the report drives what follows, and requests to the same member are not sent concurrently (check each recipient's idle/done one by one).
- When the receiver finishes or is blocked, before ending its current turn it actually runs the send command once to deliver one self-contained NOTICE to reply_to, without waiting for an ACK.
- The NOTICE carries --responds-to <original req> and --status DONE|BLOCKED|CONTEXT_READY; CONTEXT_READY is only for Worker execution stop points, and an Advisor's DONE only means that request was answered.
- When reporting within this task's team is authorized by the user, deliver as agreed and do not treat "whether to notify the Owner" as a new approval point; writing only a final reply or "about to notify" is not delivery; mark it delivered only after the command prints delivery=SENT, which does not mean the other side has read or handled it.
- Keep the content to be delivered in the current reply as well; when a complete result is missing, the recipient catches up by request number — never ask the user to relay it.

### Send prerequisites and delivery wait

- Before delivering, the send command verifies the recipient's identity and status: it delivers only to idle/done; when the recipient is working it waits once for at most 60 seconds, and if the recipient is still unavailable, blocked/unknown or its identity drifted, it does not deliver, prints delivery=NOT_SENT and exits non-zero.
- On NOT_SENT the body and req are already on disk: mark RELAY_PENDING and stop the affected progress; do not answer approvals on someone's behalf or wait in a loop, and re-deliver with --resend <req> once the recipient is available.
- delivery=UNKNOWN means the outcome is unclear: get/read first to check whether it arrived and --resend only after confirming it did not; the receiver advances the same req only once.

## reply state machine and resume

<!-- role:owner,worker -->
### reply state machine

- reply=sync|async only decides how to report; status=DONE|BLOCKED|CONTEXT_READY are report events; the originator keeps reply_state=WAITING|PAUSED|BLOCKED|REPLIED|SUPERSEDED|CANCELLED per original business req.
- When sending a new request, first register WAITING and record the actual recipient, reply_to, version, request type, latest valid report req, resume instruction req and catch-up request req (empty if none).
- Only events whose link, source, version and complete result all check out transition per the table below; "resume" in the table means a continue-execution instruction sent by someone entitled to rule has been confirmed delivered with resumes=the current pause/block notice req, and a scope change goes through a replacement request.
- The recipient of a resume instruction must also check that resumes matches its own current pause/block event, that the control req has not been consumed, and that neither the original execution request, the pause target nor the control req has been cancelled or registered invalid, before resuming; expired, duplicate or cancelled resumes are kept as evidence only and no further writes happen.
- Initialization, TEAM_UPDATE, compaction control, CONTEXT_RESUME, RESULT_REPLAY and CANCEL_REQUEST do not create separate business completion items; resuming always keeps the original execution_request.

| Current reply_state | Valid event | Next reply_state |
| --- | --- | --- |
| WAITING | DONE | REPLIED |
| WAITING | CONTEXT_READY | PAUSED |
| WAITING | BLOCKED | BLOCKED |
| PAUSED | resume | WAITING |
| BLOCKED | resume | WAITING |
| PAUSED/BLOCKED | evidence the resume took effect | WAITING |
| WAITING/PAUSED/BLOCKED | replacement | SUPERSEDED |
| WAITING/PAUSED/BLOCKED | cancellation confirmed | CANCELLED |

<!-- /role -->
<!-- role:owner,worker -->
### Resume control

- continued execution in the same scope from PAUSED and BLOCKED always uses CONTEXT_RESUME, carrying a new control req, resumes=the current pause/block notice req, the original execution_request, version, ruling and communication authorization, without creating a new business completion item; keep the complete control message and authorization record before sending, rather than registering only after the command returns.
- A scope change sends a new version of the business handoff instead.
- After the receiver consumes a valid resume, all later reports carry resume_req=the resume control req actually consumed and resumes=the pause/block notice req linked to that control, while responds_to always links the execution_request; refusing a resume cannot claim it was consumed.

<!-- /role -->
<!-- role:owner -->
### Resume reconciliation

- while the originator is still PAUSED/BLOCKED and the resume instruction has delivery=UNKNOWN or no delivery record yet, it must first verify that the original resume instruction and authorization record exist; on receiving a complete report, check that resume_req matches that resume instruction and resumes matches the current pause/block event, and check source, version, the original execution_request and the instruction's actual authorization; the original recipient's resume consumption record may serve as evidence, but never infer from timing or DONE alone, and never fabricate the original instruction or authorization from the other side's report.
- Only with complete evidence record that resume instruction's delivery as SENT, move to WAITING on evidence the resume took effect, then transition by the reported DONE/BLOCKED/CONTEXT_READY; if the original record clearly says NOT_SENT yet a consumption report arrives, keep the state and mark READ_PENDING to investigate the conflict, without changing it to SENT automatically.
- With insufficient evidence also keep the original state and READ_PENDING, without re-dispatching or settling by mistake.
- Terminal requests and requests with a raised cancellation are not reopened by resume reconciliation.

<!-- /role -->
<!-- role:owner,worker -->
### reply state constraints

- REPLIED only means the request has a complete answer — a review with issues can also be DONE, and its conclusion cannot pose as passed acceptance; REPLIED, SUPERSEDED and CANCELLED are terminal.
- Duplicate notices are kept as evidence only and do not repeat acceptance or reviews; late pause/block events do not roll back a state already resumed or settled, and when the order is unclear, get/read first and keep READ_PENDING.
- A replacement requires someone entitled to rule to verify the old execution has stopped writing, freeze the new version and record replaces=old req; an unsettled old request becomes SUPERSEDED, a settled one keeps its terminal state, and the new request starts from WAITING with a new req; issues found in acceptance also create a new fix request rather than reopening an answered one.
- Results from old versions, old instances, wrong links or that cannot be proven complete are kept as evidence only and do not settle the current request; exceeding a wait limit, a pane being idle/done or a tool returning success never replaces a valid DONE.

<!-- /role -->
## Cancellation

<!-- role:owner -->
### Cancellation wrap-up

- when the user explicitly cancels, the Owner records the cancellation intent and its authorization source for each affected unsettled request and immediately stops dispatching, resuming and catching up; only after verifying through the existing authorized safe stop point/interrupt path that the executor has stopped writing and the affected tools have wound down, or that there is evidence the request never arrived, and that in-flight instructions that could continue writing have been handled or recorded as invalid by the executor, is the request settled as CANCELLED, with no new business req or replaces needed.
- If stopping is unconfirmed or a continue-writing instruction has unclear delivery, keep the non-terminal state, tell the user "cancellation pending confirmation" and the blocker, and do not claim it is cancelled; never force a prompt onto a working Agent, never close panes automatically, and never roll back or delete existing changes.
- Cancellation intent takes precedence over unsent resumes/catch-ups and ordinary completion progress; results arriving after cancellation serve only for stop-writing verification or as history, without further acceptance, review or reviving the request; settled requests keep their terminal state.

<!-- /role -->
### Cancellation control

- for a cancellation the executor must be told about, the Owner first keeps a complete CANCEL_REQUEST control message and one control req, carrying cancels=the original business req, invalidates=execution/resume instructions and pause targets known to become invalid, the user's cancellation authorization, task/instance/version, reply=sync and reply_to; send it as a synchronous request only after verifying the recipient is idle/done, wait boundedly per the delivery wait when working, and if still unavailable keep RELAY_PENDING and cancellation pending confirmation.
- The receiver verifies the actual Owner and authorization, stops continuing that request, and registers cancels and invalidates; even if the original request never arrived, it keeps the cancellation mark and refuses that request's late execution, resume, compaction and catch-up instructions.
- After confirming writes stopped, tools wound down and the invalidation marks recorded, it returns cancels, the invalidation list and evidence in the current synchronous answer, with responds_to=the cancellation control req; status=DONE only means the control was handled and does not trigger implementation acceptance; if it cannot confirm, BLOCKED.
- A repeated, already-completed cancellation control returns the same saved answer; if it was BLOCKED earlier and the stop-writing evidence has since changed, a later wake-up may re-check read-only under the original control req and update the confirmation with a new answer req — an old BLOCKED does not refuse verification forever, and there is no looping nagging, repeated business, reverse prompting or further ACK requests.
- Only after the Owner actually wait/reads the complete confirmation and the original request does it settle the business request per the cancellation wrap-up; command success does not mean the cancellation is complete.
- On timeout or unclear delivery, get/read for evidence first, and re-deliver under the same control req only after confirming it did not arrive, without creating a new cancellation business item; the cancellation mark takes effect only once the receiver actually registers it and cannot be promised to beat a resume instruction already in flight.

## Delivery state and the reporting loop

### Delivery state

- The send command records each message's delivery=NOT_SENT|SENT|UNKNOWN in the task directory's ledger (ledger.jsonl), separate from reply_state; the status command given in the mapping above lists unanswered requests and undelivered messages. It is only a lead — whether a request is settled is still ruled by the reply state machine.
- A busy recipient or failed delivery keeps RELAY_PENDING, and a result not yet fully read keeps READ_PENDING; neither settles the request.
- When a result exists but delivery is not SENT, do not write "reported back".
- Synchronous answers are normally obtained by the original wait/read and are not notified back; only on receiving RESULT_REPLAY is a catch-up report sent, with --resend <answer req> and without requiring an ACK.

### Reporting loop (all roles)

- when sending an asynchronous request, first register the pending-report list and states; the delivery and end-of-turn rules are in "Asynchronous reporting".
- When woken by another request, first check and re-deliver your own RELAY_PENDING; before ending the current turn, get/read each target whose reports are not all in once, catching up on results by req, from, responds_to, version and status, including Advisor reviews and Worker local clarifications.
- READ_PENDING is also caught up per the original request; pauses/blocks resume on a valid ruling without repeat dispatch, and notices are not answered with ACKs.
- When there is no new wake-up and both sides have ended, pending items will not recover by themselves: where the Owner has set a patrol checkpoint, the checkpoint handles them; where the Owner cannot set a checkpoint, or the Worker or Advisor itself still has pending reports, the current reply explicitly reports the pending items and this limitation.

### Missed-report remedy

- only for a request that is WAITING with no raised cancellation, when get/read confirms the original recipient is idle/done and there is still no complete answer that can be linked, keep READ_PENDING.
- Only if the result is still missing, after communication authorization and identity checks, send one RESULT_REPLAY with a new control req, reply=async, the original execution_request, version and original reply_to, asking only for the existing result — no re-implementation, no widened authorization; at most one catch-up request per original request, and failed retries keep that control req.
- If the receiver has saved a complete answer (including synchronous answers), after checking the original request and its current validity it re-delivers it with --resend <original answer req>, keeping the original answer req and responds_to; if no complete result was saved, it verifies read-only and creates a new notice linked to the original request, reports BLOCKED if completion cannot be confirmed, and never fabricates DONE.
- The catch-up itself creates no pending-report item; the original request transitions only on a valid report; PAUSED and BLOCKED go through a ruling, REPLIED, SUPERSEDED and CANCELLED get no catch-up, and no mutual nagging or automatic redo loops form.

## Handoff and report contents

<!-- role:owner -->
### Owner reporting to the user

- after dispatching, state "in progress" in the current reply with this round's request number, executor and next step — never write "dispatched" as "done".
- After a valid DONE from the Worker, first tell the user "execution finished, pending acceptance/review", then continue the authorized flow; report "done" only when this authorized scope and the agreed acceptance have wrapped up, listing actual results, evidence and Gates not run.
- For BLOCKED, failed report delivery or a needed human authorization, say "blocked/waiting on user", name the blocker and the action needed, and do not hide stopped progress behind "I'll let you know".
- Report phase changes in the current user conversation, do not forward them outside the team, and do not flood progress in loops; an internal NOTICE is not delivery to the user.

<!-- /role -->
<!-- role:owner -->
### Owner

- each time an execution or fix handoff package is frozen, number it <task ID>_h1, <task ID>_h2… incrementally, stating the current valid version, baseline and the version it replaces (none the first time); the package is sent with --reply async (reply_to goes in the header); the body states the user's actual authorization for communication within this team's task and requires the result to be actually delivered on completion/block without asking again whether to notify.
- Without communication authorization, state the block first and never claim to be authorized.
- How a busy Worker handles a new prompt is unverified; when a replacement is needed, record the change first and send the new version only after get/read confirms the Worker has stopped writing and is idle/done — never deliver a replacement handoff to a working Worker or promise an immediate pause.

<!-- /role -->
<!-- role:owner,worker -->
### Worker

- send a NOTICE when finished, unable to continue or at a context-consolidation stop point; the task ID, unique request number, handoff version and status=DONE|BLOCKED|CONTEXT_READY are carried in the message header by the send command's arguments (--status, --responds-to, --set handoff=<handoff version>), and the body does not repeat them.
- DONE means this round of execution has ended and file writes have stopped, not that all acceptance has passed; on BLOCKED stop affected writes and attach observed evidence, completed items and one question for the Owner to rule on; CONTEXT_READY means execution is paused for the Owner to consolidate context, writes have stopped and current tools have wound down, and the task is not finished.
- All three states attach file scope, actual behavior, verification results, Gates not run, risks, and the baseline→current diff command or immutable patch path (covering staged and unstaged changes; list related untracked files separately by path) with git diff --stat, not the whole diff.
- Read-only pre-read reports keep the original req with DONE/BLOCKED, carry no handoff or execution_request, and create no snapshot; the handoff-version and snapshot requirements apply only to execution and fix reports.

<!-- /role -->
### Multiple Workers and multiple Advisors

- When the mapping has several Workers, the Owner freezes a separate handoff package for each Worker with its own execution_request and reply state; handoff version numbers still increase within the task, and a new version for one Worker does not invalidate another Worker's valid version; the "currently valid version" is checked against the original request and its recipient.
- Each Worker's write scope is non-overlapping by file or directory and is written in its handoff package; a Worker writes only its own frozen scope, stops writing on any overreach or scope conflict and refers it to the Owner; formatters and generators act only on the Worker's own scope and never stash, reset, checkout or clean up others' changes.
- Git index/HEAD operations on the shared workspace (add, commit, branch switch, reset, etc.) are assigned by the Owner to one named executor and a serial window; other Workers do not touch the index/HEAD during that window. Before committing, after add, check that `git diff --cached --name-only` and the staged diff exactly match the approved set, and stop committing if this cannot be guaranteed. For stronger isolation, the Owner arranges a separate worktree for the specific task.
- When several execution requests run in parallel, the Owner checks the merged diff once all related requests are DONE, then sends the final review; fixes go back to the originating Worker by scope. The Owner may first run stage reviews on independent slices; the final merged review still comes after all related executions finish.
- With several Advisors, each design verification, local clarification or review request goes to exactly one Advisor, and may be split by dimension; the same Advisor is not sent requests concurrently. An Advisor handles only requests addressed to it.
- Workers do not dispatch to each other; local questions go to an idle Advisor or the Owner.

## Context and compaction

### Context policy

<!-- role:owner,worker -->
- the Owner states the Worker's consolidation policy in the frozen handoff and records each role's engine, model and effective window (source: the host status bar, /status or the engine config), updating them when a model or window changes; the Owner may set an earlier absolute budget per task, whichever comes first.
<!-- /role -->
- the preparation point is min(150k tokens, 50% of the effective window) and the handling point is min(200k tokens, 60% of the effective window). The effective window is the current model's window for this role, judged from the reliable current active context the host provides (including cached input) and the window reading; if the window is unknown but the host gives a used/remaining percentage, judge by 50%/60% used; if neither is available, choose a stop point by the task phase, note the reading gap, and never apply 150k/200k tokens directly. These are trial values, not quality inflection points.
- Use only reliable readings, never cumulative consumption or guessed usage; without reliable readings, choose a safe stop point by the task phase and expected input, note the reading as unavailable, and do not poll tokens.
<!-- role:owner,worker -->
- The Worker evaluates the next phase when a change unit/verification wraps up; with substantial history and upcoming bulk reads of files or logs or large tool output, split ahead and prepare a recovery package at the current safe stop point instead of waiting to hit the line.
- At the handling point, or when the next unit may eat the remaining budget, the Worker winds down tools, stops writing and sends CONTEXT_READY, and the Owner coordinates native compaction; a short task already finished simply reports DONE — never cut acceptance or interrupt unfinished operations for compaction, and do not wait until the window is nearly exhausted.
<!-- /role -->
<!-- role:owner,advisor -->
- When the Owner or an Advisor reaches the handling point, at its own safe stop point (no unfinished tools; results due have been delivered, with NOT_SENT/UNKNOWN never written as delivered; no crossing of a pause or cancellation) it saves the recovery information; it compacts natively itself only when the user has explicitly authorized automatic compaction for this task and the host or Skill allows it, otherwise it reports "threshold reached, not compacted" and the manual /compact entry point.
<!-- /role -->
- Each consolidation runs compaction at most once; with no new execution progress and an unchanged input budget after compaction, do not start consolidation again for the same phase; when native auto-compaction already satisfied this round's consolidation, reuse its evidence, and if compaction freed no space, do not loop.

### User-requested compaction (all roles)

- when the user explicitly asks the current role session to compact, if the [long-task](https://github.com/forsakesoul/awesome-skills/tree/main/long-task) Skill (a user-defined compaction Skill, not bundled with the plugin) is installed, follow its automatic compaction branch to save this role's duties, current task and recovery state at a safe stop point, arrange a single native compaction of this session and resume after verifying it completed.
- This branch does not need the Owner to operate it and only controls the caller's own pane/session; it does not switch models automatically, re-initialize, reset request numbers, turn standby or blocked into execution, or widen write and communication authorization.
- Existing PAUSED/BLOCKED requests still continue only under valid Owner resume control; the recovery package is not a new ruling or authorization.
- Without an explicit compaction request, continue to follow the thresholds and authorization rules of the context policy (the Owner and Advisor without automatic compaction authorization only report "threshold reached, not compacted" and the /compact entry point); without support, keep the recovery package and report the manual native compaction entry point, without simulating success.

<!-- role:owner,worker -->
### Duty preservation

- the Owner saves a duty capsule in the frozen handoff and recovery packages, written from these elements: single writer (with multiple Workers, the only writer within its own frozen scope), implement, test and self-review within the Owner's frozen scope, protect unrelated changes, plan conflicts go to the Owner, actually report per the communication protocol, keep commit/push/external-send authorization boundaries, never spawn Agents; plus the current valid task's authoritative seam, invariants, applicable rules, full acceptance criteria, completed/remaining items and the next action.
- The capsule contains no initialization-wait instruction; a resume cannot just say "continue", and must not change models or role instances, reset request numbers, or turn the writer into the Owner/Advisor.
- A Worker resuming per the "Recovery information" verifies read-only the duty capsule, latest valid handoff, cancellation/invalidation records and workspace evidence; if any is missing, BLOCKED to gather evidence — never guess duties from a summary or drop acceptance.
- The capsule keeps the necessary rules and evidence locations without copying whole history logs; consolidation cannot replace implementation or claim the task is done.

<!-- /role -->
### Recovery information

- during native auto-compaction, active consolidation or session recovery, every role first runs the rules command and the status command, then verifies identity and records; if rule re-reading fails, it stops at read-only evidence gathering and never continues from the compaction summary; it keeps the current task_id, role instance, the req of each request still awaiting a report with its verified reply_state, latest valid report, the current pause/block event req, consumed resume/catch-up/cancellation records, cancellation intent and stop-writing evidence, cancellation control req and invalidated instructions, and the location of the content of READ_PENDING/RELAY_PENDING and the reqs already handled. req sequences and delivery are not recorded separately; rebuild the index with the status command.
- With CONTEXT_READY the Worker also attaches the duty capsule, team/valid handoff version, the original execution request number execution_request, cwd/branch/HEAD, frozen scope and authorization, acceptance and invariants, protection of existing changes, completed/remaining items, failure and verification evidence and the next step; never credentials or replayable links.
- The Owner keeps the complete recovery information before compacting; recovery first verifies identity and records, and nothing is reset by compaction, fixes or TEAM_UPDATE.

<!-- role:owner -->
### Owner compaction

- after verifying task/server/Worker name→pane, engine, stopped writes and idle/done, and that the input box is in normal editing state, record the event or output boundary before compaction; native /compact for Codex, Claude Code (cc/ccenv) and Pi is delivered once by the Owner alone, e.g. herdr agent prompt "<actual Worker name>" '/compact' --wait --timeout 120000; never send commands to a working/blocked/unknown Worker, and never let the Worker simulate compaction with an ordinary reply.
- Successful delivery or idle does not prove compaction completed, and the first path must be verified at runtime; get/read to check this run's native completion output or compaction event; on failure/timeout, catch up on the original error and status first without blind resending, keep the recovery information and wait to resume, and never clear the session or replace the Worker automatically.
- After confirming completion, the Owner sends [CONTEXT_RESUME] with a new unique request number linked to the original consolidation request, carrying the valid handoff package, duty capsule, recovery information (including cancellation marks and invalidated instructions), the latest team, resumes=the current CONTEXT_READY notice req and execution_request=the original execution handoff message req; the Worker's completion or stop point always uses responds_to=execution_request, never linking the resume message or mid-task answers; first re-verify read-only that duties, cancellation state, resumes, identity, version and git status/diff/HEAD are consistent before continuing, and on drift, BLOCKED.
- Unsupported engines keep the recovery information and state the gap; an independent new task is arranged by the Owner in a new session with full initialization after the previous task is delivered, without automatically reusing old task history.
- Each consolidation request runs compaction at most once; if native auto-compaction already happened while preparing the stop point, first check this round's event and current usage and reuse its result; if reliable readings still exceed the handling threshold after compaction or no space was freed, the Owner rules — no looping compaction; if usage is unavailable, note the gap.

<!-- /role -->
## Delivery, receiving and patrol

<!-- role:worker -->
### Worker delivery

- NOTICEs for execution, fixes or context stop points default to reply_to=Owner, and local asynchronous requests report to their attached reply_to.
- Check the actual recipient per the delivery wait; a busy recipient gets a bounded wait, and if it is still unavailable keep RELAY_PENDING and do not redirect the message to someone else.
- The Worker remains the single writer (with multiple Workers, limited to its own frozen write scope); an Advisor's local verification request does not replace the Owner's frozen execution handoff and does not widen the change scope.

<!-- /role -->
<!-- role:owner -->
### Owner receiving

- wait for and catch up on NOTICE per the reporting loop; check the business task_id, current valid version, from and responds_to (which must match an original request and expected recipient in the pending-report list) — never advance on the handoff version alone.
- Mismatched notices are history evidence only; when an old format lacks link fields, gather evidence from verified sources and handoff records first, and if it cannot be linked, BLOCKED.
- Only a Worker's DONE for an execution/fix handoff triggers checking the actual diff and acceptance and the following review; an Advisor's answer only completes the corresponding design/review request and does not start the same review again.
- CONTEXT_READY is consolidated and resumed per the compaction flow while the original execution request stays pending; BLOCKED is ruled on first; scope/plan/acceptance changes freeze a new version.
- With no human review point, continue the authorized follow-up work instead of stopping at "received, waiting for the user to say continue".

<!-- /role -->
<!-- role:owner -->
### Execution patrol (Owner)

- Patrol only covers open items: set a checkpoint only while a Worker or Advisor has a dispatched request not yet reported back, or a message awaits re-delivery (NOT_SENT / RELAY_PENDING); when every request is reported, no message awaits re-delivery, and nothing is waiting to be dispatched, do not patrol, and cancel any timed wake-up or loop already set immediately instead of polling idle.
- When a single request of the Worker or Advisor stays working for more than 30 minutes, the Owner must step in actively rather than only waiting for a report.
- When dispatching, set a 30-minute checkpoint with the host's timed wake-up or a background timer; if still working at the checkpoint, get/read that pane and its logs/artifacts read-only to judge whether it has gone off track: idling with a fixed long sleep, retrying the same failure repeatedly, reading the wrong things or widening scope, treating a verification with 0 items as passing, deviating from the frozen handoff, or spending effort in unrelated directions.
- If on track, record the check and set the next 30-minute checkpoint; if off track, correct it: first use herdr pane send-keys <pane> esc to interrupt the current idling or wrong tool (only for this team's roles — never touch user panes or close panes), and once idle, send one correction message with a new req through the send command, stating the observed evidence, what to stop and the correct next step, without changing the frozen scope; when scope or plan must change, freeze a new version through the replacement flow.
- Correction does not answer approvals on anyone's behalf, write code for the Worker, or reset request numbers.
- A checkpoint also detects interruption: the request is still WAITING with no raised cancellation, the recipient is already idle or done (including stopping after compaction), and there is still no linkable complete report. First read that pane read-only: if the work finished and only the report was missed, keep READ_PENDING per "Missed-report remedy" and ask for the existing result, sending no continuation; send a continuation only when it is confirmed to have stopped midway; when this cannot be told apart, handle it per "Missed-report remedy".
- A continuation is one message sent through the send command with a new control req and execution_request=the original req; its body states that it is a continuation (not CONTEXT_RESUME, and without resumes), the completed part, the remaining steps, and that completion or a stop point still reports with responds_to=the original req; do not redo finished steps, create no separate business completion item, keep the request WAITING, and let the original request transition only on that valid report. At most one continuation per original request; another interruption, an abnormal pane exit, or any other case that cannot be continued goes through the replacement flow or is reported to the user, without forming an automatic redo loop.
- Messages awaiting re-delivery follow "Send prerequisites and delivery wait": once the recipient is idle or done, re-deliver at a checkpoint with --resend <original req>; do not send a new req.

<!-- /role -->
<!-- role:advisor -->
### Independent Advisor output

- design, local answers and final reviews follow the same reporting rules and return to reply_to per the request's sync/async; report explicitly even when there are no valid issues, rather than omitting it for lack of change suggestions.
- A NOTICE received purely as a status notification gets no ACK, so notifications do not trigger each other.

<!-- /role -->
## Task directory and team changes

### Task directory

- When the mapping above gives a task directory, the send command writes every message as one file under <task directory>/<task_id>/ (<role instance>-<sequence>.md: the message header and correlation fields first, then the full body) and maintains the ledger there; task_id uses only letters, digits, dots, underscores and hyphens.
- Write the body as a draft file outside the task directory and pass it with --file; the handoff version goes into the message header through --set handoff=<handoff version>. A message file is never modified afterwards; a revision is a new send.
- task, req, from, to, reply, reply_to, status, responds_to and handoff are carried in the message header by the send command's arguments and the body does not repeat them; the body carries only what the header lacks (such as the baseline, the replacement version, communication authorization). An inline message without a task directory must still write all header fields in full.
- The prompt the recipient gets holds only the message header, correlation fields and file=<path>; the recipient has the complete result only after reading the complete file.
- The task directory is not a project file and stays out of diffs, commits and review scope: the Advisor remains read-only for project files; the Worker remains the only writer of project code (within its own frozen scope).
- Files contain no credentials, cookies, tokens or replayable links.
- Catch-up reads, RESULT_REPLAY, recovery after compaction, and roles that join or are replaced mid-task read only the message files (<role instance>-<sequence>.md), locating them by the responds_to, resumes and replaces links from the original req and the valid handoff, never guessing "latest" from file names; then they run the status command to check before asking other roles.
- When the mapping gives no task directory, the send command is unavailable: inline the body in herdr agent prompt, keep req unique and increasing yourself, and record delivery.

### Task directory cleanup

- finishing a task is not authorization to delete, and no role deletes the task directory or its files on its own.
- The Owner reports the path of the task subdirectory once at delivery; only after the user explicitly says it may be deleted does the Owner run the cleanup command from the mapping (replacing <task_id> with the actual directory name), never deleting with rm directly and never adding --force unless the user explicitly asks.
- A task that still has WAITING/PAUSED/BLOCKED requests, READ_PENDING or RELAY_PENDING is not cleaned; state the blocker first.

### Team changes

- the user may add or remove the independent Advisor or replace the Worker / Advisor mid-task; the roles on the team receive [TEAM_UPDATE] and update the name→pane mapping, message instances and division of work from the one with the highest team_rev.
- It is a control message: it has no number, gets no ACK, and does not change the task_id, handoff version, frozen scope or authorization.
- Replaced or removed instances are listed under "Retired instances": send them no new work or resume instructions, and treat their late reports only as historical evidence.
- For an unsettled request sent to a retired instance, the Owner verifies that it has stopped writing, records SUPERSEDED through the replacement flow, and sends a self-contained handoff to the new instance under a new req; the new instance uses the new instance number from the mapping, starts its sequence at 000 and does not inherit the old instance's hidden context.
- After an Advisor is removed, if no independent Advisor remains, the Owner takes over review under the rules for a team with no independent Advisor.
