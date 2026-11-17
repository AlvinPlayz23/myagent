#!/usr/bin/env python3
"""Render theme-faithful concept frames; no screenshots or Go UI changes."""
import html
import json
from pathlib import Path

ROOT = Path(__file__).parent
OUT = ROOT / 'assets'
OUT.mkdir(exist_ok=True)

class Frame:
    def __init__(self, colors):
        self.c = colors
        self.parts = []
    def color(self, role):
        return self.c.get(role, self.c['text'])
    def box(self, x, y, w, h, role='backgroundPanel', stroke=None):
        self.parts.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" fill="{self.color(role)}"'+(f' stroke="{self.color(stroke)}"' if stroke else '')+'/>')
    def line(self, x1,y1,x2,y2,role='border'):
        self.parts.append(f'<path d="M{x1} {y1}H{x2}" stroke="{self.color(role)}"/>' if y1==y2 else f'<path d="M{x1} {y1}L{x2} {y2}" stroke="{self.color(role)}"/>')
    def text(self,x,y,text,role='text',size=16,bold=False):
        self.parts.append(f'<text x="{x}" y="{y}" fill="{self.color(role)}" font-size="{size}"'+(' font-weight="bold"' if bold else '')+'>'+html.escape(text)+'</text>')
    def save(self,path):
        path.write_text('<svg xmlns="http://www.w3.org/2000/svg" width="1120" height="680" viewBox="0 0 1120 680"><g font-family="DejaVu Sans Mono, monospace">'+''.join(self.parts)+'</g></svg>')

def composer(f,x=32,y=532,w=836,own=True,home=False):
    f.box(x,y,w,104,'backgroundPanel')
    f.box(x,y,3,104,'primary')
    f.text(x+24,y+35,'Ask anything…', 'textMuted',17)
    f.box(x+24,y+17,9,23,'textMuted')
    f.text(x+24,y+79,('◇ Build' if own else 'Build'),'primary',15)
    f.text(x+(115 if own else 98),y+79,'Claude Sonnet 4.5','text',15)
    f.text(x+326,y+79,('· high' if own else 'Anthropic'),'textMuted',14)
    f.text(x+w-223,y+130,'tab agents   / commands','textMuted',12)
    if own and not home:
        f.text(x,y+130,'~/myagent   main ↑1','textMuted',12)

def tool(f,x,y,w=790,own=True,expanded=True):
    if own:
        f.text(x,y,'✓','success',17)
        f.text(x+26,y,'Read','secondary',16,True)
        f.text(x+90,y,'src/ui/composer.go','text',16)
        f.text(x+w-103,y,'12ms · 48L','textMuted',12)
    else:
        f.text(x,y,'→ Read src/ui/composer.go','text',16)
    if expanded:
        f.box(x+22,y+15,w-22,110,'backgroundPanel')
        for row,(code,role) in enumerate([('func (s *Shell) Render(w, h int) string {','syntaxKeyword'),('    prompt := s.composer.Render(w, h)','text'),('    return s.root.Compose(prompt)','syntaxFunction'),('}','textMuted')]):
            f.text(x+36,y+40+row*22,code,role,14)

def logo(f,own=True):
    word='myagent' if own else 'opencode'
    # Pixel lettering retains the terminal cell structure without borrowing a font logo.
    glyphs={'m':['10001','11011','10101','10001','10001'],'y':['10001','01010','00100','00100','00100'],'a':['01110','10001','11111','10001','10001'],'g':['01111','10000','10111','10001','01110'],'e':['11111','10000','11110','10000','11111'],'n':['10001','11001','10101','10011','10001'],'t':['11111','00100','00100','00100','00100'],'o':['01110','10001','10001','10001','01110'],'p':['11110','10001','11110','10000','10000'],'c':['01111','10000','10000','10000','01111'],'d':['11110','10001','10001','10001','11110']}
    unit=8; start=(1120-(len(word)*6-1)*unit)/2
    for letter,ch in enumerate(word):
        for row,bits in enumerate(glyphs[ch]):
            for col,bit in enumerate(bits):
                if bit=='1':
                    f.box(start+(letter*6+col)*unit+1,216+row*unit+5,unit-1,unit-1,'backgroundElement')
                    f.box(start+(letter*6+col)*unit,216+row*unit,unit-1,unit-1,'textMuted' if letter<2 else 'text')

