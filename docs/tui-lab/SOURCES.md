# Theme research — 2026-11-17

## Inspected references

- OpenCode theme documentation: https://opencode.ai/docs/themes/
  Lists all seven gallery themes and documents semantic colors/raised surfaces.
- OpenCode source snapshot `73de67f`, `packages/tui/src/theme/assets/*.json`:
  exact dark roles are resolved in `theme-palettes.json`. Per-theme source links
  and named aliases are included. These are the established palette assets,
  not a claim that all current v2 contextual surface tokens are reproduced.
- Catppuccin OpenCode: https://github.com/catppuccin/opencode
  Inspected the public Mocha screenshot; it uses a plum background, a darker
  input panel, pastel foregrounds and the OpenCode agent rail. The collection
  supports alternate accents; its screenshot accent is not claimed to match
  the built-in JSON's primary blue exactly.
  Screenshot: https://raw.githubusercontent.com/catppuccin/opencode/main/assets/mocha.webp
- TokyoNight's official OpenCode extras:
  https://github.com/folke/tokyonight.nvim/tree/main/extras/opencode
- Public X post: https://x.com/anthonysheww/status/1996939778156384364
  Public text: “A Vercel theme for @opencode. Link to gist in reply.”
  The link resolved to:
  https://gist.github.com/anthonyshew/9bfd709949f83b4acac9062787c071a7
  The gist title is “Vercel theme for opencode.” The gallery uses the bundled
  OpenCode Vercel asset, not an unverified assertion that it equals the gist.
- A second public X post was accessible:
  https://x.com/kitlangton/status/2011626052955812046
  Its accessible text did not establish theme details, so it was not used as
  visual evidence. No logged-in feed or complete attached-media access.

## What is original here

The SVG layouts, fixture conversation, pixel myagent lettering, compact tool
metadata, and diamond detail toggle are new visualization work for this thread.
They are not screenshots or implemented production Go widgets. The OpenCode-like
reference mode is also an illustration, not a pixel-perfect upstream capture.

## Attribution

The OpenCode palettes are MIT licensed, Copyright (c) 2025 opencode. The complete
notice is preserved at `assets/LICENSE.opencode.txt`.
The actual Catppuccin screenshot is from `catppuccin/opencode`, MIT licensed;
the complete notice is preserved at `assets/LICENSE.catppuccin.txt`.
No third-party X images, videos, avatars, private data, or credentials are copied.
