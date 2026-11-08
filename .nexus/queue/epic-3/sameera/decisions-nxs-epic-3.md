## 2026-11-08 — Home shows pre-consent unconditionally in #33
- **Choice:** Home renders the pre-consent section for every signed-in user until #34 adds connected-mailbox state.
- **Why:** No mailbox storage exists before #34, so routing on connected state would need a table this story does not own.
- **Refuted alternative:** Add a stub mailbox count to the store in #33.
