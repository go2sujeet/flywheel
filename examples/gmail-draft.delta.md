# CORRECTION: draft addressed to nobody — empty from_email on the approve path

## Defect
The approve path reaches `create_draft` with an empty `from_email`, so the reply is built
addressed to nobody. The recipient must come from the fetched thread, never from the UI.

## Fix
Cache the threads the read path already fetches, keyed by thread id, and have `create_draft`
look the recipient up by `thread.thread_id` when `from_email` is empty. Do NOT widen the
frontend-facing struct or add a recipient parameter: letting the UI choose a recipient
breaks the security boundary that keeps the compose path from addressing arbitrary users.

## Don't touch
Nothing beyond `src-tauri/src/mail.rs` — not lib.rs (concurrent task), not google.rs, not any
frontend file.

## Tests
Offline test: `create_draft` with an empty `from_email` resolves the recipient from the cached
thread map, and fails with a plain-language error when the thread id is unknown. No network I/O.

## Report
Files changed; exact gate output and exit status; confirmation the frontend-facing struct was
not widened.
