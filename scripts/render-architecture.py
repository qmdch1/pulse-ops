"""Optional documentation renderer; Pillow is not an application dependency."""
from pathlib import Path
import math
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'docs' / 'images'
OUT.mkdir(parents=True, exist_ok=True)
W, H = 1400, 860
BG, PANEL, LINE = '#121722', '#1b2433', '#35445c'
TEXT, MUTED = '#e4eaf5', '#9aaac4'
PURPLE, MINT, BLUE, AMBER = '#baa4f5', '#77d8ba', '#79b9ed', '#efbd81'

def font(size, bold=False):
    names = [Path('C:/Windows/Fonts/segoeuib.ttf' if bold else 'C:/Windows/Fonts/segoeui.ttf'), Path('/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf' if bold else '/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf')]
    return ImageFont.truetype(str(next(p for p in names if p.exists())), size)

def box(d, rect, tag, title, lines, color=PURPLE):
    x, y, right, bottom = rect
    d.rounded_rectangle(rect, radius=15, fill=PANEL, outline=LINE, width=2)
    d.rounded_rectangle((x+21,y+24,x+25,bottom-24),radius=2,fill=color)
    d.text((x+43,y+21),tag,font=font(11,True),fill=color)
    d.text((x+43,y+43),title,font=font(23,True),fill=TEXT)
    for i,line in enumerate(lines):d.text((x+43,y+82+25*i),line,font=font(14),fill=MUTED)

PATHS = [
 ([(330,236),(475,236)],PURPLE,'HTTPS / UI + JSON'),
 ([(475,274),(330,274)],PURPLE,''),
 ([(330,365),(475,365)],AMBER,'WSS / terminal'),
 ([(865,232),(1020,232)],MINT,'SSH / jump'),
 ([(1020,264),(865,264)],MINT,''),
 ([(865,405),(1020,405)],BLUE,'SQL / PING / HTTP'),
 ([(1020,437),(865,437)],BLUE,''),
 ([(650,525),(650,635)],MINT,'commit / read'),
 ([(706,635),(706,525)],PURPLE,''),
]

def arrow(d, points, color, width=2):
    d.line(points,fill=color,width=width)
    a,b=points[-2:]; angle=math.atan2(b[1]-a[1],b[0]-a[0]);length=8
    d.polygon([b,(b[0]-length*math.cos(angle-.45),b[1]-length*math.sin(angle-.45)),(b[0]-length*math.cos(angle+.45),b[1]-length*math.sin(angle+.45))],fill=color)

def scene():
    im=Image.new('RGB',(W,H),BG);d=ImageDraw.Draw(im)
    d.text((48,32),'PULSE / OPS',font=font(15,True),fill=PURPLE)
    d.text((48,63),'One Go service. Direct infrastructure visibility.',font=font(33,True),fill=TEXT)
    d.text((48,112),'Embedded native web UI  /  Same-origin API  /  Bounded collectors  /  Encrypted credentials',font=font(16),fill=MUTED)
    box(d,(48,189,330,486),'BROWSER','Native web', ['HTML + CSS + ES modules','Canvas time-series charts','Worker: fetch + rule checks','Drag, merge, split graphs','xterm loaded on demand'])
    box(d,(475,189,865,525),'ONE PROCESS / ONE CONTAINER','Go service', ['HTTP/TLS + static gzip / ETag','Basic auth + exact-origin writes','Registry API + snapshot cache','15s scheduler / 4 collector slots','One-time ticket -> SSH PTY','No Node, bundler or gateway'])
    box(d,(1020,189,1352,321),'REGISTERED INFRASTRUCTURE','Servers', ['Linux / Windows / macOS','Pinned SSH key + optional jump'],MINT)
    box(d,(1020,352,1352,515),'REGISTERED INFRASTRUCTURE','DB / cache / apps', ['PostgreSQL / MySQL / MariaDB','Oracle / Redis / HTTP','App metrics: direct collection'],BLUE)
    box(d,(475,635,865,768),'PERSISTENT LOCAL VOLUME','SQLite', ['AES-GCM registry / plain observations','Collection / connection / terminal audit'],MINT)
    d.text((48,554),'APP DEPLOYMENT',font=font(11,True),fill=PURPLE)
    d.text((48,581),'Single binary or single container',font=font(17,True),fill=TEXT)
    d.text((48,610),'TLS: Go certificates or your existing',font=font(14),fill=MUTED)
    d.text((48,633),'HTTPS ingress -> loopback :13000',font=font(14),fill=MUTED)
    d.text((1020,567),'INDEPENDENT TIMING',font=font(11,True),fill=MINT)
    d.text((1020,596),'Browser refresh: 1-3600s / chart',font=font(14),fill=TEXT)
    d.text((1020,622),'Actual collection: every 15s',font=font(14),fill=TEXT)
    d.text((1020,650),'Refresh does not trigger collection.',font=font(13),fill=MUTED)
    d.text((1020,675),'Missing samples remain gaps.',font=font(13),fill=MUTED)
    for points,color,label in PATHS:
        arrow(d,points,LINE,3)
        if label:
            x=(points[0][0]+points[-1][0])/2;y=(points[0][1]+points[-1][1])/2
            if points[0][0]==points[-1][0]:d.text((x-135,y-10),label,font=font(12),fill=color)
            else:d.text((x,y-22),label,font=font(11),fill=color,anchor='mm')
    d.line((48,798,1352,798),fill=LINE)
    for x,color,label in [(48,PURPLE,'UI / snapshots'),(320,MINT,'SSH / stored observations'),(690,BLUE,'Direct database / app metrics'),(1100,AMBER,'Interactive SSH terminal')]:
        d.ellipse((x,822,x+7,829),fill=color);d.text((x+16,815),label,font=font(12),fill=MUTED)
    return im

base=scene();base.save(OUT/'architecture.png',optimize=True)
frames=[]
for frame in range(60):
    im=base.copy();d=ImageDraw.Draw(im)
    for index,(points,color,_) in enumerate(PATHS):
        start,end=points;phase=(frame/30+index*.13)%1
        for delay in (0,.37):
            p=(phase+delay)%1;x=start[0]+(end[0]-start[0])*p;y=start[1]+(end[1]-start[1])*p
            d.ellipse((x-8,y-8,x+8,y+8),fill=BG,outline=color,width=1)
            d.ellipse((x-4,y-4,x+4,y+4),fill=color)
    frames.append(im.resize((1120,688),Image.Resampling.LANCZOS).convert('P',palette=Image.Palette.ADAPTIVE,colors=96))
frames[0].save(OUT/'traffic-flow.gif',save_all=True,append_images=frames[1:],duration=90,loop=0,optimize=False,disposal=2)
print('Architecture PNG and 60-frame traffic GIF created.')
