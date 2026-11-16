# Changelog

## 0.6.5

- **The agent's face keeps its disc.** The empty reader pane's avatar used to sit in a dark square: the web view's hole clears the card under it, so the face now paints its own disc in the window's base color, as the statistics page's does.
- **The face follows the pointer over the mail list.** Its page only hears the pointer over its own web view; the pane now passes it the position as far as the shell's edge, so the eyes move while you browse the list.
- **A thread reveals once, when it is ready.** The wave crossing the reader pane used to begin while HTML mail still loaded, so content flashed in half-drawn — and a mail whose sensitive parts finished being read mid-wave got a second reveal on top. The pane's wave now holds its veil until every page has loaded (five seconds at most), HTML pages stay unmounted under it until it ends, and a late masked copy joins the reveal already running instead of waving again.

## 0.6.4

- **The sign-in sheet closes once you are in.** Adding an account used to wait for the first sync — up to a minute — while the sheet hung open on "signing in". It closes as soon as the login is checked and the account kept; the first sync runs in the back like every other.
- **A mail sized to the window no longer grows without end.** A newsletter built on a 100vh block asked for a height taller than itself forever, so its card kept scrolling away. The web view now refuses the second equal growth in a row and holds its height.

## 0.6.3

- **Google sign-in needs only the address.** The OAuth client ID and secret are built into the app (an installed app's client pair is public by design), so the window's Google sign-in asks just for the mailbox. The "客户端 ID" and "客户端密钥" fields remain for using your own client, and so do `--client-id`, `$ZMAIL_CLIENT_ID` and `$ZMAIL_CLIENT_SECRET` on the command line.

## 0.6.2

- **Desktop notifications.** A new mail in the inbox raises a macOS notification: the sender as its heading, the subject and snippet as its text, grouped with the app's own. Clicking it brings up the window on that mail. At most the newest five of a sync are shown. The statistics page's "新邮件时显示桌面通知" switch turns them off; the first notification is also how macOS asks for permission.
- **Notifications stay masked.** With 打码 on, the subject and snippet a notification shows are the masked copy — patterns at once, and the local model's reading as soon as it is done. A notice never carries what the window hides.

## 0.6.1

- **Intel Macs.** Zmail is built for Intel as well as Apple silicon, for macOS 13 or later. An Intel Mac installs `zmail-0.6.1-amd64.dmg` and updates itself from its own update file; Apple silicon apps update as before.

## 0.6.0

- **Unread mail stands out.** An unread mail has a blue dot, heavier text and its time in blue; a read one is a step lighter, and its text stays in place when it is read.
- **Verification codes.** A mail that holds a verification code carries a "验证码" tag in the list, also while the code itself is hidden.
- **Command line tool.** The statistics page has a "命令行工具" panel: "安装" makes `zmail` a command in the terminal, as a link at `/usr/local/bin/zmail` to the app, so it follows the app's updates. The panel shows whether it is installed and its version, and updates another `zmail` found at that path to this app's. `zmail version` prints the version.
- **Google sign-in in the window.** Adding a Google account opens on OAuth, for Workspace accounts: the address, an OAuth client and its secret, then the browser. Personal Gmail switches to an app password.
- **A client ID is built in.** `--client-id` is optional for `zmail account add --provider google --oauth`, and so is the window's "客户端 ID" field: left out, Zmail signs in with the client ID it is built with. `--client-id`, `$ZMAIL_CLIENT_ID` or the field names another. The client's secret is asked for as before.

## 0.5.2

- **HTML mail looks better.** A mail without colors of its own is drawn on a soft ground in larger type, with the app's blue for links, and follows the system's dark appearance; a mail with styles of its own keeps the white it is drawn for.
- **Room around the text.** A mail of paragraphs that ends in a table keeps its margin instead of running to the card's edge.
- **Tables.** A table with a border attribute shows as a single-line grid with padded cells, not double lines.

## 0.5.1

- **Update check.** This release changes no behavior. It is the first one an installed 0.5.0 can update to, and confirms that the update downloads, verifies its signature and restarts into the new version.

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