def render(theme,scene,own):
    f=Frame(theme['colors']);f.box(0,0,1120,680,'background')
    brand='myagent' if own else 'opencode'
    if scene=='home':
        logo(f,own);composer(f,205,330,710,own,True)
        if own:
            f.text(205,495,'◇  Your workspace. Your next move.','textMuted',13)
        f.text(28,650,'~/projects/myagent','textMuted',12)
        f.text(984,650,brand,'textMuted',12)
    elif scene=='session':
        f.text(32,36,('◇ ' if own else '')+'Refine the terminal composer','text',17,True)
        f.text(32,59,'session / ui-refresh','textMuted',12)
        f.box(908,0,212,680,'backgroundPanel')
        for y,label,content in [(37,'Context','8.2k / 200k · 4%'),(140,'Workspace','myagent · main'),(245,'Changes','2 files · +28 −11'),(346,'Plan','✓ inspect composer'),(428,'Language servers','● gopls connected')]:
            f.text(932,y,label,'text',14,True);f.text(932,y+28,content,'success' if y==428 else 'textMuted',11)
        f.text(932,396,'○ verify snapshots','textMuted',11)
        f.box(32,92,836,62,'backgroundPanel');f.box(32,92,3,62,'primary')
        f.text(56,129,'Make the composer feel more like myagent.','text',16)
        f.text(56,196,'Thinking: keep the layout; refine the details.','textMuted',14)
        tool(f,56,241,790,own)
        f.text(56,420,'A quieter interface, with a small identity of its own.','text',16)
        f.text(56,451,'The diamond marks the agent. Tool status stays semantic.','textMuted',14)
        f.text(56,482,'▣  Build · Claude Sonnet 4.5 · 1.4s','primary',12)
        composer(f,own=own)
    elif scene in ('models','commands'):
        # Intentionally quiet backdrop; the palette is the comparison target.
        f.text(32,36,'~/myagent','textMuted',13);composer(f,own=own)
        f.box(182,112,756,454,'backgroundPanel')
        f.text(212,148,('◇ ' if own else '')+('Select a model' if scene=='models' else 'Commands'),'text',18,True)
        f.text(870,148,'esc','textMuted',12)
        f.text(212,190,'Search models or provider…' if scene=='models' else 'Search commands…','textMuted',15)
        if scene=='models':
            rows=[('Anthropic',None,None),('Claude Sonnet 4.5','claude-sonnet-4-5','✓ current · 200k'),('Claude Opus 4.5','claude-opus-4-5','200k'),('OpenAI',None,None),('GPT-5','gpt-5','400k'),('Local',None,None),('Qwen3 Coder','qwen3-coder','128k')]
        else:
            rows=[('Suggested',None,None),('Switch model','/models',''),('Reasoning effort','/effort','high'),('Session',None,None),('New conversation','/new',''),('Resume session','/resume',''),('Appearance','/customize','')]
        for i,(title,detail,end) in enumerate(rows):
            y=229+i*38
            if detail is None:f.text(212,y,title,'secondary',13,True)
            else:
                selected=i==1
                if selected:f.box(202,y-24,716,34,'primary')
                color='background' if selected else 'text'
                f.text(212,y,('◇' if own else '›') if selected else ' ',color,15)
                f.text(240,y,title,color,15)
                if i not in (1,2) or scene!='models':f.text(554,y,detail,'background' if selected else 'textMuted',12)
                f.text(730,y,end,'background' if selected else 'textMuted',12)
        f.text(212,536,'↑↓ navigate   enter select','textMuted',12)
        f.text(803,536,'4 models' if scene=='models' else '5 actions','textMuted',12)
    else:
        f.text(36,40,('◇ ' if own else '')+'Agent activity','text',18,True)
        f.text(36,76,'Thinking · 1.2s','textMuted',14)
        f.text(56,108,'Inspect the renderer, then change only the prompt.','textMuted',14)
        tool(f,36,165,1040,own)
        f.text(36,335,'✓','success',17);f.text(63,335,'Edit src/ui/composer.go','text',16)
        f.box(58,353,1018,132,'backgroundPanel')
        f.text(75,383,'−  Border(lipgloss.RoundedBorder())','diffRemoved',15)
        f.text(75,415,'+  Border(lipgloss.Border{Left: "┃"})','diffAdded',15)
        f.text(75,447,'+  Foreground(theme.Agent(0).Color())','diffAdded',15)
        f.text(36,537,'✓','success',17);f.text(63,537,'go test ./internal/ui/...','text',16)
        f.text(63,569,'3 packages passed · 0 failures · 0.8s','success',14)
        f.text(36,634,'Build complete  ·  2 files changed','textMuted',13)
    return f

def main():
    data=json.loads((ROOT/'theme-palettes.json').read_text())
    themes=data['themes'] if isinstance(data,dict) else data
    for theme in themes:
        for scene in ['session','home','models','commands','tools']:
            for own in (False,True):render(theme,scene,own).save(OUT/f'{theme["id"]}-{scene}-{"myagent" if own else "reference"}.svg')
        f=Frame(theme['colors']);f.box(0,0,1120,680,'background')
        # Compact palette strip uses the same source colors as the concept frames.
        for i,role in enumerate(['background','backgroundPanel','primary','secondary','success','syntaxKeyword','syntaxString']):
            f.box(i*160,0,160,680,role)
        path=OUT/f'{theme["id"]}-swatch.svg'
        f.save(path)
        path.write_text(path.read_text().replace('width="1120" height="680" viewBox="0 0 1120 680"','width="1120" height="40" viewBox="0 0 1120 40" preserveAspectRatio="none"'))
    print(f'Rendered {len(themes)} themes × 5 scenes × 2 identities')

if __name__=='__main__':main()
