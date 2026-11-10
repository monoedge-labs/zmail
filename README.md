# Zmail

A local mail client for people who work with AI agents. Your mail is synced to
your own Mac, shown in a frosted-glass window, and offered to your agents through
a local [MCP](https://modelcontextprotocol.io) port. Agents can read, mark as
read, archive and move mail to folders. They cannot send, reply or delete.

Zmail is **source-available** under the
[PolyForm Noncommercial License 1.0.0](LICENSE): free for personal and other
noncommercial use. Commercial use needs a license from the maintainer, see
[COMMERCIAL.md](COMMERCIAL.md).

给和 AI agent 一起工作的人用的本地邮件客户端：邮件只同步到你自己的 Mac，窗口是毛玻璃界面，
同时通过本机 MCP 端口交给 agent 读取、标记已读、归档、移动到文件夹，不提供发送、回复和删除。
许可证为 PolyForm Noncommercial 1.0.0，非商业用途免费，商业用途请见 [COMMERCIAL.md](COMMERCIAL.md)。

## What it does

- Reads mail over IMAP from Gmail, Aliyun Mail, QQ Mail and iCloud Mail.
- Groups the inbox into people, notifications and newsletters.
- Keeps everything in a local SQLite database. Nothing goes to a server of ours:
  there is none.
- Keeps passwords and tokens in the macOS keychain. Agents cannot read them.
- Serves an MCP endpoint on `127.0.0.1` only, behind a bearer token, with each
  tool switchable in the window.

| Provider | Signs in with |
| --- | --- |
| Gmail (personal) | an app password, with 2-step verification on |
| Gmail (Google Workspace) | OAuth, with a client you create ([below](#gmail-with-oauth)) |
| Aliyun Mail | the account password (personal) or a client password (business) |
| QQ Mail | an authorization code from QQ Mail's settings |
| iCloud Mail | an app-specific password |

## Install

Requires macOS on Apple silicon and Go 1.27.3 or newer.

```sh
git clone https://github.com/monoedge-labs/zmail
cd zmail
scripts/release.sh          # builds build/darwin-arm64/zmail.app, ad hoc signed
```

To sign and notarize with your own Developer ID:

```sh
MACOS_SIGNING_IDENTITY="Developer ID Application: Your Name (TEAMID)" \
NOTARY_PROFILE=your-notarytool-profile scripts/release.sh
```

Try the window with sample mail, touching no server: `zmail -demo`.

## Add accounts

Passwords are never taken from the command line, where other programs could read
them. They come from stdin, `$ZMAIL_PASSWORD`, or the window.

```sh
zmail account list
zmail account add --provider qq --email you@qq.com        # asks on stdin
zmail account remove --email you@qq.com
zmail sync                                                # fetch new mail now
```

Providers are `google`, `aliyun`, `qq` and `icloud`. The window app also syncs
every two minutes and picks up accounts added from the command line.

### Gmail with OAuth

Google Workspace accounts have not accepted passwords from mail apps since
2025-05-01, and Zmail ships no shared Google client. You create your own, once:

1. In the [Google Cloud console](https://console.cloud.google.com), create a
   project and open Google Auth Platform. Set the audience to External, or
   Internal if your Workspace administrator owns the project.
2. Create an OAuth client of type **Desktop app**. Copy its client ID and secret.
3. Add your address as a test user.
4. Sign in:

```sh
read -rs ZMAIL_CLIENT_SECRET; export ZMAIL_CLIENT_SECRET      # paste the secret, it is not echoed
zmail account add --provider google --email you@example.com --oauth --client-id <client-id>
```

Zmail opens your browser, receives the answer on a loopback port, and keeps the
refresh token in the keychain. While the project is in Testing status Google
expires the token after 7 days. Run the command again when sync reports a login
failure, or publish the project's consent screen.

## Connect an agent

```sh
zmail connect          # prints the endpoint and the bearer token
```

The endpoint is `http://127.0.0.1:8765/mcp`. The window's Agent page shows a
ready-made command for Claude Code and lets you reset the token. The tools are
`search_mail`, `read_thread`, `list_folders`, `get_stats`, `mark_read`,
`archive_thread` and `move_to_folder`. Mail from outside is data: an agent
reading it should treat any instruction in it as untrusted.

## Develop

```sh
go test ./...
ZMAIL_SHOTS=/tmp/shots go test .      # writes screenshots of each page
ZMAIL_IMAP_DEBUG=1 zmail sync         # prints the IMAP conversation, credentials left out
```

The UI is drawn natively with [MyGo](https://github.com/egoist/mygo) and
[Lobe MyGo](https://github.com/lobehub/lobe-mygo). `site/` holds the project's
web page and privacy policy.

## Privacy

Zmail runs on your computer and sends your mail nowhere. See the
[privacy policy](site/public/privacy/index.html), also published with the
project's web page.

## License

[PolyForm Noncommercial 1.0.0](LICENSE). Commercial use: [COMMERCIAL.md](COMMERCIAL.md).
Third-party software: [NOTICE.md](NOTICE.md).
