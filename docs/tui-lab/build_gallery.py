#!/usr/bin/env python3
"""Assemble the self-contained gallery from the resolved source palettes."""
import html
import json
from pathlib import Path
from render_themes import main as render

ROOT=Path(__file__).parent
NOTES={
 'tokyonight':('Tokyo Night','indigo / electric blue','Inky blue surfaces, electric blue focus, and lavender syntax. Cool, legible, and recognizably terminal.'),
 'catppuccin':('Catppuccin Mocha','plum / soft pastels','Plum-tinted blacks and soft pastel syntax. The panel is darker than the canvas, as in the real OpenCode preview.'),
 'kanagawa':('Kanagawa','ink / muted blue','Warm ink backgrounds with restrained blues and ochres. Dense information, without the neon.'),
 'vesper':('Vesper','charcoal / peach','Near-black surfaces with a peach accent and mint strings. Minimal, warm, and high contrast.'),
 'osaka-jade':('Osaka Jade','forest / jade','Deep green surfaces with jade focus and warm text. My preferred starting point for a subtle myagent identity.'),
 'rosepine':('Rosé Pine','violet / rose','Dark violet surfaces, soft cyan focus, and rose details. Quiet, distinctive, and less blue-dominant.'),
 'vercel':('Vercel','black / blue','Neutral black surfaces, blue focus, and saturated syntax. A Vercel-inspired direction, with a related theme shared publicly on X.'),
}

def main():
    render()
    data=json.loads((ROOT/'theme-palettes.json').read_text())
    themes=data['themes']
    options=[];frames=[];contact=[];display=[]
    for theme in themes:
        name,note,story=NOTES[theme['id']]
        t={'id':theme['id'],'name':name,'note':note,'story':story,'source':theme['source_url'],'palette':[{'name':role,'hex':theme['colors'][role]} for role in ['background','primary','text']]}
        display.append(t)
        tid=theme['id'];check=' checked' if tid=='osaka-jade' else ''
        options.append(f'<label class="theme-option"><img class="swatch" src="assets/{tid}-swatch.svg" alt="{html.escape(name)} source palette"><span class="theme-name"><input type="radio" name="theme" value="{tid}"{check}>{html.escape(name)}</span><span class="theme-note">{html.escape(note)}</span></label>')
        for scene in ['session','home','models','commands','tools']:
            for own in (True,False):
                suffix='myagent' if own else 'reference'
                attrs=f'data-theme="{tid}" data-scene="{scene}" data-own="{str(own).lower()}"'
                img=f'<img {attrs} src="assets/{tid}-{scene}-{suffix}.svg" alt="{html.escape(name)} · {scene} · {suffix} concept" width="1120" height="680"'
                visible=tid=='osaka-jade' and scene=='session' and own
                frames.append(img+('>' if visible else ' hidden>'))
                contact.append(f'<figure data-scene="{scene}" data-own="{str(own).lower()}" hidden><a href="#theme-title" data-pick="{tid}">'+img+f'><figcaption>{html.escape(name)}<span>inspect ↗</span></figcaption></a></figure>')
    template=(ROOT/'gallery.template.html').read_text()
    page=template.replace('__THEME_OPTIONS__',''.join(options)).replace('__FRAMES__',''.join(frames)).replace('__CONTACT__',''.join(contact)).replace('__REVISION__',data['source']['short_revision']).replace('__THEME_DATA__',json.dumps(display,ensure_ascii=False))
    (ROOT/'index.html').write_text(page)
    print(f'Built gallery: {len(themes)} palettes, {len(frames)} main frames, {len(page.encode())} HTML bytes')

if __name__=='__main__':main()
