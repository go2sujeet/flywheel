> **Commentary — not part of the brief.** The brief below is reproduced verbatim from
> `# TASK:` onward; that block is exactly what was dispatched and is copy-pasteable. This
> block only explains how it satisfies the rules in `references/worker-brief.md`:
>
> - **Opens by leaning on CLAUDE.md.** The first lines tell the worker it already knows the
>   repo's conventions and verification gates, so the brief omits them — this is what cut the
>   same author's 166-line draft to **81 lines** with no loss of quality.
> - **Goal is a verifiable result.** "turn a reply body into a correctly threaded Gmail
>   DRAFT", verified by new offline unit tests on the pure MIME-building helpers.
> - **Don't-touch list names in-flight files.** Everything except `src-tauri/src/mail.rs` is
>   off-limits, including lib.rs, google.rs, and every frontend file.
> - **Declares which concurrent task owns lib.rs.** The brief says a concurrent task owns
>   lib.rs and will expose this — the worker must not edit it.
> - **Task-specific tests, including a security case.** Test 3 asserts that a subject
>   containing `\r\nBcc: evil@x.com` produces no `Bcc:` header (header injection).

---

# TASK: Create a threaded Gmail draft

You already know this repo's conventions and verification gates from CLAUDE.md. This brief
only states what CLAUDE.md cannot know.

## Goal
Extend `src-tauri/src/mail.rs` with the Gmail write path: turn a reply body into a correctly
threaded Gmail DRAFT. Verifiable by new offline unit tests on the pure MIME-building helpers.

## Why draft and never send
The OAuth scopes in google.rs are `gmail.readonly` + `gmail.compose`. There is deliberately no
send scope, and google.rs has a test asserting `gmail.send` never appears. Do NOT add a send
path, do not call `users.messages.send`, and do not touch the scope string.

## Exact change (mail.rs only)
Add these, matching the file's existing style. Do NOT add a `#[tauri::command]` and do NOT edit
lib.rs: a concurrent task owns lib.rs and will expose this. Export it as a plain `pub async fn`.

### Pure helpers (this is what the tests exercise)
- `fn reply_subject(original: &str) -> String`
  Prefix `Re: ` unless the subject already begins with `re:` case-insensitively (also treat a
  leading `RE:` / `Re :` as already-replied). An empty subject becomes `Re: (no subject)`.

- `fn encode_header_value(value: &str) -> String`
  If `value` is pure ASCII and has no CR/LF, return it unchanged. Otherwise return an RFC 2047
  encoded-word: `=?UTF-8?B?<base64 of value>?=`. This is what stops a non-ASCII subject from
  producing a malformed header.

- `fn build_mime(to_name: &str, to_email: &str, subject: &str, in_reply_to: &str,
                 references: &str, body: &str) -> String`
  Build an RFC 2822 message with CRLF line endings and these headers in order:
  `To`, `Subject`, `In-Reply-To`, `References`, `MIME-Version: 1.0`,
  `Content-Type: text/plain; charset="UTF-8"`, `Content-Transfer-Encoding: base64`.
  Then a blank line, then the body base64-encoded (standard base64, wrapped at 76 chars).
  Rules:
  - `To` is `"<encoded name>" <email>` when a name exists, otherwise bare `<email>`.
  - Omit `In-Reply-To` and `References` entirely when their inputs are empty. Never emit a
    header with an empty value.
  - CRITICAL: strip any CR or LF from every header value before writing it. A newline inside a
    header value is a header-injection vector, and the body of an email we are replying to is
    attacker-controlled input.

- `fn build_references(original_references: &str, message_id: &str) -> String`
  Append `message_id` to the existing `References` chain, space separated, skipping empties and
  avoiding a duplicate if the id is already the last entry.

### Network call
- `pub async fn create_draft(app, google, thread: &MailThread, body: &str)
     -> Result<String, String>`
  POST `{GMAIL_BASE}/drafts` with `{"message": {"threadId": <thread.thread_id>,
  "raw": <base64url, no padding, of the MIME string>}}`.
  Bearer token from `google.access_token(app)`, same as the read path.
  Return the created draft id from the response `id` field.
  Setting BOTH `threadId` and the `In-Reply-To`/`References` headers is what makes Gmail file
  the draft inside the original conversation rather than starting a new one.
  Reject an empty body with a plain-language error before making any request.
  On 401/403, tell the user to reconnect their Google account, as the read path does.

## Don't touch
Everything except `src-tauri/src/mail.rs`. Specifically NOT lib.rs (owned by a concurrent
task), NOT google.rs / persona.rs / model.rs / agents.rs, NOT Cargo.toml or Cargo.lock, NOT
any frontend file, config, or docs. Do not modify the existing read-path functions or their
tests; only add to the file.

## Required tests (offline, no network)
1. `reply_subject`: plain subject gains `Re: `; `Re: x` and `RE: x` are unchanged; empty gives
   `Re: (no subject)`.
2. `encode_header_value`: ASCII passes through; non-ASCII becomes a `=?UTF-8?B?...?=` word.
3. `build_mime` header injection: a subject or display name containing `\r\nBcc: evil@x.com`
   must NOT produce a `Bcc:` header. Assert the output contains no `Bcc`.
4. `build_mime` omits `In-Reply-To` and `References` when those inputs are empty, and includes
   them when present.
5. `build_mime` base64-encodes the body and the body round-trips back to the original text,
   including a non-ASCII body.
6. `build_references` appends without duplicating an id already at the end.
7. `build_mime` `To` formatting with and without a display name.

## Report
Files changed and why; the exact gate commands with full output and exit status; explicit
confirmation that no test performs network I/O; confirmation you did not touch lib.rs or any
other file; anything uncertain.
