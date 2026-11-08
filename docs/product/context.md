---
product: Geronimo
last_updated: 2026-11-08
---

# Product Context

## Product Overview

Geronimo connects to Gmail and Outlook and gives users different views over their whole inbox: by topic, by sender, and by urgency. Each view drills down to the emails behind it. It is built for busy professionals whose inbox grows faster than they can read it: newsletters, notifications, promotions, and the real messages buried among them. Geronimo helps them skim, act on what matters, and clear the rest.

- **Category:** B2C productivity tool, self-serve <!-- Inferred: individual users, no team buyer -->
- **Platforms:** Web app (Go + HTMX) <!-- TODO: Verify target devices -->
- **Mail providers:** Gmail, Outlook

## Vision & Strategy

<!-- TODO: Your vision statement. Not inferred. -->

**Current focus:** Organized, drillable views that turn an overflowing inbox into something users can work through.

### Anti-goals

- **No LLM or AI features for now.** Categorizing and summarizing use rules and metadata (sender, List-Id/List-Unsubscribe headers, provider labels and categories, keywords).
- **Not a full email client.** No composing or sending. <!-- Inferred: TODO verify -->
- **No team or shared inboxes.** <!-- Inferred from individual persona -->

## Personas

### Primary: The Overloaded Professional

Busy professional, 28–50. Gets more email than they can process: work threads, personal mail, subscriptions they signed up for to learn and grow, receipts, alerts. Leaves much of it unread "for later".

- **Job to be done:** When I open my inbox, I want to see what needs me and clear what doesn't, so I stop carrying a growing backlog and don't miss what's important.
- **Pain points:** The backlog grows faster than they read. Important mail gets buried under bulk mail. Deleting feels risky, so they keep everything. Gmail tabs and Outlook Focused Inbox sort mail into flat buckets but don't help them work through it.
- **Success looks like:** The unread count drops every session, and they trust they haven't missed anything important.

### Secondary: The Subscription Hoarder <!-- TODO: Verify -->

Signed up for many newsletters and courses for self-improvement. Needs the sender and topic views to triage them in bulk.

## Domain Context

- Inbox volume keeps growing: newsletters, SaaS notifications, transactional mail, marketing. Users rarely unsubscribe or delete.
- Gmail and Outlook ship their own AI summaries. Geronimo avoids that race for now and competes on organization and clearing.
- Users grant mail access to a third party only when trust is high. The first-run consent screen is a key drop-off point.

## Industry Benchmarks

| Metric                               | Benchmark                    | Use                                          |
| ------------------------------------ | ---------------------------- | -------------------------------------------- |
| OAuth connect rate (start → granted) | ~50–70% for mail-access apps | Activation estimates <!-- TODO: Verify -->   |
| Consumer productivity D30 retention  | ~10–20%                      | Retention estimates <!-- TODO: Verify -->    |

## Regulatory & Compliance

| Requirement | Applies When | Note |
| --- | --- | --- |
| Google restricted-scope verification + CASA security assessment (yearly) | Any Gmail scope that reads or changes mail (`gmail.readonly`, `gmail.modify`, `gmail.metadata`) in production beyond 100 users | Plan lead time before public launch. Recheck Google's current policy. |
| Microsoft publisher verification | Outlook (Microsoft Graph `Mail.Read`/`Mail.ReadWrite`) for consumer and multi-tenant users | Needed so the consent screen doesn't show "unverified". |
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

**Table stakes:** Gmail and Outlook support, bulk archive and mark-read, one-tap unsubscribe, clear data-privacy statement.

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
- **Impact thresholds:** High >20% of users, Medium 5–20%, Low <5%.
- **Effort justified:** High up to 4 weeks, Medium up to 2 weeks, Low under 1 week.

## Product Principles

1. **Clearing beats reading.** Prefer the action that reduces the unread count.
2. **Trust first.** Ask for the smallest mail scopes that work. Say plainly what we store.
3. **Every view drills down.** A category is a doorway to emails, never a dead end.
4. **Reversible by default.** Archive before delete, so users act without fear.

## Company Scale

- **Stage:** Pre-launch, no code yet. <!-- Inferred -->
- **Team:** Solo.
- **Users:** None yet. Scope work as MVP; optimize for activation, not scale.
- **License:** GPL-3.0, open source.
