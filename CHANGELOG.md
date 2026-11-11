# Changelog

## 0.5.0

- **Updates.** Zmail checks for new versions in the background and updates itself from signed GitHub releases; "Check for Updates…" is in the app menu. An update verifies a signature made with the key in `mygo.json`.
- **Labels.** An agent puts labels on mail with `zmail label add|remove --label L ID...`, lists them with `zmail label list`, and finds mail with `zmail search --label L`; `search` and `read` show each message's `labels`. Once some mail has a label, a button in the list's header groups the list by label. Labels stay on your Mac.
- **IP addresses are hidden.** IPv4 and IPv6 addresses are masked by the patterns, in the window and in what an agent reads.
- **Masking on demand.** Mail is masked when it comes into view, and an agent's reads are masked as they happen. What the local model finds is cached on disk, so the first screen of a large mailbox no longer waits for the model: on a real mailbox it fell from about 6 s to 0.6 s on the first launch, and to 10 ms on the next.
- **Loading states.** The list and an opened mail show skeletons while their text is being masked; a mail the model is reading is lit by a moving beam, and a wave crosses it once it is masked.
- **HTML mail.** Pages keep their margin from the card's edge, and the scrollbar no longer sits over the text.

## 0.4.0

- **Blurred masking.** Hidden values in the window show as blurred text of the same shape as the real value, in titles, the list, the mail body and the text of HTML mail. The window never receives the real value.
- **Faster reading of HTML mail.** The local model no longer reads the blanks between tags: a mail that took about 2.4 s takes 0.05 s.
- **Pay slips.** Salary and amount figures separated by non-breaking spaces are hidden, and a name keeps its Latin suffix under the same mask.
- **Report a problem.** `zmail issue` and the sidebar's "反馈问题" panel open a GitHub issue with the draft filled in; you edit and submit it, and Zmail uploads nothing. An optional AI polish runs on an OpenAI-compatible service you set with `zmail llm set`.
- **Confirm before a link opens.** A link in a mail first shows its full address with a warning about phishing, and opens in the browser only after you confirm. The protection can be turned off in settings.
