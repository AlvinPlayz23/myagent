# myagent — sourced terminal theme studies

A static, dependency-free gallery of seven **actual OpenCode dark palettes**:
Tokyo Night, Catppuccin Mocha, Kanagawa, Vesper, Osaka Jade, Rosé Pine, Vercel.
Five component views (conversation, home, models, commands, tools/diff) and a
small myagent-detail toggle keep the layout controlled while comparing color.

The large terminal frames are **SVG illustrations**, not captures from either
running TUI. They demonstrate source-faithful palettes on proposed layouts.
All sessions, code, tool timings and diff content are fixed synthetic fixtures.
The actual upstream Catppuccin screenshot is separately labeled in the sources.
No Go renderer, saved configuration, provider, or model call is changed.

## Preview

```sh
python3 -m http.server 8080 --bind 0.0.0.0 --directory docs/tui-lab
```

The page also works directly from disk.
The surrounding page inherits Hoplite theme tokens when embedded and browser
system colors standalone; the SVG images preserve the real terminal palettes.

## Rebuild the illustrations and page

```sh
python3 docs/tui-lab/build_gallery.py
```

`theme-palettes.json` records resolved dark semantic colors, original aliases,
MIT attribution, and pinned source URLs. `render_themes.py` produces the SVG
frames, and `gallery.template.html` + `build_gallery.py` assemble `index.html`.
No build-time dependencies beyond Python's standard library are needed.

See `SOURCES.md` for the research trail and license details. The earlier
performance measurements are preserved in `PERFORMANCE.md` and
`measurements.json`; this revision makes no new performance claims.
