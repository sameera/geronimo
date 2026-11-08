---
product: Geronimo
last_updated: 2026-11-08
---

# Product Context

## Product Overview

Geronimo connects to Gmail and Outlook and gives users different views over their whole inbox: by topic, by sender, and by urgency. Each view drills down to the emails behind it. It is built for busy professionals whose inbox grows faster than they can read it: newsletters, notifications, promotions, and the real messages buried among them. Geronimo helps them skim, act on what matters, and clear the rest.

Geronimo is the user's daily entry point instead of the provider inbox. It is the triage layer, not the reader. Tapping an email opens it in the native Gmail or Outlook UI, which also handles reading, replies, search, threads, and mark-as-unread. Flows: [docs/design/user-flows.md](../design/user-flows.md).

- **Category:** B2C productivity tool, self-serve <!-- Inferred: individual users, no team buyer -->
- **Platforms:** Mobile-first responsive web (Go + HTMX). No native app.
- **Mail providers:** Gmail, Outlook
- **Accounts:** Multiple accounts shown as one unified list, grouped, with an account chip on each row.

## Vision & Strategy

Geronimo is where you start your email day. Open it instead of your inbox, see what needs you, clear what doesn't, and step into Gmail or Outlook only to read in full or reply. Your mail stays in your own account, and Geronimo only moves it where you can get it back.

**Current focus:** Organized, drillable views that turn an overflowing inbox into something users can work through.

### Anti-goals

- **No LLM or AI features for now.** Categorizing and summarizing use rules and metadata (sender, List-Id/List-Unsubscribe headers, provider labels and categories, keywords).
- **Not a full email client.** No composing, sending, or replying. Replies happen in the native Gmail or Outlook UI.
- **Phase 1 delegates to the native UI:** email body rendering, attachments, threads, search, and mark-as-unread. Geronimo shows email lists only.
- **No unsubscribe in Phase 1.** Clearing uses archive and delete by sender or group instead.
- **No permanent delete.** Delete moves mail to trash. Geronimo has no "empty trash".
- **No team or shared inboxes.** <!-- Inferred from individual persona -->

## Personas

### Primary: The Overloaded Professional

Busy professional, 28–50. Gets more email than they can process: work threads, personal mail, subscriptions they signed up for to learn and grow, receipts, alerts. Leaves much of it unread "for later".

- **Job to be done:** When I open my inbox, I want to see what needs me and clear what doesn't, so I stop carrying a growing backlog and don't miss what's important.
- **Pain points:** The backlog grows faster than they read. Important mail gets buried under bulk mail. Deleting feels risky, so they keep everything. Gmail tabs and Outlook Focused Inbox sort mail into flat buckets but don't help them work through it.
- **Success looks like:** The unread count drops every session, and they trust they haven't missed anything important.

### Secondary: The Subscription Hoarder <!-- TODO: Verify -->

Signed up for many newsletters and courses for self-improvement. Needs the sender and topic views to triage them in bulk. Phase 1 has no unsubscribe, so this persona clears by archiving or deleting whole sender groups.

## Domain Context

- Inbox volume keeps growing: newsletters, SaaS notifications, transactional mail, marketing. Users rarely unsubscribe or delete.
- Gmail and Outlook ship their own AI summaries. Geronimo avoids that race for now and competes on organization and clearing.
- Users grant mail access to a third party only when trust is high. The first-run consent screen is a key drop-off point.
- Clearing requires write scopes (`gmail.modify`, `Mail.ReadWrite`). A read-only start cannot archive or delete. `gmail.modify` also permits body access, so the "never reads bodies" promise must be enforced in code, not only stated.

## Industry Benchmarks

| Metric                               | Benchmark                    | Use                                          |
| ------------------------------------ | ---------------------------- | -------------------------------------------- |
| OAuth connect rate (start → granted) | ~50–70% for mail-access apps | Activation estimates <!-- TODO: Verify -->   |
| Consumer productivity D30 retention  | ~10–20%                      | Retention estimates <!-- TODO: Verify -->    |

## Regulatory & Compliance

| Requirement | Applies When | Note |
| --- | --- | --- |
| Google restricted-scope verification + CASA security assessment (yearly) | Any Gmail scope that reads or changes mail (`gmail.readonly`, `gmail.modify`, `gmail.metadata`) in production beyond 100 users | Plan lead time before public launch. Recheck Google's current policy. Phase 1 requests `gmail.modify` from day one. |
| Microsoft publisher verification | Outlook (Microsoft Graph `Mail.Read`/`Mail.ReadWrite`) for consumer and multi-tenant users | Needed so the consent screen doesn't show "unverified". Phase 1 requests `Mail.ReadWrite` from day one. |
| GDPR / CCPA | Any EU or California users | Email content is personal data: retention limits, deletion on account removal. |
| Google API Services User Data Policy (Limited Use) | Any Gmail data use | No ads use, no selling data, no human reading of mail without consent. |

## Competitive Landscape

| Competitor | What it does | Gap Geronimo targets |
| --- | --- | --- |
| SaneBox | Background sorting into folders | Sorts but doesn't help clear. No drill-down views. Paid only. |
| Clean Email | Bulk cleanup by sender and pattern | Cleanup tool, not a daily way to work through the inbox. |
| Meco | Moves newsletters into a separate reader | Covers newsletters only. The rest of the inbox stays. |
| Gmail tabs / Outlook Focused Inbox | Built-in categories | Flat buckets, no topic or urgency view, no help clearing. |
| Built-in AI summaries (Gemini, Copilot) | Per-email summaries | Out of scope for us by choice. |

**Table stakes:** Gmail and Outlook support, bulk archive and mark-read, one-tap unsubscribe, clear data-privacy statement. Phase 1 knowingly omits unsubscribe.

## Success Metrics

- **North star:** Unread email count falls session over session, measured against the count when the user connects their inbox:

    | After session | Unread reduced by |
    | ------------- | ----------------- |
    | 1             | 30%               |
    | 2             | 50%               |
    | 3             | 70%               |
    | 4             | 100%              |

    <!-- Interpreted as cumulative reduction from the starting unread count. TODO: Verify -->
- **Supporting:** Share of users who reach each session target. Emails cleared per session.
- **Entry-point metrics:** Geronimo replaces the provider inbox as the place users start, so unread reduction alone is not enough. Track daily opens per active user, and the share of opened emails that the user reached by tapping through Geronimo. <!-- Added 2026-11-08. TODO: set targets once baseline data exists. -->
- **Impact thresholds:** High >20% of users, Medium 5–20%, Low <5%.
- **Effort justified:** High up to 4 weeks, Medium up to 2 weeks, Low under 1 week.

## Product Principles

1. **Clearing beats reading.** Prefer the action that reduces the unread count. Opening an email marks it read; the follow-up mark keeps it on the user's list.
2. **Trust first.** Ask for the smallest mail scopes that work. Say plainly what we store and what we never do: Geronimo reads, archives, and moves mail to trash. It never deletes permanently. It never reads message bodies. These are product commitments, not just copy: the product is built so it cannot do them, and the consent screen says so.
3. **Every view drills down.** A category is a doorway to emails, never a dead end.
4. **Reversible by default.** Archive before delete. Delete means move to trash. Every action has undo.
5. **Provider is the source of truth.** Follow-up, read state, archive, and trash are stored in Gmail or Outlook, never only in Geronimo.

## Company Scale

- **Stage:** Pre-launch, no code yet. <!-- Inferred -->
- **Team:** Solo.
- **Users:** None yet. Scope work as MVP; optimize for activation, not scale.
- **License:** GPL-3.0, open source.
