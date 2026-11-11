# Changelog

## 0.4.0

- **Blurred masking.** Hidden values in the window show as blurred text of the same shape as the real value, in titles, the list, the mail body and the text of HTML mail. The window never receives the real value.
- **Faster reading of HTML mail.** The local model no longer reads the blanks between tags: a mail that took about 2.4 s takes 0.05 s.
- **Pay slips.** Salary and amount figures separated by non-breaking spaces are hidden, and a name keeps its Latin suffix under the same mask.
- **Report a problem.** `zmail issue` and the sidebar's "反馈问题" panel open a GitHub issue with the draft filled in; you edit and submit it, and Zmail uploads nothing. An optional AI polish runs on an OpenAI-compatible service you set with `zmail llm set`.
- **Confirm before a link opens.** A link in a mail first shows its full address with a warning about phishing, and opens in the browser only after you confirm. The protection can be turned off in settings.
