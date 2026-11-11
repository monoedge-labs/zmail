#!/bin/sh
# Rebuilds assets/botavatars.js: the BotAvatar component of Libraries.dev
# (MIT), bundled with React for the agent's face on the statistics page.
# Needs gh and bun. The sources are read from the pinned commit, not run.
set -e
REF=387b049ca8fe97b5d72999b5bf61a19ab1ccff81
cd "$(dirname "$0")/.."
out=$(pwd)/assets/botavatars.js
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/src"
for f in BotAvatar.tsx color.ts draw.ts engine.ts mesh.ts plastic.ts presets.ts shapes.ts ticker.ts types.ts wear.ts; do
  gh api -H "Accept: application/vnd.github.raw" "repos/Jakubantalik/Libraries.dev/contents/packages/bot-avatars/src/$f?ref=$REF" > "$tmp/src/$f"
done
cat > "$tmp/entry.tsx" <<'JS'
import { createRoot } from 'react-dom/client';
import { BotAvatar } from './src/BotAvatar';

declare global { interface Window { setAvatar: (props: object) => void } }
const root = createRoot(document.getElementById('r')!);
window.setAvatar = (props) => root.render(<BotAvatar {...props} />);
JS
cd "$tmp"
echo '{"name":"botavatars","private":true,"type":"module"}' > package.json
bun add react@18 react-dom@18
bun build entry.tsx --minify --target=browser --define 'process.env.NODE_ENV="production"' --outfile="$out"
